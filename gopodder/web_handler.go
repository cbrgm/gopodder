package gopodder

import (
	"cmp"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cbrgm/gopodder/gopodder/web"
	"github.com/dustin/go-humanize"
	"github.com/google/uuid"
)

const (
	RoleAdmin    = "admin"
	RoleStandard = "standard"

	SettingSelfRegistration   = "self_registration"
	SettingAllowUserCreation  = "allow_user_creation"
	SettingAllowSharing       = "allow_sharing"
	SettingAllowAPIKeys       = "allow_api_keys"
	SettingMaxUsersPerAccount = "max_users_per_account"
	SettingMaxAPIKeys         = "max_api_keys_per_account"
	SettingMinPasswordLength  = "min_password_length"
	SettingSessionMaxAge      = "session_max_age_hours"

	defaultSessionMaxAgeHours = 168
	defaultMaxAPIKeys         = 25
)

type WebHandler struct {
	logger     *slog.Logger
	store      Store
	build      BuildInfo
	listenAddr string
	dbBackend  string
	startedAt  time.Time
}

func NewWebHandler(api *API) *WebHandler {
	return &WebHandler{
		logger:     api.logger,
		store:      api.store,
		build:      api.build,
		listenAddr: api.listenAddr,
		dbBackend:  api.dbBackend,
		startedAt:  api.startedAt,
	}
}

type webContextKey string

const webContextKeyAccount webContextKey = "web_account"

func (h *WebHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /setup", h.handleSetupPage)
	mux.HandleFunc("POST /setup", h.handleSetupSubmit)
	mux.HandleFunc("GET /login", h.handleLoginPage)
	mux.HandleFunc("POST /login", h.handleLoginSubmit)
	mux.HandleFunc("POST /logout", h.handleLogout)
	mux.HandleFunc("GET /register", h.handleRegisterPage)
	mux.HandleFunc("POST /register", h.handleRegisterSubmit)
	mux.HandleFunc("GET /{$}", h.withSession(h.handleDashboard))

	// Self-service: Account
	mux.HandleFunc("GET /account", h.withSession(h.handleSelfAccountPage))
	mux.HandleFunc("POST /account/password", h.withSession(h.handleSelfChangeAccountPassword))
	mux.HandleFunc("POST /account/delete", h.withSession(h.handleSelfDeleteAccount))
	mux.HandleFunc("POST /account/keys", h.withSession(h.handleCreateAPIKey))
	mux.HandleFunc("POST /account/keys/{id}/delete", h.withSession(h.handleDeleteAPIKey))

	// Self-service: gPodder Users
	mux.HandleFunc("GET /users", h.withSession(h.handleSelfUsersPage))
	mux.HandleFunc("POST /users", h.withSession(h.handleSelfCreateUser))
	mux.HandleFunc("GET /users/{username}", h.withSession(h.handleSelfUserDetail))
	mux.HandleFunc("POST /users/{username}/password", h.withSession(h.handleSelfChangeUserPassword))
	mux.HandleFunc("POST /users/{username}/delete", h.withSession(h.handleSelfDeleteUser))
	mux.HandleFunc("POST /users/{username}/devices/{device}/delete", h.withSession(h.handleSelfDeleteDevice))
	mux.HandleFunc("POST /users/{username}/subscriptions/delete", h.withSession(h.handleSelfDeleteSubscriptions))
	mux.HandleFunc("POST /users/{username}/subscriptions/delete-one", h.withSession(h.handleSelfDeleteSubscription))
	mux.HandleFunc("POST /users/{username}/subscriptions/add", h.withSession(h.handleSelfAddSubscription))
	mux.HandleFunc("POST /users/{username}/subscriptions/import", h.withSession(h.handleSelfImportOPML))
	mux.HandleFunc("POST /users/{username}/sharing/enable", h.withSession(h.handleSelfEnableSharing))
	mux.HandleFunc("POST /users/{username}/sharing/disable", h.withSession(h.handleSelfDisableSharing))

	// Admin: Accounts
	mux.HandleFunc("GET /admin/accounts", h.withAdmin(h.handleAccountsPage))
	mux.HandleFunc("POST /admin/accounts", h.withAdmin(h.handleCreateAccount))
	mux.HandleFunc("GET /admin/accounts/{id}", h.withAdmin(h.handleAccountEditPage))
	mux.HandleFunc("POST /admin/accounts/{id}", h.withAdmin(h.handleUpdateAccount))
	mux.HandleFunc("POST /admin/accounts/{id}/password", h.withAdmin(h.handleChangeAccountPassword))
	mux.HandleFunc("POST /admin/accounts/{id}/delete", h.withAdmin(h.handleDeleteAccount))
	mux.HandleFunc("POST /admin/accounts/{id}/keys/{keyId}/delete", h.withAdmin(h.handleAdminDeleteAPIKey))

	// Admin: gPodder Users (under account)
	mux.HandleFunc("POST /admin/accounts/{id}/users", h.withAdmin(h.handleCreateUser))
	mux.HandleFunc("GET /admin/accounts/{id}/users/{username}", h.withAdmin(h.handleUserDetail))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/password", h.withAdmin(h.handleChangeUserPassword))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/delete", h.withAdmin(h.handleDeleteUser))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/devices/{device}/delete", h.withAdmin(h.handleDeleteDevice))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/subscriptions/delete", h.withAdmin(h.handleDeleteSubscriptions))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/subscriptions/delete-one", h.withAdmin(h.handleDeleteSingleSubscription))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/subscriptions/add", h.withAdmin(h.handleAdminAddSubscription))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/subscriptions/import", h.withAdmin(h.handleAdminImportOPML))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/sharing/enable", h.withAdmin(h.handleAdminEnableSharing))
	mux.HandleFunc("POST /admin/accounts/{id}/users/{username}/sharing/disable", h.withAdmin(h.handleAdminDisableSharing))

	// Admin: Settings
	mux.HandleFunc("GET /admin/settings", h.withAdmin(h.handleSettingsPage))
	mux.HandleFunc("POST /admin/settings", h.withAdmin(h.handleSettingsSave))

	// Status
	mux.HandleFunc("GET /status", h.withSession(h.handleStatusPage))

	mux.HandleFunc("GET /opml", h.withSession(h.handleOPML))

	// Public sharing (no auth)
	mux.HandleFunc("GET /user/{username}/subscriptions.opml", h.handlePublicOPML)
	mux.HandleFunc("GET /user/{username}/subscriptions/rss", h.handlePublicRSS)
}

func (h *WebHandler) SetupGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/setup" || hasAPIPrefix(r.URL.Path) || r.URL.Path == "/login" || r.URL.Path == "/register" || strings.HasPrefix(r.URL.Path, "/user/") {
			next.ServeHTTP(w, r)
			return
		}
		count, err := h.store.CountAccounts(r.Context())
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if count == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Pages

func (h *WebHandler) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	count, _ := h.store.CountAccounts(r.Context())
	if count > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = web.SetupPage("").Render(r.Context(), w)
}

func (h *WebHandler) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	count, _ := h.store.CountAccounts(r.Context())
	if count > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	password2 := r.FormValue("password2")

	if username == "" || password == "" {
		_ = web.SetupPage("Username and password are required.").Render(r.Context(), w)
		return
	}
	if !isValidUsername(username) {
		_ = web.SetupPage("Username must be 1-64 characters and contain only letters, numbers, dots, dashes, or underscores.").Render(r.Context(), w)
		return
	}
	if password != password2 {
		_ = web.SetupPage("Passwords do not match.").Render(r.Context(), w)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		_ = web.SetupPage(msg).Render(r.Context(), w)
		return
	}

	id := uuid.New().String()
	if err := h.store.CreateAccount(r.Context(), id, username, hashPassword(password), RoleAdmin, time.Now()); err != nil {
		h.logger.Error("failed to create initial admin account", "err", err)
		_ = web.SetupPage("Failed to create account.").Render(r.Context(), w)
		return
	}

	h.logger.Info("initial admin account created", "username", username)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *WebHandler) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	_ = web.LoginPage("", h.isSettingEnabled(r.Context(), SettingSelfRegistration), h.build.Version).Render(r.Context(), w)
}

func (h *WebHandler) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	showRegister := h.isSettingEnabled(r.Context(), SettingSelfRegistration)

	acct, err := h.store.GetAccount(r.Context(), username)
	if err != nil || !checkPassword(acct.PWHash, password) {
		h.logger.Warn("failed login attempt", "username", username)
		_ = web.LoginPage("Invalid username or password.", showRegister, h.build.Version).Render(r.Context(), w)
		return
	}

	sessionID := uuid.New().String()
	if err := h.store.UpdateAccountSession(r.Context(), acct.ID, &sessionID, time.Now()); err != nil {
		h.logger.Error("failed to create session", "err", err, "username", username)
		_ = web.LoginPage("Internal error.", showRegister, h.build.Version).Render(r.Context(), w)
		return
	}

	_ = h.store.UpdateAccountLastLogin(r.Context(), acct.ID, time.Now())

	maxAge := cmp.Or(h.getSettingInt(r.Context(), SettingSessionMaxAge), defaultSessionMaxAgeHours)
	http.SetCookie(w, &http.Cookie{
		Name:     "web_session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge) * 3600,
	})
	h.logger.Info("account logged in", "username", username)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *WebHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("web_session")
	if err == nil && cookie.Value != "" {
		acct, err := h.store.GetAccountBySession(r.Context(), cookie.Value)
		if err == nil {
			_ = h.store.UpdateAccountSession(r.Context(), acct.ID, nil, time.Time{})
			h.logger.Info("account logged out", "username", acct.Username)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "web_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *WebHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// Self-service: Account

func (h *WebHandler) handleSelfAccountPage(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())

	var keys []web.APIKeyData
	if h.apiKeysAllowed(r.Context(), acct) {
		stored, _ := h.store.ListAPIKeysByAccount(r.Context(), acct.ID)
		keys = make([]web.APIKeyData, 0, len(stored))
		for _, k := range stored {
			keys = append(keys, web.APIKeyData{
				ID:        k.ID,
				Name:      k.Name,
				Prefix:    k.Prefix,
				Role:      k.Role,
				CreatedAt: formatTimestamp(k.CreatedAt),
				LastUsed:  optionalTimeAgo(k.LastUsed),
			})
		}
	}

	var newKey string
	if c, err := r.Cookie("api_key_flash"); err == nil && c.Value != "" {
		newKey = c.Value
		http.SetCookie(w, &http.Cookie{
			Name:   "api_key_flash",
			Value:  "",
			Path:   "/account",
			MaxAge: -1,
		})
	}

	data := web.AccountSelfData{
		Account:        acct.Username,
		IsAdmin:        acct.Role == RoleAdmin,
		Flash:          r.URL.Query().Get("flash"),
		Error:          r.URL.Query().Get("error"),
		APIKeys:        keys,
		APIKeysAllowed: h.apiKeysAllowed(r.Context(), acct),
		NewKey:         newKey,
	}
	_ = web.AccountSelfPage(data).Render(r.Context(), w)
}

func (h *WebHandler) handleSelfChangeAccountPassword(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	current := r.FormValue("current_password")
	password := r.FormValue("password")
	password2 := r.FormValue("password2")

	if !checkPassword(acct.PWHash, current) {
		http.Redirect(w, r, "/account?error=Current+password+is+incorrect.", http.StatusSeeOther)
		return
	}
	if password == "" || password != password2 {
		http.Redirect(w, r, "/account?error=New+passwords+do+not+match.", http.StatusSeeOther)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		http.Redirect(w, r, "/account?error="+msg, http.StatusSeeOther)
		return
	}
	_ = h.store.UpdateAccountPassword(r.Context(), acct.ID, hashPassword(password))
	http.Redirect(w, r, "/account?flash=Password+updated.", http.StatusSeeOther)
}

func (h *WebHandler) handleSelfDeleteAccount(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())

	if acct.Role == RoleAdmin && h.isLastAdmin(r.Context(), acct.ID) {
		http.Redirect(w, r, "/account?error=Cannot+delete+the+last+admin+account.", http.StatusSeeOther)
		return
	}

	h.deleteAccountCascade(r.Context(), acct.ID)
	h.logger.Info("account self-deleted", "username", acct.Username)
	http.SetCookie(w, &http.Cookie{
		Name:   "web_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Self-service: API Keys

func (h *WebHandler) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())

	if !h.apiKeysAllowed(r.Context(), acct) {
		http.Redirect(w, r, "/account?error=API+key+creation+is+disabled.", http.StatusSeeOther)
		return
	}

	if acct.Role != RoleAdmin {
		maxKeys := cmp.Or(h.getSettingInt(r.Context(), SettingMaxAPIKeys), defaultMaxAPIKeys)
		count, _ := h.store.CountAPIKeysByAccount(r.Context(), acct.ID)
		if count >= maxKeys {
			http.Redirect(w, r, "/account?error=Maximum+number+of+API+keys+reached.", http.StatusSeeOther)
			return
		}
	}

	name := r.FormValue("name")
	if name == "" || len(name) > 64 {
		http.Redirect(w, r, "/account?error=Key+name+must+be+1-64+characters.", http.StatusSeeOther)
		return
	}

	role := r.FormValue("role")
	if role != RoleAdmin || acct.Role != RoleAdmin {
		role = RoleStandard
	}

	raw, prefix, hash, err := generateAPIKey()
	if err != nil {
		h.logger.Error("failed to generate API key", "err", err)
		http.Redirect(w, r, "/account?error=Failed+to+generate+API+key.", http.StatusSeeOther)
		return
	}

	key := APIKey{
		ID:        uuid.New().String(),
		AccountID: acct.ID,
		Name:      name,
		Prefix:    prefix,
		Hash:      hash,
		Role:      role,
		CreatedAt: time.Now(),
	}
	if err := h.store.CreateAPIKey(r.Context(), key); err != nil {
		h.logger.Error("failed to store API key", "err", err)
		http.Redirect(w, r, "/account?error=Failed+to+create+API+key.", http.StatusSeeOther)
		return
	}

	h.logger.Info("API key created", "name", name, "role", role, "account", acct.Username)
	http.SetCookie(w, &http.Cookie{
		Name:     "api_key_flash",
		Value:    raw,
		Path:     "/account",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   60,
	})
	http.Redirect(w, r, "/account?flash=API+key+created.+Copy+it+now+-+it+will+not+be+shown+again.", http.StatusSeeOther)
}

func (h *WebHandler) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	id := r.PathValue("id")
	_ = h.store.DeleteAPIKey(r.Context(), id, acct.ID)
	http.Redirect(w, r, "/account?flash=API+key+deleted.", http.StatusSeeOther)
}

func (h *WebHandler) apiKeysAllowed(ctx context.Context, acct *Account) bool {
	if acct.Role == RoleAdmin {
		return true
	}
	return h.isSettingEnabled(ctx, SettingAllowAPIKeys)
}

// Self-service: gPodder Users

func (h *WebHandler) handleSelfUsersPage(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	usersData := h.buildUsersData(r.Context(), acct.ID)
	canCreate := acct.Role == RoleAdmin || h.isSettingEnabled(r.Context(), SettingAllowUserCreation)

	data := web.UserManagementData{
		Account:        acct.Username,
		IsAdmin:        acct.Role == RoleAdmin,
		Users:          usersData,
		Flash:          r.URL.Query().Get("flash"),
		Error:          r.URL.Query().Get("error"),
		CanCreateUsers: canCreate,
	}
	_ = web.UserManagementPage(data).Render(r.Context(), w)
}

func (h *WebHandler) handleSelfUserDetail(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	acct := webAccountFromContext(r.Context())
	data := h.buildUserDetailData(r.Context(), r, acct, username, "users", "/users/"+username, "/users", "Back to Users")
	data.Flash = r.URL.Query().Get("flash")
	data.Error = r.URL.Query().Get("error")
	_ = web.UserDetailPage(data).Render(r.Context(), w)
}

func (h *WebHandler) handleSelfCreateUser(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	if acct.Role != RoleAdmin && !h.isSettingEnabled(r.Context(), SettingAllowUserCreation) {
		http.Redirect(w, r, "/users?error=User+creation+is+disabled.", http.StatusSeeOther)
		return
	}
	if h.userLimitReached(r.Context(), acct.ID) {
		http.Redirect(w, r, "/users?error=User+limit+reached.+Cannot+create+more+users.", http.StatusSeeOther)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		http.Redirect(w, r, "/users?error=Username+and+password+are+required.", http.StatusSeeOther)
		return
	}
	if !isValidUsername(username) {
		http.Redirect(w, r, "/users?error=Username+must+be+1-64+characters+using+only+letters,+numbers,+dots,+dashes,+or+underscores.", http.StatusSeeOther)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		http.Redirect(w, r, "/users?error="+msg, http.StatusSeeOther)
		return
	}
	if _, err := h.store.GetUser(r.Context(), username); err == nil {
		http.Redirect(w, r, "/users?error=Username+already+exists.+Please+choose+a+different+name.", http.StatusSeeOther)
		return
	}
	if err := h.store.CreateUser(r.Context(), username, hashPassword(password), acct.ID); err != nil {
		h.logger.Error("failed to create gpodder user", "err", err, "username", username, "account", acct.Username)
		http.Redirect(w, r, "/users?error=Failed+to+create+user.", http.StatusSeeOther)
		return
	}
	h.logger.Info("gpodder user created", "username", username, "account", acct.Username)
	http.Redirect(w, r, "/users?flash=User+created.", http.StatusSeeOther)
}

func (h *WebHandler) handleSelfChangeUserPassword(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}

	password := r.FormValue("password")
	password2 := r.FormValue("password2")
	if password == "" || password != password2 {
		http.Redirect(w, r, "/users/"+username+"?error=Passwords+do+not+match.", http.StatusSeeOther)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		http.Redirect(w, r, "/users/"+username+"?error="+msg, http.StatusSeeOther)
		return
	}
	if err := h.store.UpdateUserPassword(r.Context(), username, hashPassword(password)); err != nil {
		h.logger.Error("failed to update gpodder user password", "err", err, "username", username)
	}
	http.Redirect(w, r, "/users/"+username+"?flash=Password+updated.", http.StatusSeeOther)
}

func (h *WebHandler) handleSelfDeleteUser(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	acct := webAccountFromContext(r.Context())
	h.deleteUserCascade(r.Context(), username)
	h.logger.Info("gpodder user deleted", "username", username, "account", acct.Username)
	http.Redirect(w, r, "/users?flash=User+deleted.", http.StatusSeeOther)
}

func (h *WebHandler) handleSelfDeleteDevice(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	_ = h.store.DeleteDevice(r.Context(), username, r.PathValue("device"))
	http.Redirect(w, r, "/users/"+username, http.StatusSeeOther)
}

func (h *WebHandler) handleSelfDeleteSubscriptions(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	subs, _ := h.store.GetSubscriptions(r.Context(), username)
	if len(subs) > 0 {
		_ = h.store.UpdateSubscriptions(r.Context(), username, nil, subs, time.Now().Unix())
	}
	http.Redirect(w, r, "/users/"+username, http.StatusSeeOther)
}

func (h *WebHandler) handleSelfDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	h.deleteSubscription(r.Context(), username, r.FormValue("url"))
	http.Redirect(w, r, "/users/"+username, http.StatusSeeOther)
}

func (h *WebHandler) handleSelfAddSubscription(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	redirect := "/users/" + username
	if err := h.addSubscription(r.Context(), username, r); err != "" {
		http.Redirect(w, r, redirect+"?error="+err, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Subscribed+to+feed.", http.StatusSeeOther)
}

func (h *WebHandler) handleSelfImportOPML(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	redirect := "/users/" + username
	n, err := h.importOPML(r.Context(), username, r)
	if err != nil {
		http.Redirect(w, r, redirect+"?error=Invalid+OPML+file.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Imported+"+fmt.Sprint(n)+"+subscriptions.", http.StatusSeeOther)
}

func (h *WebHandler) userBelongsToAccount(ctx context.Context, username, accountID string) bool {
	user, err := h.store.GetUser(ctx, username)
	if err != nil {
		return false
	}
	return user.AccountID == accountID
}

func (h *WebHandler) requireOwnUser(w http.ResponseWriter, r *http.Request) (username string, ok bool) {
	acct := webAccountFromContext(r.Context())
	username = r.PathValue("username")
	if !h.userBelongsToAccount(r.Context(), username, acct.ID) {
		http.Redirect(w, r, "/users", http.StatusSeeOther)
		return "", false
	}
	return username, true
}

func (h *WebHandler) deleteUserCascade(ctx context.Context, username string) {
	deleteUserCascade(ctx, h.store, username)
}

func (h *WebHandler) deleteAccountCascade(ctx context.Context, accountID string) {
	deleteAccountCascade(ctx, h.store, accountID)
}

func deleteUserCascade(ctx context.Context, store Store, username string) {
	_ = store.DeleteAllUserEpisodes(ctx, username)
	_ = store.DeleteAllUserSubscriptions(ctx, username)
	_ = store.DeleteAllUserDevices(ctx, username)
	_ = store.DeleteUser(ctx, username)
}

func deleteAccountCascade(ctx context.Context, store Store, accountID string) {
	users, _ := store.ListUsersByAccount(ctx, accountID)
	for _, u := range users {
		deleteUserCascade(ctx, store, u.Username)
	}
	_ = store.DeleteAPIKeysByAccount(ctx, accountID)
	_ = store.DeleteAccount(ctx, accountID)
}

func (h *WebHandler) deleteSubscription(ctx context.Context, username, url string) {
	if url == "" {
		return
	}
	_ = h.store.UpdateSubscriptions(ctx, username, nil, []string{url}, time.Now().Unix())
}

func (h *WebHandler) addSubscription(ctx context.Context, username string, r *http.Request) (errMsg string) {
	url := strings.TrimSpace(r.FormValue("url"))
	if url == "" {
		return "Feed+URL+is+required."
	}
	if !isValidFeedURL(url) {
		return "Invalid+URL.+Only+http+and+https+URLs+are+allowed."
	}
	now := time.Now().Unix()
	_ = h.store.ReactivateSubscription(ctx, username, url, now)
	_ = h.store.UpdateSubscriptions(ctx, username, []string{url}, nil, now)
	return ""
}

func (h *WebHandler) importOPML(ctx context.Context, username string, r *http.Request) (int, error) {
	urls, err := h.parseOPMLUpload(r)
	if err != nil {
		return 0, err
	}
	urls = filterValidURLs(urls)
	now := time.Now().Unix()
	for _, u := range urls {
		_ = h.store.ReactivateSubscription(ctx, username, u, now)
	}
	_ = h.store.UpdateSubscriptions(ctx, username, urls, nil, now)
	return len(urls), nil
}

func (h *WebHandler) buildUsersData(ctx context.Context, accountID string) []web.UserData {
	users, _ := h.store.ListUsersByAccountWithStats(ctx, accountID)
	usersData := make([]web.UserData, 0, len(users))
	for _, u := range users {
		usersData = append(usersData, web.UserData{
			Username:      u.Username,
			Devices:       u.Devices,
			Subscriptions: u.Subscriptions,
			LastActivity:  optionalTimeAgo(u.LastActivity),
		})
	}
	return usersData
}

func (h *WebHandler) buildUserDetailData(ctx context.Context, r *http.Request, acct *Account, username, activeTab, basePath, backURL, backLabel string) web.UserDetailData {
	devices, _ := h.store.ListDevices(ctx, username)
	subs, _ := h.store.GetSubscriptions(ctx, username)
	user, _ := h.store.GetUser(ctx, username)

	devData := make([]web.DeviceData, 0, len(devices))
	for _, d := range devices {
		devData = append(devData, web.DeviceData{
			ID:           d.ID,
			Caption:      d.Caption,
			Type:         d.Type,
			LastActivity: optionalTimeAgo(d.LastActivity),
		})
	}

	var shareToken, shareOPMLURL, shareRSSURL string
	if user != nil && user.ShareToken != nil {
		shareToken = *user.ShareToken
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base := scheme + "://" + r.Host
		shareOPMLURL = base + "/user/" + username + "/subscriptions.opml?token=" + shareToken
		shareRSSURL = base + "/user/" + username + "/subscriptions/rss?token=" + shareToken
	}

	return web.UserDetailData{
		Account:        acct.Username,
		AccountID:      acct.ID,
		IsAdmin:        acct.Role == RoleAdmin,
		ActiveTab:      activeTab,
		BasePath:       basePath,
		BackURL:        backURL,
		BackLabel:      backLabel,
		Username:       username,
		Devices:        devData,
		Subscriptions:  subs,
		ShareToken:     shareToken,
		ShareOPMLURL:   shareOPMLURL,
		ShareRSSURL:    shareRSSURL,
		SharingAllowed: h.isSettingEnabled(ctx, SettingAllowSharing),
	}
}

// Status

func (h *WebHandler) handleStatusPage(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	stats, _ := h.store.GetStats(r.Context())

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	data := web.StatusData{
		Account: acct.Username,
		IsAdmin: acct.Role == RoleAdmin,
		Stats: web.StatsData{
			Accounts:      stats.Accounts,
			Users:         stats.Users,
			Devices:       stats.Devices,
			Subscriptions: stats.Subscriptions,
			Episodes:      stats.Episodes,
		},
		Uptime:     time.Since(h.startedAt).Truncate(time.Second).String(),
		MemAlloc:   formatBytes(mem.Alloc),
		Goroutines: runtime.NumGoroutine(),
		Version:    h.build.Version,
		Revision:   h.build.Revision,
		BuildDate:  h.build.BuildDate,
		GoVersion:  h.build.GoVersion,
		Platform:   h.build.Platform,
		ListenAddr: h.listenAddr,
		DBBackend:  h.dbBackend,
	}
	_ = web.StatusPage(data).Render(r.Context(), w)
}

// Admin: Accounts

func (h *WebHandler) handleAccountsPage(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	accounts, err := h.store.ListAccounts(r.Context())
	if err != nil {
		h.logger.Error("failed to list accounts", "err", err)
	}

	accountsData := make([]web.AccountData, 0, len(accounts))
	for _, a := range accounts {
		accountsData = append(accountsData, web.AccountData{
			ID:           a.ID,
			Username:     a.Username,
			Role:         a.Role,
			Users:        a.UserCount,
			CreatedAt:    formatTimestamp(a.CreatedAt),
			LastLogin:    optionalTimeAgo(a.LastLogin),
			LastActivity: optionalTimeAgo(a.LastActivity),
		})
	}

	data := web.AccountsPageData{
		Account:  acct.Username,
		Accounts: accountsData,
		Flash:    r.URL.Query().Get("flash"),
		Error:    r.URL.Query().Get("error"),
	}
	_ = web.AccountsPage(data).Render(r.Context(), w)
}

func (h *WebHandler) handleAccountEditPage(w http.ResponseWriter, r *http.Request) {
	currentAcct := webAccountFromContext(r.Context())
	id := r.PathValue("id")

	acct, err := h.store.GetAccountByID(r.Context(), id)
	if err != nil {
		http.Redirect(w, r, "/admin/accounts", http.StatusSeeOther)
		return
	}

	stored, _ := h.store.ListAPIKeysByAccount(r.Context(), id)
	keys := make([]web.APIKeyData, 0, len(stored))
	for _, k := range stored {
		keys = append(keys, web.APIKeyData{
			ID:        k.ID,
			Name:      k.Name,
			Prefix:    k.Prefix,
			Role:      k.Role,
			CreatedAt: formatTimestamp(k.CreatedAt),
			LastUsed:  optionalTimeAgo(k.LastUsed),
		})
	}

	data := web.AccountEditData{
		Account:     currentAcct.Username,
		EditAccount: web.AccountData{ID: acct.ID, Username: acct.Username, Role: acct.Role},
		Users:       h.buildUsersData(r.Context(), id),
		APIKeys:     keys,
		Flash:       r.URL.Query().Get("flash"),
		Error:       r.URL.Query().Get("error"),
	}
	_ = web.AccountEditPage(data).Render(r.Context(), w)
}

func (h *WebHandler) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	role := r.FormValue("role")
	if username == "" || password == "" {
		http.Redirect(w, r, "/admin/accounts?error=Username+and+password+are+required.", http.StatusSeeOther)
		return
	}
	if !isValidUsername(username) {
		http.Redirect(w, r, "/admin/accounts?error=Username+must+be+1-64+characters+using+only+letters,+numbers,+dots,+dashes,+or+underscores.", http.StatusSeeOther)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		http.Redirect(w, r, "/admin/accounts?error="+msg, http.StatusSeeOther)
		return
	}
	if role != RoleAdmin {
		role = RoleStandard
	}
	if _, err := h.store.GetAccount(r.Context(), username); err == nil {
		http.Redirect(w, r, "/admin/accounts?error=Username+already+exists.", http.StatusSeeOther)
		return
	}
	id := uuid.New().String()
	if err := h.store.CreateAccount(r.Context(), id, username, hashPassword(password), role, time.Now()); err != nil {
		h.logger.Error("failed to create account", "err", err, "username", username)
		http.Redirect(w, r, "/admin/accounts?error=Failed+to+create+account.", http.StatusSeeOther)
		return
	}
	h.logger.Info("account created", "username", username, "role", role)
	http.Redirect(w, r, "/admin/accounts?flash=Account+created.", http.StatusSeeOther)
}

func (h *WebHandler) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	username := r.FormValue("username")
	role := r.FormValue("role")

	if username != "" {
		if !isValidUsername(username) {
			http.Redirect(w, r, "/admin/accounts/"+id+"?error=Invalid+username.", http.StatusSeeOther)
			return
		}
		_ = h.store.UpdateAccountUsername(r.Context(), id, username)
	}
	if role == RoleAdmin || role == RoleStandard {
		if role == RoleStandard && h.isLastAdmin(r.Context(), id) {
			http.Redirect(w, r, "/admin/accounts/"+id+"?error=Cannot+demote+the+last+admin+account.", http.StatusSeeOther)
			return
		}
		_ = h.store.UpdateAccountRole(r.Context(), id, role)
	}
	http.Redirect(w, r, "/admin/accounts/"+id+"?flash=Account+updated.", http.StatusSeeOther)
}

func (h *WebHandler) handleChangeAccountPassword(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	password := r.FormValue("password")
	password2 := r.FormValue("password2")
	if password == "" || password != password2 {
		http.Redirect(w, r, "/admin/accounts/"+id+"?error=Passwords+do+not+match.", http.StatusSeeOther)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		http.Redirect(w, r, "/admin/accounts/"+id+"?error="+msg, http.StatusSeeOther)
		return
	}
	_ = h.store.UpdateAccountPassword(r.Context(), id, hashPassword(password))
	http.Redirect(w, r, "/admin/accounts/"+id+"?flash=Password+updated.", http.StatusSeeOther)
}

func (h *WebHandler) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if h.isLastAdmin(r.Context(), id) {
		http.Redirect(w, r, "/admin/accounts/"+id+"?error=Cannot+delete+the+last+admin+account.", http.StatusSeeOther)
		return
	}

	acct, _ := h.store.GetAccountByID(r.Context(), id)
	h.deleteAccountCascade(r.Context(), id)
	username := ""
	if acct != nil {
		username = acct.Username
	}
	h.logger.Info("account deleted", "username", username)
	http.Redirect(w, r, "/admin/accounts?flash=Account+deleted.", http.StatusSeeOther)
}

func (h *WebHandler) handleAdminDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	keyID := r.PathValue("keyId")
	_ = h.store.DeleteAPIKey(r.Context(), keyID, accountID)
	h.logger.Info("API key revoked by admin", "key_id", keyID, "account_id", accountID)
	http.Redirect(w, r, "/admin/accounts/"+accountID+"?flash=API+key+revoked.", http.StatusSeeOther)
}

// Admin: gPodder Users (under account)

func (h *WebHandler) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	basePath := "/admin/accounts/" + accountID + "/users/" + username

	data := h.buildUserDetailData(r.Context(), r, acct, username, "accounts", basePath, "/admin/accounts/"+accountID, "Back to Account")
	data.AccountID = accountID
	if target, err := h.store.GetAccountByID(r.Context(), accountID); err == nil {
		data.AccountName = target.Username
	}
	data.Flash = r.URL.Query().Get("flash")
	data.Error = r.URL.Query().Get("error")
	_ = web.UserDetailPage(data).Render(r.Context(), w)
}

func (h *WebHandler) handleChangeUserPassword(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	password := r.FormValue("password")
	password2 := r.FormValue("password2")
	redirect := "/admin/accounts/" + accountID + "/users/" + username
	if password == "" || password != password2 {
		http.Redirect(w, r, redirect+"?error=Passwords+do+not+match.", http.StatusSeeOther)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		http.Redirect(w, r, redirect+"?error="+msg, http.StatusSeeOther)
		return
	}
	if err := h.store.UpdateUserPassword(r.Context(), username, hashPassword(password)); err != nil {
		h.logger.Error("failed to update gpodder user password", "err", err, "username", username)
	}
	http.Redirect(w, r, redirect+"?flash=Password+updated.", http.StatusSeeOther)
}

func (h *WebHandler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		http.Redirect(w, r, "/admin/accounts/"+accountID+"?error=Username+and+password+are+required.", http.StatusSeeOther)
		return
	}
	if !isValidUsername(username) {
		http.Redirect(w, r, "/admin/accounts/"+accountID+"?error=Username+must+be+1-64+characters+using+only+letters,+numbers,+dots,+dashes,+or+underscores.", http.StatusSeeOther)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		http.Redirect(w, r, "/admin/accounts/"+accountID+"?error="+msg, http.StatusSeeOther)
		return
	}
	if h.userLimitReached(r.Context(), accountID) {
		http.Redirect(w, r, "/admin/accounts/"+accountID+"?error=User+limit+reached.+Cannot+create+more+users.", http.StatusSeeOther)
		return
	}
	if _, err := h.store.GetUser(r.Context(), username); err == nil {
		http.Redirect(w, r, "/admin/accounts/"+accountID+"?error=Username+already+exists.+Please+choose+a+different+name.", http.StatusSeeOther)
		return
	}
	if err := h.store.CreateUser(r.Context(), username, hashPassword(password), accountID); err != nil {
		h.logger.Error("failed to create gpodder user", "err", err, "username", username)
		http.Redirect(w, r, "/admin/accounts/"+accountID+"?error=Failed+to+create+user.", http.StatusSeeOther)
		return
	}
	h.logger.Info("gpodder user created", "username", username, "account_id", accountID)
	http.Redirect(w, r, "/admin/accounts/"+accountID+"?flash=User+created.", http.StatusSeeOther)
}

func (h *WebHandler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	h.deleteUserCascade(r.Context(), username)
	h.logger.Info("gpodder user deleted by admin", "username", username, "account_id", accountID)
	http.Redirect(w, r, "/admin/accounts/"+accountID, http.StatusSeeOther)
}

func (h *WebHandler) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	device := r.PathValue("device")
	_ = h.store.DeleteDevice(r.Context(), username, device)
	h.logger.Info("device deleted", "username", username, "device", device)
	http.Redirect(w, r, "/admin/accounts/"+accountID+"/users/"+username, http.StatusSeeOther)
}

func (h *WebHandler) handleDeleteSubscriptions(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	subs, _ := h.store.GetSubscriptions(r.Context(), username)
	if len(subs) > 0 {
		_ = h.store.UpdateSubscriptions(r.Context(), username, nil, subs, time.Now().Unix())
	}
	http.Redirect(w, r, "/admin/accounts/"+accountID+"/users/"+username, http.StatusSeeOther)
}

func (h *WebHandler) handleDeleteSingleSubscription(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	h.deleteSubscription(r.Context(), username, r.FormValue("url"))
	http.Redirect(w, r, "/admin/accounts/"+accountID+"/users/"+username, http.StatusSeeOther)
}

func (h *WebHandler) handleAdminAddSubscription(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	redirect := "/admin/accounts/" + accountID + "/users/" + username

	if err := h.addSubscription(r.Context(), username, r); err != "" {
		http.Redirect(w, r, redirect+"?error="+err, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Subscribed+to+feed.", http.StatusSeeOther)
}

func (h *WebHandler) handleAdminImportOPML(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	redirect := "/admin/accounts/" + accountID + "/users/" + username

	n, err := h.importOPML(r.Context(), username, r)
	if err != nil {
		http.Redirect(w, r, redirect+"?error=Invalid+OPML+file.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirect+"?flash=Imported+"+fmt.Sprint(n)+"+subscriptions.", http.StatusSeeOther)
}

// Registration

func (h *WebHandler) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if !h.isSettingEnabled(r.Context(), SettingSelfRegistration) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = web.RegisterPage(web.RegisterPageData{}).Render(r.Context(), w)
}

func (h *WebHandler) handleRegisterSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.isSettingEnabled(r.Context(), SettingSelfRegistration) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	password2 := r.FormValue("password2")

	if username == "" || password == "" {
		_ = web.RegisterPage(web.RegisterPageData{Error: "Username and password are required."}).Render(r.Context(), w)
		return
	}
	if !isValidUsername(username) {
		_ = web.RegisterPage(web.RegisterPageData{Error: "Username must be 1-64 characters and contain only letters, numbers, dots, dashes, or underscores."}).Render(r.Context(), w)
		return
	}
	if password != password2 {
		_ = web.RegisterPage(web.RegisterPageData{Error: "Passwords do not match."}).Render(r.Context(), w)
		return
	}
	if msg := h.checkPasswordLength(r.Context(), password); msg != "" {
		_ = web.RegisterPage(web.RegisterPageData{Error: msg}).Render(r.Context(), w)
		return
	}

	if _, err := h.store.GetAccount(r.Context(), username); err == nil {
		_ = web.RegisterPage(web.RegisterPageData{Error: "Username already exists. Please choose a different name."}).Render(r.Context(), w)
		return
	}

	id := uuid.New().String()
	if err := h.store.CreateAccount(r.Context(), id, username, hashPassword(password), RoleStandard, time.Now()); err != nil {
		h.logger.Error("failed to create self-registered account", "err", err, "username", username)
		_ = web.RegisterPage(web.RegisterPageData{Error: "Failed to create account."}).Render(r.Context(), w)
		return
	}

	h.logger.Info("account self-registered", "username", username)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Admin: Settings

func (h *WebHandler) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	sessionMaxAge := cmp.Or(h.getSettingInt(r.Context(), SettingSessionMaxAge), defaultSessionMaxAgeHours)
	episodeRetention := cmp.Or(h.getSettingInt(r.Context(), SettingEpisodeRetention), defaultEpisodeRetentionDays)
	data := web.SettingsPageData{
		Account:             acct.Username,
		Flash:               r.URL.Query().Get("flash"),
		Error:               r.URL.Query().Get("error"),
		SelfRegistration:    h.isSettingEnabled(r.Context(), SettingSelfRegistration),
		AllowUserCreation:   h.isSettingEnabled(r.Context(), SettingAllowUserCreation),
		AllowSharing:        h.isSettingEnabled(r.Context(), SettingAllowSharing),
		AllowAPIKeys:        h.isSettingEnabled(r.Context(), SettingAllowAPIKeys),
		MaxUsersPerAccount:  h.getSettingInt(r.Context(), SettingMaxUsersPerAccount),
		MaxAPIKeys:          cmp.Or(h.getSettingInt(r.Context(), SettingMaxAPIKeys), defaultMaxAPIKeys),
		MinPasswordLength:   h.getSettingInt(r.Context(), SettingMinPasswordLength),
		SessionMaxAge:       sessionMaxAge,
		EpisodeRetention:    episodeRetention,
		InactiveAccountDays: h.getSettingInt(r.Context(), SettingInactiveAccountDays),
	}
	_ = web.SettingsPage(data).Render(r.Context(), w)
}

func (h *WebHandler) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessionMaxAge := parseFormInt(r.FormValue("session_max_age_hours"))
	episodeRetention := parseFormInt(r.FormValue("episode_retention_days"))
	inactiveAccountDays := parseFormInt(r.FormValue("inactive_account_days"))

	if sessionMaxAge < 1 {
		h.settingsError(w, r, "Session max age must be at least 1 hour.")
		return
	}
	if episodeRetention != 0 && (episodeRetention < minEpisodeRetentionDays || episodeRetention > maxEpisodeRetentionDays) {
		h.settingsError(w, r, fmt.Sprintf("Episode retention must be 0 (disabled) or between %d and %d days.", minEpisodeRetentionDays, maxEpisodeRetentionDays))
		return
	}
	if inactiveAccountDays != 0 && (inactiveAccountDays < minInactiveAccountDays || inactiveAccountDays > maxInactiveAccountDays) {
		h.settingsError(w, r, fmt.Sprintf("Inactive account days must be 0 (disabled) or between %d and %d days.", minInactiveAccountDays, maxInactiveAccountDays))
		return
	}

	_ = h.store.SetSetting(ctx, SettingSelfRegistration, boolString(r.FormValue("self_registration") == "true"))
	_ = h.store.SetSetting(ctx, SettingAllowUserCreation, boolString(r.FormValue("allow_user_creation") == "true"))
	_ = h.store.SetSetting(ctx, SettingAllowSharing, boolString(r.FormValue("allow_sharing") == "true"))
	_ = h.store.SetSetting(ctx, SettingAllowAPIKeys, boolString(r.FormValue("allow_api_keys") == "true"))
	_ = h.store.SetSetting(ctx, SettingMaxUsersPerAccount, clampedIntString(r.FormValue("max_users_per_account")))
	_ = h.store.SetSetting(ctx, SettingMaxAPIKeys, clampedMinIntString(r.FormValue("max_api_keys_per_account"), 1))
	_ = h.store.SetSetting(ctx, SettingMinPasswordLength, clampedIntString(r.FormValue("min_password_length")))
	_ = h.store.SetSetting(ctx, SettingSessionMaxAge, strconv.Itoa(sessionMaxAge))
	_ = h.store.SetSetting(ctx, SettingEpisodeRetention, strconv.Itoa(episodeRetention))
	_ = h.store.SetSetting(ctx, SettingInactiveAccountDays, strconv.Itoa(inactiveAccountDays))

	h.logger.Info("settings updated")
	http.Redirect(w, r, "/admin/settings?flash=Settings+saved.", http.StatusSeeOther)
}

func (h *WebHandler) settingsError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/admin/settings?error="+strings.ReplaceAll(msg, " ", "+"), http.StatusSeeOther)
}

func parseFormInt(s string) int {
	n, _ := strconv.Atoi(s)
	if n < 0 {
		return 0
	}
	return n
}

func (h *WebHandler) isSettingEnabled(ctx context.Context, key string) bool {
	val, err := h.store.GetSetting(ctx, key)
	if err != nil {
		return key == SettingAllowUserCreation || key == SettingAllowSharing
	}
	return val == "true"
}

func (h *WebHandler) getSettingInt(ctx context.Context, key string) int64 {
	val, err := h.store.GetSetting(ctx, key)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(val, 10, 64)
	return n
}

func (h *WebHandler) checkPasswordLength(ctx context.Context, password string) string {
	minLen := cmp.Or(h.getSettingInt(ctx, SettingMinPasswordLength), 8)
	if int64(len(password)) < minLen {
		return fmt.Sprintf("Password must be at least %d characters.", minLen)
	}
	return ""
}

func (h *WebHandler) userLimitReached(ctx context.Context, accountID string) bool {
	limit := h.getSettingInt(ctx, SettingMaxUsersPerAccount)
	if limit <= 0 {
		return false
	}
	users, err := h.store.ListUsersByAccount(ctx, accountID)
	if err != nil {
		return false
	}
	return int64(len(users)) >= limit
}

// OPML

type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    opmlHead `xml:"head"`
	Body    opmlBody `xml:"body"`
}

type opmlHead struct {
	Title       string `xml:"title"`
	DateCreated string `xml:"dateCreated,omitempty"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	Type     string        `xml:"type,attr"`
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr,omitempty"`
	XMLURL   string        `xml:"xmlUrl,attr"`
	Outlines []opmlOutline `xml:"outline"`
}

func (h *WebHandler) handleOPML(w http.ResponseWriter, r *http.Request) {
	acct := webAccountFromContext(r.Context())
	username := r.URL.Query().Get("user")

	if username == "" {
		http.Error(w, "user parameter required", http.StatusBadRequest)
		return
	}

	if acct.Role != RoleAdmin && !h.userBelongsToAccount(r.Context(), username, acct.ID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	subs, err := h.store.GetSubscriptions(r.Context(), username)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	outlines := make([]opmlOutline, 0, len(subs))
	for _, u := range subs {
		outlines = append(outlines, opmlOutline{Type: "rss", Text: u, Title: u, XMLURL: u})
	}

	doc := opmlDoc{
		Version: "2.0",
		Head: opmlHead{
			Title:       fmt.Sprintf("goPodder subscriptions for %s", username),
			DateCreated: time.Now().UTC().Format(time.RFC1123Z),
		},
		Body: opmlBody{Outlines: outlines},
	}

	filename := fmt.Sprintf("%s-subscriptions.opml", username)

	w.Header().Set("Content-Type", "text/x-opml+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(doc)
}

func (h *WebHandler) parseOPMLUpload(r *http.Request) ([]string, error) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		return nil, err
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var doc opmlDoc
	if err := xml.NewDecoder(file).Decode(&doc); err != nil {
		return nil, err
	}

	var urls []string
	collectOPMLURLs(doc.Body.Outlines, &urls)
	return urls, nil
}

func collectOPMLURLs(outlines []opmlOutline, urls *[]string) {
	for _, o := range outlines {
		if o.XMLURL != "" {
			*urls = append(*urls, o.XMLURL)
		}
		collectOPMLURLs(o.Outlines, urls)
	}
}

// Sharing

func (h *WebHandler) handleSelfEnableSharing(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	if !h.isSettingEnabled(r.Context(), SettingAllowSharing) {
		http.Redirect(w, r, "/users/"+username, http.StatusSeeOther)
		return
	}
	_ = h.store.SetUserShareToken(r.Context(), username, new(uuid.New().String()))
	http.Redirect(w, r, "/users/"+username+"?flash=Share+links+generated.", http.StatusSeeOther)
}

func (h *WebHandler) handleSelfDisableSharing(w http.ResponseWriter, r *http.Request) {
	username, ok := h.requireOwnUser(w, r)
	if !ok {
		return
	}
	_ = h.store.SetUserShareToken(r.Context(), username, nil)
	http.Redirect(w, r, "/users/"+username+"?flash=Sharing+disabled.", http.StatusSeeOther)
}

func (h *WebHandler) handleAdminEnableSharing(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	if !h.isSettingEnabled(r.Context(), SettingAllowSharing) {
		http.Redirect(w, r, "/admin/accounts/"+accountID+"/users/"+username, http.StatusSeeOther)
		return
	}
	_ = h.store.SetUserShareToken(r.Context(), username, new(uuid.New().String()))
	http.Redirect(w, r, "/admin/accounts/"+accountID+"/users/"+username+"?flash=Share+links+generated.", http.StatusSeeOther)
}

func (h *WebHandler) handleAdminDisableSharing(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	username := r.PathValue("username")
	_ = h.store.SetUserShareToken(r.Context(), username, nil)
	http.Redirect(w, r, "/admin/accounts/"+accountID+"/users/"+username+"?flash=Sharing+disabled.", http.StatusSeeOther)
}

func (h *WebHandler) handlePublicOPML(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	token := r.URL.Query().Get("token")

	if token == "" || !h.isSettingEnabled(r.Context(), SettingAllowSharing) {
		http.NotFound(w, r)
		return
	}

	user, err := h.store.GetUserByShareToken(r.Context(), token)
	if err != nil || user.Username != username {
		http.NotFound(w, r)
		return
	}

	subs, err := h.store.GetSubscriptions(r.Context(), username)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	outlines := make([]opmlOutline, 0, len(subs))
	for _, u := range subs {
		outlines = append(outlines, opmlOutline{Type: "rss", Text: u, Title: u, XMLURL: u})
	}

	doc := opmlDoc{
		Version: "2.0",
		Head: opmlHead{
			Title:       fmt.Sprintf("goPodder subscriptions for %s", username),
			DateCreated: time.Now().UTC().Format(time.RFC1123Z),
		},
		Body: opmlBody{Outlines: outlines},
	}

	w.Header().Set("Content-Type", "text/x-opml+xml")
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(doc)
}

func (h *WebHandler) handlePublicRSS(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	token := r.URL.Query().Get("token")

	if token == "" || !h.isSettingEnabled(r.Context(), SettingAllowSharing) {
		http.NotFound(w, r)
		return
	}

	user, err := h.store.GetUserByShareToken(r.Context(), token)
	if err != nil || user.Username != username {
		http.NotFound(w, r)
		return
	}

	subs, err := h.store.GetSubscriptions(r.Context(), username)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type rssItem struct {
		XMLName xml.Name `xml:"item"`
		Title   string   `xml:"title"`
		Link    string   `xml:"link"`
	}
	type rssChannel struct {
		XMLName     xml.Name  `xml:"channel"`
		Title       string    `xml:"title"`
		Description string    `xml:"description"`
		Items       []rssItem `xml:"item"`
	}
	type rssDoc struct {
		XMLName xml.Name   `xml:"rss"`
		Version string     `xml:"version,attr"`
		Channel rssChannel `xml:"channel"`
	}

	items := make([]rssItem, 0, len(subs))
	for _, u := range subs {
		items = append(items, rssItem{Title: u, Link: u})
	}

	doc := rssDoc{
		Version: "2.0",
		Channel: rssChannel{
			Title:       fmt.Sprintf("goPodder subscriptions for %s", username),
			Description: fmt.Sprintf("Podcast subscriptions for %s", username),
			Items:       items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml")
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(doc)
}

// Middleware

func (h *WebHandler) withSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acct := h.getSessionAccount(r)
		if acct == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(h.sessionContext(r, acct)))
	}
}

func (h *WebHandler) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acct := h.getSessionAccount(r)
		if acct == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if acct.Role != RoleAdmin {
			h.logger.Warn("unauthorized admin access attempt", "username", acct.Username, "path", r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r.WithContext(h.sessionContext(r, acct)))
	}
}

func (h *WebHandler) sessionContext(r *http.Request, acct *Account) context.Context {
	cookie, _ := r.Cookie("web_session")
	ctx := context.WithValue(r.Context(), webContextKeyAccount, acct)
	return context.WithValue(ctx, web.ContextKeyCSRFToken, generateCSRFToken(cookie.Value))
}

func (h *WebHandler) getSessionAccount(r *http.Request) *Account {
	cookie, err := r.Cookie("web_session")
	if err != nil || cookie.Value == "" {
		return nil
	}
	acct, err := h.store.GetAccountBySession(r.Context(), cookie.Value)
	if err != nil {
		return nil
	}
	if h.sessionExpired(r.Context(), acct.SessionCreated) {
		_ = h.store.UpdateAccountSession(r.Context(), acct.ID, nil, time.Time{})
		return nil
	}
	return acct
}

func (h *WebHandler) sessionExpired(ctx context.Context, sessionCreated *time.Time) bool {
	if sessionCreated == nil {
		return false
	}
	maxAge := cmp.Or(h.getSettingInt(ctx, SettingSessionMaxAge), defaultSessionMaxAgeHours)
	return time.Since(*sessionCreated) > time.Duration(maxAge)*time.Hour
}

func webAccountFromContext(ctx context.Context) *Account {
	if v, ok := ctx.Value(webContextKeyAccount).(*Account); ok {
		return v
	}
	return nil
}

func (h *WebHandler) isLastAdmin(ctx context.Context, id string) bool {
	accounts, err := h.store.ListAccounts(ctx)
	if err != nil {
		return true
	}
	for _, a := range accounts {
		if a.ID != id && a.Role == RoleAdmin {
			return false
		}
	}
	return true
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func clampedIntString(s string) string {
	n, _ := strconv.ParseInt(s, 10, 64)
	if n < 0 {
		n = 0
	}
	return strconv.FormatInt(n, 10)
}

func clampedMinIntString(s string, minVal int64) string {
	n, _ := strconv.ParseInt(s, 10, 64)
	if n < minVal {
		n = minVal
	}
	return strconv.FormatInt(n, 10)
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatTimestamp(t time.Time) string {
	if t.IsZero() || t.Unix() == 0 {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func optionalTimeAgo(t *time.Time) string {
	if t == nil || t.IsZero() || t.Unix() == 0 {
		return "-"
	}
	return humanize.Time(*t)
}
