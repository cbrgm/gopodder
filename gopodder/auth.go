package gopodder

import (
	"cmp"
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const contextKeyUsername contextKey = "username"

func UsernameFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(contextKeyUsername).(string); ok {
		return v
	}
	return ""
}

func (a *API) authenticate(r *http.Request) (*User, bool) {
	username, password, ok := r.BasicAuth()
	if !ok {
		if sessionID := extractSessionCookie(r); sessionID != "" {
			user, err := a.store.GetUserBySession(r.Context(), sessionID)
			if err == nil && user != nil && !a.apiSessionExpired(r.Context(), user.SessionCreated) {
				return user, true
			}
		}
		return nil, false
	}

	user, err := a.store.GetUser(r.Context(), username)
	if err != nil {
		return nil, false
	}

	if !checkPassword(user.PWHash, password) {
		return nil, false
	}

	return user, true
}

func (a *API) apiSessionExpired(ctx context.Context, sessionCreated *time.Time) bool {
	if sessionCreated == nil {
		return true
	}
	val, _ := a.store.GetSetting(ctx, SettingSessionMaxAge)
	hours, _ := strconv.ParseInt(val, 10, 64)
	hours = cmp.Or(hours, defaultSessionMaxAgeHours)
	return time.Since(*sessionCreated) > time.Duration(hours)*time.Hour
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	pathUser := r.PathValue("username")

	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="gopodder"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if pathUser != username {
		http.Error(w, "username mismatch", http.StatusBadRequest)
		return
	}

	user, err := a.store.GetUser(r.Context(), username)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if !checkPassword(user.PWHash, password) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := uuid.New().String()
	_ = a.store.UpdateUserSession(r.Context(), username, &sessionID, time.Now())

	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "sessionid",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	a.logger.Debug("user logged in", "username", username)
	w.WriteHeader(http.StatusOK)
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sessionID := extractSessionCookie(r); sessionID != "" {
		user, err := a.store.GetUserBySession(r.Context(), sessionID)
		if err == nil && user != nil {
			_ = a.store.UpdateUserSession(r.Context(), user.Username, nil, time.Time{})
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "sessionid",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	w.WriteHeader(http.StatusOK)
}

func extractSessionCookie(r *http.Request) string {
	cookie, err := r.Cookie("sessionid")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func hashPassword(password string) string {
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash)
}

func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
