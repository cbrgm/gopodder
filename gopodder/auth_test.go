package gopodder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	t.Run("produces bcrypt hash", func(t *testing.T) {
		got := hashPassword("testpass")
		if got == "" {
			t.Error("hashPassword returned empty string")
		}
		if got[0:4] != "$2a$" {
			t.Errorf("hashPassword should produce bcrypt hash, got prefix %q", got[0:4])
		}
	})

	t.Run("checkPassword verifies correctly", func(t *testing.T) {
		hash := hashPassword("hello")
		if !checkPassword(hash, "hello") {
			t.Error("checkPassword should return true for correct password")
		}
		if checkPassword(hash, "wrong") {
			t.Error("checkPassword should return false for wrong password")
		}
	})

	t.Run("different calls produce different hashes", func(t *testing.T) {
		a := hashPassword("hello")
		b := hashPassword("hello")
		if a == b {
			t.Error("bcrypt should produce different hashes due to random salt")
		}
	})
}

func TestExtractSessionCookie(t *testing.T) {
	tests := []struct {
		name   string
		cookie *http.Cookie
		want   string
	}{
		{"valid cookie", &http.Cookie{Name: "sessionid", Value: "abc-123"}, "abc-123"},
		{"no cookie", nil, ""},
		{"wrong cookie name", &http.Cookie{Name: "other", Value: "abc-123"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.cookie != nil {
				r.AddCookie(tt.cookie)
			}
			got := extractSessionCookie(r)
			if got != tt.want {
				t.Errorf("extractSessionCookie() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUsernameFromContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"username present", context.WithValue(t.Context(), contextKeyUsername, "testuser"), "testuser"},
		{"username absent", t.Context(), ""},
		{"wrong type in context", context.WithValue(t.Context(), contextKeyUsername, 123), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UsernameFromContext(tt.ctx)
			if got != tt.want {
				t.Errorf("UsernameFromContext() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWithCORS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := withCORS(inner)

	t.Run("sets CORS headers on normal request", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("missing Access-Control-Allow-Origin header")
		}
		if w.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("missing Access-Control-Allow-Methods header")
		}
		if w.Header().Get("Access-Control-Allow-Headers") == "" {
			t.Error("missing Access-Control-Allow-Headers header")
		}
		if w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Error("Allow-Credentials must not be set with Allow-Origin: * (CORS spec violation)")
		}
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("OPTIONS preflight returns 200 without calling next", func(t *testing.T) {
		called := false
		preflight := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))

		r := httptest.NewRequest(http.MethodOptions, "/", nil)
		w := httptest.NewRecorder()
		preflight.ServeHTTP(w, r)

		if called {
			t.Error("OPTIONS should not call inner handler")
		}
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestHandleLogin(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{
		Username: "testuser",
		PWHash:   hashPassword("testpass"),
	}
	api := newTestAPI(store)
	handler := api.Handler()

	tests := []struct {
		name       string
		username   string
		password   string
		pathUser   string
		wantStatus int
	}{
		{"successful login", "testuser", "testpass", "testuser", http.StatusOK},
		{"wrong password", "testuser", "wrongpass", "testuser", http.StatusUnauthorized},
		{"nonexistent user", "nobody", "pass", "nobody", http.StatusUnauthorized},
		{"path username mismatch", "testuser", "testpass", "otheruser", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/2/auth/"+tt.pathUser+"/login.json", nil)
			r.SetBasicAuth(tt.username, tt.password)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				cookies := w.Result().Cookies()
				if !slices.ContainsFunc(cookies, func(c *http.Cookie) bool {
					return c.Name == "sessionid" && c.Value != ""
				}) {
					t.Error("expected sessionid cookie on successful login")
				}
			}
		})
	}
}

func TestHandleLogin_NoAuth(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	r := httptest.NewRequest(http.MethodPost, "/api/2/auth/testuser/login.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestHandleLogin_RotatesSession(t *testing.T) {
	store := newMockStore()
	sid := "old-session-123"
	store.users["testuser"] = &User{
		Username:  "testuser",
		PWHash:    hashPassword("testpass"),
		SessionID: &sid,
	}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("login always generates new session", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/2/auth/testuser/login.json", nil)
		r.SetBasicAuth("testuser", "testpass")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		idx := slices.IndexFunc(w.Result().Cookies(), func(c *http.Cookie) bool {
			return c.Name == "sessionid" && c.Value != ""
		})
		if idx == -1 {
			t.Fatal("expected new sessionid cookie")
		}
		if w.Result().Cookies()[idx].Value == "old-session-123" {
			t.Error("login should rotate session, not reuse old one")
		}
	})

	t.Run("login without basic auth returns 401", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/2/auth/testuser/login.json", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})
}

func TestHandleLogout(t *testing.T) {
	store := newMockStore()
	sid := "session-abc"
	now := time.Now()
	store.users["testuser"] = &User{
		Username:       "testuser",
		PWHash:         hashPassword("testpass"),
		SessionID:      &sid,
		SessionCreated: &now,
	}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("logout clears cookie and invalidates server session", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/2/auth/testuser/logout.json", nil)
		r.AddCookie(&http.Cookie{Name: "sessionid", Value: "session-abc"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if !slices.ContainsFunc(w.Result().Cookies(), func(c *http.Cookie) bool {
			return c.Name == "sessionid" && c.MaxAge < 0
		}) {
			t.Error("expected sessionid cookie to be cleared")
		}
		if store.users["testuser"].SessionID != nil {
			t.Error("session should be invalidated in DB after logout")
		}
	})

	t.Run("logout without cookie is OK", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/2/auth/testuser/logout.json", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestSessionBasedAuth(t *testing.T) {
	store := newMockStore()
	sid := "valid-session"
	now := time.Now()
	store.users["testuser"] = &User{
		Username:       "testuser",
		PWHash:         hashPassword("testpass"),
		SessionID:      &sid,
		SessionCreated: &now,
	}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("session cookie authenticates request", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/2/devices/testuser.json", nil)
		r.AddCookie(&http.Cookie{Name: "sessionid", Value: "valid-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("invalid session falls back to basic auth", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/2/devices/testuser.json", nil)
		r.AddCookie(&http.Cookie{Name: "sessionid", Value: "invalid-session"})
		r.SetBasicAuth("testuser", "testpass")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("no credentials returns 401 with WWW-Authenticate", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/2/devices/testuser.json", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
		if w.Header().Get("WWW-Authenticate") == "" {
			t.Error("expected WWW-Authenticate header")
		}
	})
}

func TestRouteAuth_InvalidPaths(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"unknown action", "/api/2/auth/testuser/unknown.json", http.StatusNotFound},
		{"too few segments", "/api/2/auth/testuser", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, tt.path, nil)
			r.SetBasicAuth("testuser", "testpass")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestWithRequestLogging(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	api := newTestAPI(newMockStore())
	handler := api.withRequestLogging(inner)

	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Error("inner handler should be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
