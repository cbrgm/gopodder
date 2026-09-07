package gopodder

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var errStoreDown = errors.New("store unavailable")

// failingStore wraps a Store and fails exactly one write, so a handler can be
// checked for what it reports back when the database refuses the change.
type failingStore struct {
	Store
	failOn string
}

func (f *failingStore) fails(op string) error {
	if f.failOn == op {
		return errStoreDown
	}
	return nil
}

func (f *failingStore) UpdateAccountPassword(ctx context.Context, id, pwhash string) error {
	if err := f.fails("UpdateAccountPassword"); err != nil {
		return err
	}
	return f.Store.UpdateAccountPassword(ctx, id, pwhash)
}

func (f *failingStore) DeleteAPIKey(ctx context.Context, id, accountID string) error {
	if err := f.fails("DeleteAPIKey"); err != nil {
		return err
	}
	return f.Store.DeleteAPIKey(ctx, id, accountID)
}

func (f *failingStore) SetUserShareToken(ctx context.Context, username string, token *string) error {
	if err := f.fails("SetUserShareToken"); err != nil {
		return err
	}
	return f.Store.SetUserShareToken(ctx, username, token)
}

func (f *failingStore) SetSetting(ctx context.Context, key, value string) error {
	if err := f.fails("SetSetting"); err != nil {
		return err
	}
	return f.Store.SetSetting(ctx, key, value)
}

func (f *failingStore) UpdateSubscriptions(ctx context.Context, username string, add, remove []string, ts int64) error {
	if err := f.fails("UpdateSubscriptions"); err != nil {
		return err
	}
	return f.Store.UpdateSubscriptions(ctx, username, add, remove, ts)
}

func (f *failingStore) DeleteDevice(ctx context.Context, username, deviceID string) error {
	if err := f.fails("DeleteDevice"); err != nil {
		return err
	}
	return f.Store.DeleteDevice(ctx, username, deviceID)
}

// webTestEnv builds a handler backed by a store whose named write always fails.
func webTestEnv(t *testing.T, failOn string) http.Handler {
	t.Helper()
	sid := "user-session"
	ms := newMockStore()
	ms.accounts["admin-id"] = &Account{ID: "admin-id", Username: "admin", PWHash: testHash("admin"), Role: RoleAdmin, SessionID: &sid}
	ms.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}
	ms.apiKeys = append(ms.apiKeys, APIKey{ID: "key1", AccountID: "admin-id", Name: "test", Prefix: "gp_test"})
	ms.settings[SettingAllowSharing] = "true"
	ms.settings[SettingAllowAPIKeys] = "true"
	return newTestAPI(&failingStore{Store: ms, failOn: failOn}).Handler()
}

func postForm(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(withCSRF("user-session", body)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// A failed write must never be reported to the user as a success. Each case
// below used to redirect with flash= regardless of what the store did.
func TestFailedWriteIsReportedNotSwallowed(t *testing.T) {
	cases := []struct {
		name   string
		failOn string
		path   string
		body   string
	}{
		{"change own password", "UpdateAccountPassword", "/account/password", "current_password=admin&password=newpass123&password2=newpass123"},
		{"revoke api key", "DeleteAPIKey", "/account/keys/key1/delete", ""},
		{"disable sharing", "SetUserShareToken", "/users/user1/sharing/disable", ""},
		{"enable sharing", "SetUserShareToken", "/users/user1/sharing/enable", ""},
		{"save settings", "SetSetting", "/admin/settings", "session_max_age_hours=24&episode_retention_days=0&inactive_account_days=0&max_users_per_account=5&max_api_keys_per_account=5&min_password_length=8"},
		{"add subscription", "UpdateSubscriptions", "/users/user1/subscriptions/add", "url=https://example.com/feed.xml"},
		{"delete device", "DeleteDevice", "/users/user1/devices/dev1/delete", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postForm(t, webTestEnv(t, tc.failOn), tc.path, tc.body)

			loc := w.Header().Get("Location")
			if strings.Contains(loc, "flash=") {
				t.Errorf("failed %s reported as success: %q", tc.failOn, loc)
			}
			if !strings.Contains(loc, "error=") {
				t.Errorf("failed %s gave no error to the user: %q", tc.failOn, loc)
			}
		})
	}
}

// The same handlers must still report success when the store accepts the write,
// so the error path above cannot be satisfied by always erroring.
func TestSuccessfulWriteStillReportsSuccess(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
	}{
		{"change own password", "/account/password", "current_password=admin&password=newpass123&password2=newpass123"},
		{"revoke api key", "/account/keys/key1/delete", ""},
		{"disable sharing", "/users/user1/sharing/disable", ""},
		{"save settings", "/admin/settings", "session_max_age_hours=24&episode_retention_days=0&inactive_account_days=0&max_users_per_account=5&max_api_keys_per_account=5&min_password_length=8"},
		{"add subscription", "/users/user1/subscriptions/add", "url=https://example.com/feed.xml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postForm(t, webTestEnv(t, ""), tc.path, tc.body)

			loc := w.Header().Get("Location")
			if !strings.Contains(loc, "flash=") {
				t.Errorf("successful write not reported as success: %q", loc)
			}
			if strings.Contains(loc, "error=") {
				t.Errorf("successful write reported an error: %q", loc)
			}
		})
	}
}

// Best-effort activity timestamps must stay best effort. A failing
// UpdateAccountLastLogin should not block the login it is recording.
func TestActivityTimestampFailureDoesNotBlockLogin(t *testing.T) {
	ms := newMockStore()
	ms.accounts["a1"] = &Account{ID: "a1", Username: "user1", PWHash: testHash("secret123"), Role: RoleStandard}
	handler := newTestAPI(&lastLoginFailsStore{Store: ms}).Handler()

	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=user1&password=secret123"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); strings.Contains(loc, "error=") {
		t.Errorf("login blocked by a failed activity timestamp: %q", loc)
	}
}

type lastLoginFailsStore struct{ Store }

func (s *lastLoginFailsStore) UpdateAccountLastLogin(context.Context, string, time.Time) error {
	return errStoreDown
}

// Garbage in a numeric settings field must be rejected, not silently coerced to
// zero, because zero means "unlimited" for the per-account sync login limit.
func TestSettingsRejectsNonNumericInput(t *testing.T) {
	fields := []string{"max_users_per_account", "max_api_keys_per_account", "min_password_length", "session_max_age_hours", "episode_retention_days", "inactive_account_days"}

	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			form := map[string]string{
				"session_max_age_hours":    "24",
				"episode_retention_days":   "0",
				"inactive_account_days":    "0",
				"max_users_per_account":    "5",
				"max_api_keys_per_account": "5",
				"min_password_length":      "8",
			}
			form[field] = "abc"

			var body strings.Builder
			for k, v := range form {
				body.WriteString(k + "=" + v + "&")
			}
			w := postForm(t, webTestEnv(t, ""), "/admin/settings", strings.TrimSuffix(body.String(), "&"))

			loc := w.Header().Get("Location")
			if !strings.Contains(loc, "error=") {
				t.Errorf("non-numeric %s accepted: %q", field, loc)
			}
		})
	}
}
