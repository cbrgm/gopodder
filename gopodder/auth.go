package gopodder

import (
	"cmp"
	"context"
	"net/http"
	"time"
	"uuid"

	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const contextKeyUsername contextKey = "username"

// maxPasswordBytes is the hard input limit of bcrypt. Longer passwords are
// rejected during validation so they can never reach the hashing step.
const maxPasswordBytes = 72

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
	hours := cmp.Or(settingInt(ctx, a.store, SettingSessionMaxAge), defaultSessionMaxAgeHours)
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

	maxAgeHours := cmp.Or(settingInt(r.Context(), a.store, SettingSessionMaxAge), defaultSessionMaxAgeHours)

	http.SetCookie(w, &http.Cookie{
		Name:     "sessionid",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAgeHours) * 3600,
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

// hashPassword hashes a password with bcrypt. It fails for passwords longer
// than maxPasswordBytes; callers must not store the returned hash on error, or
// the account ends up with an empty hash and nobody can log into it.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
