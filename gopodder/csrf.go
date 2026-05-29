package gopodder

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
)

var getCSRFSecret = sync.OnceValue(func() []byte {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	return secret
})

func generateCSRFToken(sessionID string) string {
	mac := hmac.New(sha256.New, getCSRFSecret())
	mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func validCSRFToken(token, sessionID string) bool {
	expected := generateCSRFToken(sessionID)
	return hmac.Equal([]byte(token), []byte(expected))
}

func csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if hasAPIPrefix(r.URL.Path) || r.URL.Path == "/login" || r.URL.Path == "/setup" || r.URL.Path == "/register" {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie("web_session")
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}

			token := r.FormValue("csrf_token")
			if token == "" {
				token = r.Header.Get("X-CSRF-Token")
			}
			if !validCSRFToken(token, cookie.Value) {
				http.Error(w, "invalid csrf token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
