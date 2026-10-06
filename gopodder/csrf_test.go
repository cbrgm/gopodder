package gopodder

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCSRFProtect_RejectsBadTokens(t *testing.T) {
	sid, token := "user-session", "share-tok"
	store := newFixtureStore(t)
	must(t, store.UpdateAccountSession(t.Context(), "admin-id", &sid, time.Now()))
	addUser(t, store, User{Username: "user1", AccountID: "admin-id", ShareToken: &token})
	handler := newTestAPI(store).Handler()

	cases := []struct {
		name      string
		form      url.Values
		header    string
		wantBlock bool
	}{
		{name: "missing token", form: url.Values{}, wantBlock: true},
		{name: "wrong token", form: url.Values{"csrf_token": {"nope"}}, wantBlock: true},
		{name: "token of another session", form: url.Values{"csrf_token": {generateCSRFToken("other-session")}}, wantBlock: true},
		{name: "valid form token", form: url.Values{"csrf_token": {generateCSRFToken(sid)}}},
		{name: "valid header token", form: url.Values{}, header: generateCSRFToken(sid)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			must(t, store.SetUserShareToken(t.Context(), "user1", &token))
			r := httptest.NewRequest(http.MethodPost, "/users/user1/sharing/disable", strings.NewReader(tc.form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.header != "" {
				r.Header.Set("X-CSRF-Token", tc.header)
			}
			r.AddCookie(&http.Cookie{Name: "web_session", Value: sid})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			disabled := userOf(t, store, "user1").ShareToken == nil
			if tc.wantBlock {
				if w.Code != http.StatusForbidden {
					t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
				}
				if disabled {
					t.Error("request without a valid csrf token still changed data")
				}
				return
			}
			if w.Code == http.StatusForbidden || !disabled {
				t.Errorf("valid token rejected: status = %d, sharing disabled = %v", w.Code, disabled)
			}
		})
	}
}
