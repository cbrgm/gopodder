package gopodder

import (
	"cmp"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newFixtureStore returns a real in-memory store holding the admin account the
// handler tests log in with.
func newFixtureStore(t *testing.T) *SQLStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	addAccount(t, s, Account{ID: "admin-id", Username: "admin", PWHash: testHash("admin"), Role: RoleAdmin})
	return s
}

// addAccount creates the account, replacing one with the same ID.
func addAccount(t *testing.T, s Store, a Account) {
	t.Helper()
	ctx := t.Context()
	if _, err := s.GetAccountByID(ctx, a.ID); err == nil {
		must(t, s.DeleteAccount(ctx, a.ID))
	}
	if err := s.CreateAccount(ctx, a.ID, a.Username, a.PWHash, a.Role, cmp.Or(a.CreatedAt, time.Now())); err != nil {
		t.Fatalf("seed account %q: %v", a.ID, err)
	}
	if a.SessionID != nil {
		if err := s.UpdateAccountSession(ctx, a.ID, a.SessionID, ptrOr(a.SessionCreated, time.Now())); err != nil {
			t.Fatalf("seed account session %q: %v", a.ID, err)
		}
	}
	if a.LastLogin != nil {
		if err := s.UpdateAccountLastLogin(ctx, a.ID, *a.LastLogin); err != nil {
			t.Fatalf("seed account last login %q: %v", a.ID, err)
		}
	}
}

// ensureAccount creates a bare account for fixtures that only name one, the
// schema requires it for users and api keys.
func ensureAccount(t *testing.T, s Store, id string) {
	t.Helper()
	if _, err := s.GetAccountByID(t.Context(), id); err != nil {
		addAccount(t, s, Account{ID: id, Username: id, Role: RoleStandard})
	}
}

func addUser(t *testing.T, s Store, u User) {
	t.Helper()
	ctx := t.Context()
	u.AccountID = cmp.Or(u.AccountID, testAccountID)
	ensureAccount(t, s, u.AccountID)
	if err := s.CreateUser(ctx, u.Username, u.PWHash, u.AccountID); err != nil {
		t.Fatalf("seed user %q: %v", u.Username, err)
	}
	if u.SessionID != nil {
		if err := s.UpdateUserSession(ctx, u.Username, u.SessionID, ptrOr(u.SessionCreated, time.Now())); err != nil {
			t.Fatalf("seed user session %q: %v", u.Username, err)
		}
	}
	if u.ShareToken != nil {
		if err := s.SetUserShareToken(ctx, u.Username, u.ShareToken); err != nil {
			t.Fatalf("seed share token %q: %v", u.Username, err)
		}
	}
	if u.LastActivity != nil {
		if err := s.UpdateUserLastActivity(ctx, u.Username, *u.LastActivity); err != nil {
			t.Fatalf("seed user activity %q: %v", u.Username, err)
		}
	}
}

func setDevices(t *testing.T, s Store, username string, devices []Device) {
	t.Helper()
	for _, d := range devices {
		dev := DeviceUpdate{Caption: &d.Caption}
		if d.Type != "" {
			dev.Type = &d.Type
		}
		if err := s.UpsertDevice(t.Context(), username, d.ID, dev); err != nil {
			t.Fatalf("seed device %q: %v", d.ID, err)
		}
		if d.LastActivity != nil {
			if err := s.UpdateDeviceLastActivity(t.Context(), username, d.ID, *d.LastActivity); err != nil {
				t.Fatalf("seed device activity %q: %v", d.ID, err)
			}
		}
	}
}

func setSubscriptions(t *testing.T, s Store, username string, urls []string) {
	t.Helper()
	if err := s.ReplaceSubscriptions(t.Context(), username, urls, time.Now().Unix()); err != nil {
		t.Fatalf("seed subscriptions %q: %v", username, err)
	}
}

func setEpisodes(t *testing.T, s Store, username string, episodes []Episode) {
	t.Helper()
	if err := s.UpdateEpisodes(t.Context(), username, episodes, time.Now().Unix()); err != nil {
		t.Fatalf("seed episodes %q: %v", username, err)
	}
}

func setSetting(t *testing.T, s Store, key, value string) {
	t.Helper()
	if err := s.SetSetting(t.Context(), key, value); err != nil {
		t.Fatalf("seed setting %q: %v", key, err)
	}
}

func addAPIKey(t *testing.T, s Store, k APIKey) {
	t.Helper()
	k.CreatedAt = cmp.Or(k.CreatedAt, time.Now())
	ensureAccount(t, s, k.AccountID)
	if err := s.CreateAPIKey(t.Context(), k); err != nil {
		t.Fatalf("seed api key %q: %v", k.ID, err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func subscriptionsOf(t *testing.T, s Store, username string) []string {
	t.Helper()
	subs, err := s.GetSubscriptions(t.Context(), username)
	must(t, err)
	return subs
}

func devicesOf(t *testing.T, s Store, username string) []Device {
	t.Helper()
	devs, err := s.ListDevices(t.Context(), username)
	must(t, err)
	return devs
}

// settingOf returns the stored value, "" when the setting is unset.
func settingOf(t *testing.T, s Store, key string) string {
	t.Helper()
	v, _ := s.GetSetting(t.Context(), key)
	return v
}

// accountOf returns the account, nil when it does not exist.
func accountOf(t *testing.T, s Store, id string) *Account {
	t.Helper()
	a, _ := s.GetAccountByID(t.Context(), id)
	return a
}

// userOf returns the user, nil when it does not exist.
func userOf(t *testing.T, s Store, username string) *User {
	t.Helper()
	u, _ := s.GetUser(t.Context(), username)
	return u
}

func accountsOf(t *testing.T, s Store) []Account {
	t.Helper()
	accts, err := s.ListAccounts(t.Context())
	must(t, err)
	return accts
}

func accountCount(t *testing.T, s Store) int {
	t.Helper()
	n, err := s.CountAccounts(t.Context())
	must(t, err)
	return int(n)
}

func ptrOr[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}

// testHash hashes a fixture password, panicking on the errors that can only
// come from a malformed test fixture.
func testHash(password string) string {
	hash, err := hashPassword(password)
	if err != nil {
		panic(err)
	}
	return hash
}

func newTestAPI(store Store) *API {
	logger := slog.Default()
	return NewAPI(logger, store, noopMetrics{}, BuildInfo{
		Version:   "test",
		Revision:  "abc123",
		BuildDate: "2024-01-01",
		GoVersion: "go1.22.0",
		Platform:  "linux/amd64",
	}, "127.0.0.1:8080", "sqlite")
}

func authedRequest(method, path, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.SetBasicAuth("testuser", "testpass")
	return r
}
