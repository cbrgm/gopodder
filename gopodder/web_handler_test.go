package gopodder

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func withCSRF(sessionID, body string) string {
	token := generateCSRFToken(sessionID)
	if body == "" {
		return "csrf_token=" + token
	}
	return body + "&csrf_token=" + token
}

func createMultipartFileWithCSRF(t *testing.T, fieldName, fileName, content, sessionID, device string) (*strings.Reader, string) {
	t.Helper()
	var b strings.Builder
	w := multipart.NewWriter(&b)
	_ = w.WriteField("csrf_token", generateCSRFToken(sessionID))
	if device != "" {
		_ = w.WriteField("device", device)
	}
	part, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = part.Write([]byte(content))
	_ = w.Close()
	return strings.NewReader(b.String()), w.FormDataContentType()
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048576, "1.0 MiB"},
		{1073741824, "1.0 GiB"},
		{2684354560, "2.5 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input time.Time
		want  string
	}{
		{"zero time", time.Time{}, "-"},
		{"unix zero", time.Unix(0, 0), "-"},
		{"valid time", time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC), "2024-06-15 10:30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTimestamp(tt.input)
			if got != tt.want {
				t.Errorf("formatTimestamp() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOptionalTimeAgo(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		got := optionalTimeAgo(nil)
		if got != "-" {
			t.Errorf("optionalTimeAgo(nil) = %q, want %q", got, "-")
		}
	})

	t.Run("zero time", func(t *testing.T) {
		ts := time.Time{}
		got := optionalTimeAgo(&ts)
		if got != "-" {
			t.Errorf("optionalTimeAgo(zero) = %q, want %q", got, "-")
		}
	})

	t.Run("valid time returns non-empty", func(t *testing.T) {
		ts := time.Now().Add(-5 * time.Minute)
		got := optionalTimeAgo(&ts)
		if got == "-" || got == "" {
			t.Errorf("optionalTimeAgo(5m ago) = %q, want a relative time string", got)
		}
	})
}

func TestHasAPIPrefix(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/2/devices/testuser.json", true},
		{"/api/2/auth/user/login.json", true},
		{"/api/", true},
		{"/login", false},
		{"/users", false},
		{"/admin/accounts", false},
		{"/ap", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := hasAPIPrefix(tt.path)
			if got != tt.want {
				t.Errorf("hasAPIPrefix(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestWebAccountFromContext(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		acct := &Account{ID: "id1", Username: "admin", Role: RoleAdmin}
		ctx := context.WithValue(t.Context(), webContextKeyAccount, acct)
		got := webAccountFromContext(ctx)
		if got == nil || got.Username != "admin" {
			t.Errorf("webAccountFromContext() = %v, want admin account", got)
		}
	})

	t.Run("absent", func(t *testing.T) {
		got := webAccountFromContext(t.Context())
		if got != nil {
			t.Errorf("webAccountFromContext() = %v, want nil", got)
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx := context.WithValue(t.Context(), webContextKeyAccount, "not an account")
		got := webAccountFromContext(ctx)
		if got != nil {
			t.Errorf("webAccountFromContext() = %v, want nil", got)
		}
	})
}

func TestIsLastAdmin(t *testing.T) {
	store := newMockStore()
	store.accounts["a1"] = &Account{ID: "a1", Username: "admin1", Role: RoleAdmin}
	api := NewAPI(nil, store, noopMetrics{}, BuildInfo{}, "localhost:8080", "sqlite")
	h := NewWebHandler(api)

	t.Run("single admin is last", func(t *testing.T) {
		if !h.isLastAdmin(t.Context(), "a1") {
			t.Error("expected true for single admin")
		}
	})

	t.Run("not last when another admin exists", func(t *testing.T) {
		store.accounts["a2"] = &Account{ID: "a2", Username: "admin2", Role: RoleAdmin}
		if h.isLastAdmin(t.Context(), "a1") {
			t.Error("expected false when another admin exists")
		}
	})

	t.Run("standard account is not last admin", func(t *testing.T) {
		store.accounts["a3"] = &Account{ID: "a3", Username: "user1", Role: RoleStandard}
		if h.isLastAdmin(t.Context(), "a3") {
			t.Error("expected false for standard account (other admins exist)")
		}
	})
}

func TestWithSession_Unauthenticated(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	h := NewWebHandler(api)

	called := false
	handler := h.withSession(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	r := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	handler(w, r)

	if called {
		t.Error("handler should not be called without session")
	}
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestWithSession_Authenticated(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "web-session-123"
	store.accounts["admin-id"].SessionID = &sid
	h := NewWebHandler(api)

	called := false
	handler := h.withSession(func(w http.ResponseWriter, r *http.Request) {
		called = true
		acct := webAccountFromContext(r.Context())
		if acct == nil || acct.Username != "admin" {
			t.Error("expected admin account in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest(http.MethodGet, "/users", nil)
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "web-session-123"})
	w := httptest.NewRecorder()
	handler(w, r)

	if !called {
		t.Error("handler should be called with valid session")
	}
}

func TestWithAdmin_NonAdmin(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	h := NewWebHandler(api)

	called := false
	handler := h.withAdmin(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	r := httptest.NewRequest(http.MethodGet, "/admin/accounts", nil)
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
	w := httptest.NewRecorder()
	handler(w, r)

	if called {
		t.Error("handler should not be called for non-admin")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestWithAdmin_Admin(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "admin-session"
	store.accounts["admin-id"].SessionID = &sid
	h := NewWebHandler(api)

	called := false
	handler := h.withAdmin(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest(http.MethodGet, "/admin/accounts", nil)
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "admin-session"})
	w := httptest.NewRecorder()
	handler(w, r)

	if !called {
		t.Error("handler should be called for admin")
	}
}

func TestSetupGuard(t *testing.T) {
	t.Run("redirects to setup when no accounts", func(t *testing.T) {
		store := newMockStore()
		// Remove the default admin that newTestAPI adds
		delete(store.accounts, "admin-id")
		api := NewAPI(nil, store, noopMetrics{}, BuildInfo{}, "localhost:8080", "sqlite")
		h := NewWebHandler(api)

		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler := h.SetupGuard(inner)
		r := httptest.NewRequest(http.MethodGet, "/users", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		if loc := w.Header().Get("Location"); loc != "/setup" {
			t.Errorf("Location = %q, want /setup", loc)
		}
	})

	t.Run("passes through for setup path", func(t *testing.T) {
		store := newMockStore()
		delete(store.accounts, "admin-id")
		api := NewAPI(nil, store, noopMetrics{}, BuildInfo{}, "localhost:8080", "sqlite")
		h := NewWebHandler(api)

		called := false
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		handler := h.SetupGuard(inner)
		r := httptest.NewRequest(http.MethodGet, "/setup", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if !called {
			t.Error("inner handler should be called for /setup")
		}
	})

	t.Run("passes through for API paths", func(t *testing.T) {
		store := newMockStore()
		delete(store.accounts, "admin-id")
		api := NewAPI(nil, store, noopMetrics{}, BuildInfo{}, "localhost:8080", "sqlite")
		h := NewWebHandler(api)

		called := false
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		handler := h.SetupGuard(inner)
		r := httptest.NewRequest(http.MethodGet, "/api/2/devices/user.json", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if !called {
			t.Error("inner handler should be called for API paths")
		}
	})

	t.Run("passes through for login path", func(t *testing.T) {
		store := newMockStore()
		delete(store.accounts, "admin-id")
		api := NewAPI(nil, store, noopMetrics{}, BuildInfo{}, "localhost:8080", "sqlite")
		h := NewWebHandler(api)

		called := false
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		handler := h.SetupGuard(inner)
		r := httptest.NewRequest(http.MethodGet, "/login", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if !called {
			t.Error("inner handler should be called for /login")
		}
	})

	t.Run("passes through when accounts exist", func(t *testing.T) {
		store := newMockStore()
		api := newTestAPI(store)
		h := NewWebHandler(api)

		called := false
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		handler := h.SetupGuard(inner)
		r := httptest.NewRequest(http.MethodGet, "/users", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if !called {
			t.Error("inner handler should be called when accounts exist")
		}
	})
}

func TestUserBelongsToAccount(t *testing.T) {
	store := newMockStore()
	store.users["alice"] = &User{Username: "alice", PWHash: "hash", AccountID: "acc1"}
	api := newTestAPI(store)
	h := NewWebHandler(api)

	t.Run("correct account", func(t *testing.T) {
		if !h.userBelongsToAccount(t.Context(), "alice", "acc1") {
			t.Error("expected true for correct account")
		}
	})

	t.Run("wrong account", func(t *testing.T) {
		if h.userBelongsToAccount(t.Context(), "alice", "acc2") {
			t.Error("expected false for wrong account")
		}
	})

	t.Run("nonexistent user", func(t *testing.T) {
		if h.userBelongsToAccount(t.Context(), "nobody", "acc1") {
			t.Error("expected false for nonexistent user")
		}
	})
}

func TestIsSettingEnabled(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	h := NewWebHandler(api)

	t.Run("returns false when setting does not exist", func(t *testing.T) {
		if h.isSettingEnabled(t.Context(), SettingSelfRegistration) {
			t.Error("expected false for nonexistent setting")
		}
	})

	t.Run("returns false when setting is false", func(t *testing.T) {
		store.settings[SettingSelfRegistration] = "false"
		if h.isSettingEnabled(t.Context(), SettingSelfRegistration) {
			t.Error("expected false for disabled setting")
		}
	})

	t.Run("returns true when setting is true", func(t *testing.T) {
		store.settings[SettingSelfRegistration] = "true"
		if !h.isSettingEnabled(t.Context(), SettingSelfRegistration) {
			t.Error("expected true for enabled setting")
		}
	})
}

func TestHandleRegisterPage_Disabled(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodGet, "/register", nil)
	w := httptest.NewRecorder()
	h.handleRegisterPage(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestHandleRegisterPage_Enabled(t *testing.T) {
	store := newMockStore()
	store.settings[SettingSelfRegistration] = "true"
	api := newTestAPI(store)
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodGet, "/register", nil)
	w := httptest.NewRecorder()
	h.handleRegisterPage(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestHandleRegisterSubmit_Disabled(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("username=newuser&password=testpass1&password2=testpass1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleRegisterSubmit(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if len(store.accounts) != 1 { // only the test admin
		t.Errorf("account was created despite registration being disabled")
	}
}

func TestHandleRegisterSubmit_Success(t *testing.T) {
	store := newMockStore()
	store.settings[SettingSelfRegistration] = "true"
	api := newTestAPI(store)
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("username=newuser&password=testpass1&password2=testpass1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleRegisterSubmit(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}

	// Verify account was created with standard role
	acct, err := store.GetAccount(t.Context(), "newuser")
	if err != nil {
		t.Fatalf("account not created: %v", err)
	}
	if acct.Role != RoleStandard {
		t.Errorf("role = %q, want %q", acct.Role, RoleStandard)
	}
}

func TestHandleRegisterSubmit_PasswordMismatch(t *testing.T) {
	store := newMockStore()
	store.settings[SettingSelfRegistration] = "true"
	api := newTestAPI(store)
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("username=newuser&password=testpass1&password2=different"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleRegisterSubmit(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (renders error page)", w.Code, http.StatusOK)
	}
	if len(store.accounts) != 1 { // only the test admin
		t.Errorf("account was created despite password mismatch")
	}
}

func TestHandleRegisterSubmit_DuplicateUsername(t *testing.T) {
	store := newMockStore()
	store.settings[SettingSelfRegistration] = "true"
	api := newTestAPI(store)
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("username=admin&password=testpass1&password2=testpass1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleRegisterSubmit(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (renders error page)", w.Code, http.StatusOK)
	}
}

func TestHandleSelfCreateUser_Disabled(t *testing.T) {
	store := newMockStore()
	store.settings["allow_user_creation"] = "false"
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("username=gpuser1&password=testpass1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
	w := httptest.NewRecorder()

	handler := h.withSession(h.handleSelfCreateUser)
	handler(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "error=") {
		t.Errorf("expected error redirect, got Location = %q", loc)
	}
	if _, err := store.GetUser(t.Context(), "gpuser1"); err == nil {
		t.Error("user should not have been created")
	}
}

func TestHandleSelfCreateUser_Enabled(t *testing.T) {
	store := newMockStore()
	store.settings[SettingAllowUserCreation] = "true"
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("username=gpuser1&password=testpass1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
	w := httptest.NewRecorder()

	handler := h.withSession(h.handleSelfCreateUser)
	handler(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if _, err := store.GetUser(t.Context(), "gpuser1"); err != nil {
		t.Errorf("user should have been created: %v", err)
	}
}

func TestHandleSelfCreateUser_AdminAlwaysAllowed(t *testing.T) {
	store := newMockStore()
	// allow_user_creation is NOT enabled
	api := newTestAPI(store)
	sid := "admin-session"
	store.accounts["admin-id"].SessionID = &sid
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("username=gpuser1&password=testpass1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "admin-session"})
	w := httptest.NewRecorder()

	handler := h.withSession(h.handleSelfCreateUser)
	handler(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if _, err := store.GetUser(t.Context(), "gpuser1"); err != nil {
		t.Errorf("admin should always be able to create users: %v", err)
	}
}

func TestHandleSettingsSave(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "admin-session"
	store.accounts["admin-id"].SessionID = &sid
	h := NewWebHandler(api)

	t.Run("enable both settings", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader("self_registration=true&allow_user_creation=true&session_max_age_hours=168&episode_retention_days=90&inactive_account_days=0"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "admin-session"})
		w := httptest.NewRecorder()

		handler := h.withAdmin(h.handleSettingsSave)
		handler(w, r)

		if w.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		if store.settings[SettingSelfRegistration] != "true" {
			t.Errorf("self_registration = %q, want true", store.settings[SettingSelfRegistration])
		}
		if store.settings[SettingAllowUserCreation] != "true" {
			t.Errorf("allow_user_creation = %q, want true", store.settings[SettingAllowUserCreation])
		}
	})

	t.Run("disable both settings", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader("session_max_age_hours=168&episode_retention_days=90&inactive_account_days=0"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "admin-session"})
		w := httptest.NewRecorder()

		handler := h.withAdmin(h.handleSettingsSave)
		handler(w, r)

		if store.settings[SettingSelfRegistration] != "false" {
			t.Errorf("self_registration = %q, want false", store.settings[SettingSelfRegistration])
		}
		if store.settings[SettingAllowUserCreation] != "false" {
			t.Errorf("allow_user_creation = %q, want false", store.settings[SettingAllowUserCreation])
		}
	})
}

func TestUserLimitReached(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	h := NewWebHandler(api)

	t.Run("no limit set means unlimited", func(t *testing.T) {
		if h.userLimitReached(t.Context(), "admin-id") {
			t.Error("expected false when no limit is set")
		}
	})

	t.Run("limit 0 means unlimited", func(t *testing.T) {
		store.settings[SettingMaxUsersPerAccount] = "0"
		store.users["u1"] = &User{Username: "u1", AccountID: "admin-id"}
		store.users["u2"] = &User{Username: "u2", AccountID: "admin-id"}
		if h.userLimitReached(t.Context(), "admin-id") {
			t.Error("expected false when limit is 0 (unlimited)")
		}
	})

	t.Run("under limit", func(t *testing.T) {
		store.settings[SettingMaxUsersPerAccount] = "3"
		if h.userLimitReached(t.Context(), "admin-id") {
			t.Error("expected false when under limit (2 < 3)")
		}
	})

	t.Run("at limit", func(t *testing.T) {
		store.settings[SettingMaxUsersPerAccount] = "2"
		if !h.userLimitReached(t.Context(), "admin-id") {
			t.Error("expected true when at limit (2 >= 2)")
		}
	})

	t.Run("over limit", func(t *testing.T) {
		store.settings[SettingMaxUsersPerAccount] = "1"
		if !h.userLimitReached(t.Context(), "admin-id") {
			t.Error("expected true when over limit (2 >= 1)")
		}
	})

	t.Run("limit applies per account", func(t *testing.T) {
		store.settings[SettingMaxUsersPerAccount] = "2"
		store.accounts["other-id"] = &Account{ID: "other-id", Username: "other", Role: RoleStandard}
		store.users["u3"] = &User{Username: "u3", AccountID: "other-id"}
		// other-id has 1 user, limit is 2
		if h.userLimitReached(t.Context(), "other-id") {
			t.Error("expected false for other account (1 < 2)")
		}
	})
}

func TestHandleSelfCreateUser_LimitReached(t *testing.T) {
	store := newMockStore()
	store.settings[SettingAllowUserCreation] = "true"
	store.settings[SettingMaxUsersPerAccount] = "1"
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	store.users["existing"] = &User{Username: "existing", AccountID: "u1"}
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("username=newuser&password=testpass1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
	w := httptest.NewRecorder()

	handler := h.withSession(h.handleSelfCreateUser)
	handler(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "error=") {
		t.Errorf("expected error redirect, got Location = %q", loc)
	}
	if _, err := store.GetUser(t.Context(), "newuser"); err == nil {
		t.Error("user should not have been created when limit is reached")
	}
}

func TestHandleSettingsSave_MaxUsers(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "admin-session"
	store.accounts["admin-id"].SessionID = &sid
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader("max_users_per_account=5&session_max_age_hours=168&episode_retention_days=90&inactive_account_days=0"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "admin-session"})
	w := httptest.NewRecorder()

	handler := h.withAdmin(h.handleSettingsSave)
	handler(w, r)

	if store.settings[SettingMaxUsersPerAccount] != "5" {
		t.Errorf("max_users_per_account = %q, want 5", store.settings[SettingMaxUsersPerAccount])
	}
}

func TestCheckPasswordLength(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	h := NewWebHandler(api)

	t.Run("no limit set enforces default minimum 8", func(t *testing.T) {
		if msg := h.checkPasswordLength(t.Context(), "short"); msg == "" {
			t.Error("expected error for password shorter than default minimum 8")
		}
		if msg := h.checkPasswordLength(t.Context(), "longpass"); msg != "" {
			t.Errorf("expected empty for 8-char password, got %q", msg)
		}
	})

	t.Run("limit 0 enforces default minimum 8", func(t *testing.T) {
		store.settings[SettingMinPasswordLength] = "0"
		if msg := h.checkPasswordLength(t.Context(), "short"); msg == "" {
			t.Error("expected error for password shorter than default minimum 8")
		}
	})

	t.Run("password too short", func(t *testing.T) {
		store.settings[SettingMinPasswordLength] = "8"
		msg := h.checkPasswordLength(t.Context(), "short")
		if msg == "" {
			t.Error("expected error for short password")
		}
		if !strings.Contains(msg, "8") {
			t.Errorf("error should mention the required length, got %q", msg)
		}
	})

	t.Run("password exactly at minimum", func(t *testing.T) {
		store.settings[SettingMinPasswordLength] = "8"
		if msg := h.checkPasswordLength(t.Context(), "12345678"); msg != "" {
			t.Errorf("expected empty for exact-length password, got %q", msg)
		}
	})

	t.Run("password over minimum", func(t *testing.T) {
		store.settings[SettingMinPasswordLength] = "8"
		if msg := h.checkPasswordLength(t.Context(), "longenoughpassword"); msg != "" {
			t.Errorf("expected empty for long password, got %q", msg)
		}
	})
}

func TestHandleRegisterSubmit_PasswordTooShort(t *testing.T) {
	store := newMockStore()
	store.settings[SettingSelfRegistration] = "true"
	store.settings[SettingMinPasswordLength] = "10"
	api := newTestAPI(store)
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("username=newuser&password=short&password2=short"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleRegisterSubmit(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (renders error page)", w.Code, http.StatusOK)
	}
	if len(store.accounts) != 1 {
		t.Error("account should not have been created")
	}
}

func TestHandleSelfCreateUser_PasswordTooShort(t *testing.T) {
	store := newMockStore()
	store.settings[SettingAllowUserCreation] = "true"
	store.settings[SettingMinPasswordLength] = "10"
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	h := NewWebHandler(api)

	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("username=gpuser1&password=short"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
	w := httptest.NewRecorder()

	handler := h.withSession(h.handleSelfCreateUser)
	handler(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "error=") {
		t.Errorf("expected error redirect, got Location = %q", loc)
	}
	if _, err := store.GetUser(t.Context(), "gpuser1"); err == nil {
		t.Error("user should not have been created")
	}
}

func TestSetupGuard_RegisterPath(t *testing.T) {
	store := newMockStore()
	delete(store.accounts, "admin-id")
	api := NewAPI(nil, store, noopMetrics{}, BuildInfo{}, "localhost:8080", "sqlite")
	h := NewWebHandler(api)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := h.SetupGuard(inner)
	r := httptest.NewRequest(http.MethodGet, "/register", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Error("inner handler should be called for /register")
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int64
	}{
		{"int64", int64(42), 42},
		{"float64", float64(99.0), 99},
		{"int", int(7), 7},
		{"nil", nil, 0},
		{"string", "hello", 0},
		{"zero int64", int64(0), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toInt64(tt.input)
			if got != tt.want {
				t.Errorf("toInt64(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleSelfAccountPage(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	handler := api.Handler()

	r := httptest.NewRequest(http.MethodGet, "/account", nil)
	r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Change Password") {
		t.Error("expected Change Password section on account page")
	}
	if !strings.Contains(w.Body.String(), "Delete My Account") {
		t.Error("expected Delete Account section on account page")
	}
}

func TestHandleSelfChangeAccountPassword(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("oldpass"), Role: RoleStandard, SessionID: &sid}
	handler := api.Handler()

	t.Run("wrong current password", func(t *testing.T) {
		body := withCSRF("user-session", "current_password=wrongpass&password=newpass&password2=newpass")
		r := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error in redirect, got %q", loc)
		}
	})

	t.Run("passwords do not match", func(t *testing.T) {
		body := withCSRF("user-session", "current_password=oldpass&password=newpass&password2=different")
		r := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error in redirect, got %q", loc)
		}
	})

	t.Run("successful password change", func(t *testing.T) {
		body := withCSRF("user-session", "current_password=oldpass&password=newpass123&password2=newpass123")
		r := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "flash=") {
			t.Errorf("expected flash in redirect, got %q", loc)
		}
		if !checkPassword(store.accounts["u1"].PWHash, "newpass123") {
			t.Error("password should be updated in store")
		}
	})

	t.Run("password too short", func(t *testing.T) {
		store.settings["min_password_length"] = "10"
		body := withCSRF("user-session", "current_password=newpass123&password=short&password2=short")
		r := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error for short password, got %q", loc)
		}
		delete(store.settings, "min_password_length")
	})
}

func TestHandleSelfDeleteAccount(t *testing.T) {
	t.Run("standard user can delete own account", func(t *testing.T) {
		store := newMockStore()
		api := newTestAPI(store)
		sid := "user-session"
		store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
		store.users["gpuser1"] = &User{Username: "gpuser1", PWHash: "hash", AccountID: "u1"}
		store.subscriptions["gpuser1"] = []string{"http://feed.com"}
		handler := api.Handler()

		r := httptest.NewRequest(http.MethodPost, "/account/delete", strings.NewReader(withCSRF("user-session", "")))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		if w.Header().Get("Location") != "/login" {
			t.Errorf("expected redirect to /login, got %q", w.Header().Get("Location"))
		}
		if _, ok := store.accounts["u1"]; ok {
			t.Error("account should be deleted")
		}
		if _, ok := store.users["gpuser1"]; ok {
			t.Error("gpodder user should be cascade deleted")
		}
		if _, ok := store.subscriptions["gpuser1"]; ok {
			t.Error("subscriptions should be cascade deleted")
		}
	})

	t.Run("last admin cannot delete own account", func(t *testing.T) {
		store := newMockStore()
		api := newTestAPI(store)
		sid := "admin-session"
		store.accounts["admin-id"] = &Account{ID: "admin-id", Username: "admin", PWHash: hashPassword("admin"), Role: RoleAdmin, SessionID: &sid}
		handler := api.Handler()

		r := httptest.NewRequest(http.MethodPost, "/account/delete", strings.NewReader(withCSRF("admin-session", "")))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "admin-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error for last admin, got %q", loc)
		}
		if _, ok := store.accounts["admin-id"]; !ok {
			t.Error("last admin account should not be deleted")
		}
	})
}

func TestHandleSelfImportOPML(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	store.users["gpuser1"] = &User{Username: "gpuser1", PWHash: "hash", AccountID: "u1"}
	store.devices["gpuser1"] = []Device{{ID: "phone", Type: "mobile"}}
	handler := api.Handler()

	t.Run("imports flat OPML", func(t *testing.T) {
		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Feed 1" xmlUrl="http://feed1.com/rss"/>
    <outline type="rss" text="Feed 2" xmlUrl="http://feed2.com/rss"/>
  </body>
</opml>`
		body, contentType := createMultipartFileWithCSRF(t, "file", "subs.opml", opml, "user-session", "")
		r := httptest.NewRequest(http.MethodPost, "/users/gpuser1/subscriptions/import", body)
		r.Header.Set("Content-Type", contentType)
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "flash=") {
			t.Errorf("expected flash in redirect, got %q", loc)
		}
		subs := store.subscriptions["gpuser1"]
		if len(subs) != 2 {
			t.Errorf("expected 2 imported subscriptions, got %d: %v", len(subs), subs)
		}
	})

	t.Run("imports nested OPML", func(t *testing.T) {
		store2 := newMockStore()
		api2 := newTestAPI(store2)
		sid2 := "user-session"
		store2.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid2}
		store2.users["gpuser1"] = &User{Username: "gpuser1", PWHash: "hash", AccountID: "u1"}
		handler2 := api2.Handler()

		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="Technology">
      <outline type="rss" text="Tech Feed" xmlUrl="http://tech.com/rss"/>
    </outline>
    <outline text="Science">
      <outline type="rss" text="Sci Feed" xmlUrl="http://sci.com/rss"/>
      <outline type="rss" text="Nature" xmlUrl="http://nature.com/rss"/>
    </outline>
  </body>
</opml>`
		body, contentType := createMultipartFileWithCSRF(t, "file", "nested.opml", opml, "user-session", "")
		r := httptest.NewRequest(http.MethodPost, "/users/gpuser1/subscriptions/import", body)
		r.Header.Set("Content-Type", contentType)
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler2.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		subs := store2.subscriptions["gpuser1"]
		if len(subs) != 3 {
			t.Errorf("expected 3 imported subscriptions from nested OPML, got %d: %v", len(subs), subs)
		}
	})

	t.Run("invalid OPML returns error", func(t *testing.T) {
		body, contentType := createMultipartFileWithCSRF(t, "file", "bad.opml", "not valid xml", "user-session", "")
		r := httptest.NewRequest(http.MethodPost, "/users/gpuser1/subscriptions/import", body)
		r.Header.Set("Content-Type", contentType)
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error for invalid OPML, got %q", loc)
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		store.users["otheruser"] = &User{Username: "otheruser", PWHash: "hash", AccountID: "other-account"}
		opml := `<?xml version="1.0"?><opml version="2.0"><body><outline type="rss" xmlUrl="http://x.com"/></body></opml>`
		body, contentType := createMultipartFileWithCSRF(t, "file", "subs.opml", opml, "user-session", "")
		r := httptest.NewRequest(http.MethodPost, "/users/otheruser/subscriptions/import", body)
		r.Header.Set("Content-Type", contentType)
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		if w.Header().Get("Location") != "/users" {
			t.Errorf("expected redirect to /users, got %q", w.Header().Get("Location"))
		}
	})
}

func TestHandleSelfAddSubscription(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	store.users["gpuser1"] = &User{Username: "gpuser1", PWHash: "hash", AccountID: "u1"}
	store.devices["gpuser1"] = []Device{{ID: "phone", Type: "mobile"}}
	handler := api.Handler()

	t.Run("adds subscription to specified device", func(t *testing.T) {
		body := withCSRF("user-session", "url=http://newpod.com/feed&device=phone")
		r := httptest.NewRequest(http.MethodPost, "/users/gpuser1/subscriptions/add", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		subs := store.subscriptions["gpuser1"]
		var found bool
		for _, s := range subs {
			if s == "http://newpod.com/feed" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected subscription to be added, got %v", subs)
		}
	})

	t.Run("defaults to web device when none specified", func(t *testing.T) {
		body := withCSRF("user-session", "url=http://another.com/feed")
		r := httptest.NewRequest(http.MethodPost, "/users/gpuser1/subscriptions/add", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		subs := store.subscriptions["gpuser1"]
		var found bool
		for _, s := range subs {
			if s == "http://another.com/feed" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected subscription on 'web' device, got %v", subs)
		}
	})

	t.Run("empty URL returns error", func(t *testing.T) {
		body := withCSRF("user-session", "url=&device=phone")
		r := httptest.NewRequest(http.MethodPost, "/users/gpuser1/subscriptions/add", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "error=") {
			t.Errorf("expected error redirect, got %q", loc)
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		store.users["otheruser"] = &User{Username: "otheruser", PWHash: "hash", AccountID: "other-account"}
		body := withCSRF("user-session", "url=http://evil.com/feed&device=phone")
		r := httptest.NewRequest(http.MethodPost, "/users/otheruser/subscriptions/add", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		if w.Header().Get("Location") != "/users" {
			t.Errorf("expected redirect to /users, got %q", w.Header().Get("Location"))
		}
	})
}

func TestHandleSelfDeleteSubscription(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	store.users["gpuser1"] = &User{Username: "gpuser1", PWHash: "hash", AccountID: "u1"}
	store.devices["gpuser1"] = []Device{{ID: "phone", Type: "mobile"}, {ID: "laptop", Type: "desktop"}}
	store.subscriptions["gpuser1"] = []string{"http://feed1.com", "http://feed2.com", "http://feed3.com"}
	handler := api.Handler()

	t.Run("removes subscription", func(t *testing.T) {
		body := withCSRF("user-session", "url=http://feed1.com")
		r := httptest.NewRequest(http.MethodPost, "/users/gpuser1/subscriptions/delete-one", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		for _, s := range store.subscriptions["gpuser1"] {
			if s == "http://feed1.com" {
				t.Error("feed1 should be removed")
			}
		}
		if len(store.subscriptions["gpuser1"]) != 2 {
			t.Errorf("should have 2 subs remaining, got %d", len(store.subscriptions["gpuser1"]))
		}
	})

	t.Run("forbidden for other user's data", func(t *testing.T) {
		store.users["otheruser"] = &User{Username: "otheruser", PWHash: "hash", AccountID: "other-account"}
		body := withCSRF("user-session", "url=http://feed2.com")
		r := httptest.NewRequest(http.MethodPost, "/users/otheruser/subscriptions/delete-one", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "user-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		if w.Header().Get("Location") != "/users" {
			t.Errorf("expected redirect to /users for forbidden, got %q", w.Header().Get("Location"))
		}
	})
}

func TestHandleCreateAPIKey_LimitEnforced(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	store.settings[SettingAllowAPIKeys] = "true"
	store.settings[SettingMaxAPIKeys] = "2"

	for i := range 2 {
		store.apiKeys = append(store.apiKeys, APIKey{
			ID: fmt.Sprintf("key-%d", i), AccountID: "u1", Name: fmt.Sprintf("key%d", i),
			Prefix: fmt.Sprintf("gp_%07d", i), Hash: "h", Role: RoleStandard,
		})
	}

	r := httptest.NewRequest(http.MethodGet, "/account", nil)
	r.AddCookie(&http.Cookie{Name: "web_session", Value: sid})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	csrf := extractCSRF(w.Body.String())

	r = httptest.NewRequest(http.MethodPost, "/account/keys", strings.NewReader("csrf_token="+csrf+"&name=overflow"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: sid})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=Maximum") {
		t.Errorf("expected limit error in redirect, got %q", loc)
	}
}

func TestHandleCreateAPIKey_AdminBypassesLimit(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	sid := "admin-session"
	store.accounts["admin-id"].SessionID = &sid
	store.settings[SettingAllowAPIKeys] = "true"
	store.settings[SettingMaxAPIKeys] = "1"

	store.apiKeys = append(store.apiKeys, APIKey{
		ID: "existing", AccountID: "admin-id", Name: "existing",
		Prefix: "gp_aaaa0000", Hash: "h", Role: RoleAdmin,
	})

	r := httptest.NewRequest(http.MethodGet, "/account", nil)
	r.AddCookie(&http.Cookie{Name: "web_session", Value: sid})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	csrf := extractCSRF(w.Body.String())

	r = httptest.NewRequest(http.MethodPost, "/account/keys", strings.NewReader("csrf_token="+csrf+"&name=admin-extra&role=admin"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: sid})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if strings.Contains(loc, "error=") {
		t.Errorf("admin should bypass key limit, got error redirect: %q", loc)
	}
}

func TestHandleCreateAPIKey_ZeroSettingUsesDefault(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	sid := "user-session"
	store.accounts["u1"] = &Account{ID: "u1", Username: "user1", PWHash: hashPassword("pass"), Role: RoleStandard, SessionID: &sid}
	store.settings[SettingAllowAPIKeys] = "true"
	store.settings[SettingMaxAPIKeys] = "0"

	r := httptest.NewRequest(http.MethodGet, "/account", nil)
	r.AddCookie(&http.Cookie{Name: "web_session", Value: sid})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	csrf := extractCSRF(w.Body.String())

	r = httptest.NewRequest(http.MethodPost, "/account/keys", strings.NewReader("csrf_token="+csrf+"&name=should-work"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "web_session", Value: sid})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if strings.Contains(loc, "error=") {
		t.Errorf("setting 0 should use default (25), key creation should succeed, got: %q", loc)
	}
}

func TestClampedMinIntString(t *testing.T) {
	tests := []struct {
		input string
		min   int64
		want  string
	}{
		{"5", 1, "5"},
		{"0", 1, "1"},
		{"-3", 1, "1"},
		{"", 1, "1"},
		{"100", 1, "100"},
		{"abc", 1, "1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := clampedMinIntString(tt.input, tt.min)
			if got != tt.want {
				t.Errorf("clampedMinIntString(%q, %d) = %q, want %q", tt.input, tt.min, got, tt.want)
			}
		})
	}
}

func extractCSRF(body string) string {
	const marker = `name="csrf_token" value="`
	idx := strings.Index(body, marker)
	if idx == -1 {
		return ""
	}
	start := idx + len(marker)
	end := strings.Index(body[start:], `"`)
	if end == -1 {
		return ""
	}
	return body[start : start+end]
}

func TestHandleHealthz(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "ok\n" {
		t.Errorf("body = %q, want %q", w.Body.String(), "ok\n")
	}
}

func TestSessionExpiry(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)

	sid := "web-session"
	expired := time.Now().Add(-200 * time.Hour)
	store.accounts["u1"] = &Account{
		ID: "u1", Username: "user1", PWHash: hashPassword("pass"),
		Role: RoleStandard, SessionID: &sid, SessionCreated: &expired,
	}
	handler := api.Handler()

	t.Run("expired web session redirects to login", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/users", nil)
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "web-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
		}
		if w.Header().Get("Location") != "/login" {
			t.Errorf("Location = %q, want /login", w.Header().Get("Location"))
		}
	})

	t.Run("fresh session works", func(t *testing.T) {
		fresh := time.Now()
		store.accounts["u1"].SessionID = &sid
		store.accounts["u1"].SessionCreated = &fresh
		r := httptest.NewRequest(http.MethodGet, "/users", nil)
		r.AddCookie(&http.Cookie{Name: "web_session", Value: "web-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestHandlePublicOPML(t *testing.T) {
	store := newMockStore()
	token := "test-share-token"
	store.users["gpuser1"] = &User{Username: "gpuser1", PWHash: hashPassword("pass"), AccountID: "acct1", ShareToken: &token}
	store.subscriptions["gpuser1"] = []string{"http://a.com/feed", "http://b.com/feed"}
	api := NewAPI(nil, store, noopMetrics{}, BuildInfo{}, "localhost:8080", "sqlite")
	h := NewWebHandler(api)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("valid token returns OPML", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/user/gpuser1/subscriptions.opml?token=test-share-token", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if ct := w.Header().Get("Content-Type"); ct != "text/x-opml+xml" {
			t.Errorf("content-type = %q, want text/x-opml+xml", ct)
		}
		body := w.Body.String()
		if !strings.Contains(body, "http://a.com/feed") || !strings.Contains(body, "http://b.com/feed") {
			t.Error("expected feed URLs in OPML output")
		}
	})

	t.Run("invalid token returns 404", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/user/gpuser1/subscriptions.opml?token=wrong", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("missing token returns 404", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/user/gpuser1/subscriptions.opml", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("wrong username returns 404", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/user/otheruser/subscriptions.opml?token=test-share-token", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})
}

func TestHandlePublicRSS(t *testing.T) {
	store := newMockStore()
	token := "test-share-token"
	store.users["gpuser1"] = &User{Username: "gpuser1", PWHash: hashPassword("pass"), AccountID: "acct1", ShareToken: &token}
	store.subscriptions["gpuser1"] = []string{"http://a.com/feed", "http://b.com/feed"}
	api := NewAPI(nil, store, noopMetrics{}, BuildInfo{}, "localhost:8080", "sqlite")
	h := NewWebHandler(api)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("valid token returns RSS", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/user/gpuser1/subscriptions/rss?token=test-share-token", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/rss+xml" {
			t.Errorf("content-type = %q, want application/rss+xml", ct)
		}
		body := w.Body.String()
		if !strings.Contains(body, "http://a.com/feed") || !strings.Contains(body, "http://b.com/feed") {
			t.Error("expected feed URLs in RSS output")
		}
		if !strings.Contains(body, "<rss") {
			t.Error("expected RSS document structure")
		}
	})

	t.Run("invalid token returns 404", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/user/gpuser1/subscriptions/rss?token=wrong", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})
}
