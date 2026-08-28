package gopodder

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"uuid"

	"golang.org/x/crypto/bcrypt"
)

const (
	apiV1Prefix = "/api/v1"

	apiKeyPrefix    = "gp_"
	apiKeyRawLen    = 16 // 16 bytes = 32 hex chars
	apiKeyPrefixLen = 11 // "gp_" + first 8 hex chars used for DB lookup
)

const contextKeyAPIAccount contextKey = "api_account"

func apiAccountFromContext(ctx context.Context) *Account {
	if v, ok := ctx.Value(contextKeyAPIAccount).(*Account); ok {
		return v
	}
	return nil
}

func generateAPIKey() (raw, prefix, hash string, err error) {
	b := make([]byte, apiKeyRawLen)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", err
	}
	raw = apiKeyPrefix + hex.EncodeToString(b)
	prefix = raw[:apiKeyPrefixLen]
	h, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", "", "", err
	}
	return raw, prefix, string(h), nil
}

func (a *API) authenticateAPIKey(r *http.Request) (*Account, *APIKey, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(token) < apiKeyPrefixLen {
		return nil, nil, false
	}

	keys, err := a.store.GetAPIKeysByPrefix(r.Context(), token[:apiKeyPrefixLen])
	if err != nil || len(keys) == 0 {
		return nil, nil, false
	}

	for i := range keys {
		if bcrypt.CompareHashAndPassword([]byte(keys[i].Hash), []byte(token)) == nil {
			acct, err := a.store.GetAccountByID(r.Context(), keys[i].AccountID)
			if err != nil {
				return nil, nil, false
			}
			go func(id string) {
				_ = a.store.UpdateAPIKeyLastUsed(context.Background(), id, time.Now())
			}(keys[i].ID)
			return acct, &keys[i], true
		}
	}
	return nil, nil, false
}

func (a *API) withAPIKey(requiredRole string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acct, key, ok := a.authenticateAPIKey(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if requiredRole == RoleAdmin && key.Role != RoleAdmin {
			writeJSONError(w, http.StatusForbidden, "forbidden")
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyAPIAccount, acct)
		next(w, r.WithContext(ctx))
	}
}

// requireOwnedUser validates that the path user belongs to the authenticated account.
// On failure it writes an error response and returns ("", false).
func (a *API) requireOwnedUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	acct := apiAccountFromContext(r.Context())
	username := r.PathValue("username")

	user, err := a.store.GetUser(r.Context(), username)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "user not found")
		return "", false
	}
	if user.AccountID != acct.ID {
		writeJSONError(w, http.StatusForbidden, "user does not belong to your account")
		return "", false
	}
	return username, true
}

func (a *API) registerAPIv1Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users", a.withAPIKey(RoleStandard, a.handleAPIListUsers))
	mux.HandleFunc("POST /api/v1/users", a.withAPIKey(RoleStandard, a.handleAPICreateUser))
	mux.HandleFunc("DELETE /api/v1/users/{username}", a.withAPIKey(RoleStandard, a.handleAPIDeleteUser))
	mux.HandleFunc("GET /api/v1/users/{username}/devices", a.withAPIKey(RoleStandard, a.handleAPIListDevices))
	mux.HandleFunc("GET /api/v1/users/{username}/subscriptions", a.withAPIKey(RoleStandard, a.handleAPIGetSubscriptions))
	mux.HandleFunc("GET /api/v1/users/{username}/subscriptions.opml", a.withAPIKey(RoleStandard, a.handleAPIGetSubscriptionsOPML))
	mux.HandleFunc("POST /api/v1/users/{username}/subscriptions", a.withAPIKey(RoleStandard, a.handleAPIUpdateSubscriptions))

	mux.HandleFunc("GET /api/v1/accounts", a.withAPIKey(RoleAdmin, a.handleAPIListAccounts))
	mux.HandleFunc("POST /api/v1/accounts", a.withAPIKey(RoleAdmin, a.handleAPICreateAccount))
	mux.HandleFunc("DELETE /api/v1/accounts/{id}", a.withAPIKey(RoleAdmin, a.handleAPIDeleteAccount))
	mux.HandleFunc("GET /api/v1/accounts/{id}/users", a.withAPIKey(RoleAdmin, a.handleAPIListAccountUsers))
}

// Standard key handlers

type apiUserResponse struct {
	Username     string  `json:"username"`
	LastActivity *string `json:"last_activity,omitempty"`
}

func usersToResponse(users []User) []apiUserResponse {
	result := make([]apiUserResponse, 0, len(users))
	for _, u := range users {
		resp := apiUserResponse{Username: u.Username}
		if u.LastActivity != nil {
			resp.LastActivity = new(u.LastActivity.UTC().Format(isoFormat))
		}
		result = append(result, resp)
	}
	return result
}

func (a *API) handleAPIListUsers(w http.ResponseWriter, r *http.Request) {
	acct := apiAccountFromContext(r.Context())
	users, err := a.store.ListUsersByAccount(r.Context(), acct.ID)
	if err != nil {
		a.logger.Error("failed to list users", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, usersToResponse(users))
}

func (a *API) handleAPICreateUser(w http.ResponseWriter, r *http.Request) {
	acct := apiAccountFromContext(r.Context())

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if !isValidUsername(req.Username) {
		writeJSONError(w, http.StatusBadRequest, "invalid username")
		return
	}
	if msg := a.checkPasswordLength(r.Context(), req.Password); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}
	if !a.userCreationAllowed(r.Context(), acct) {
		writeJSONError(w, http.StatusForbidden, "user creation is disabled")
		return
	}
	if userLimitReached(r.Context(), a.store, acct.ID) {
		writeJSONError(w, http.StatusForbidden, "user limit reached for this account")
		return
	}
	if _, err := a.store.GetUser(r.Context(), req.Username); err == nil {
		writeJSONError(w, http.StatusConflict, "username already exists")
		return
	}
	pwhash, err := hashPassword(req.Password)
	if err != nil {
		a.logger.Error("failed to hash password", "err", err, "username", req.Username)
		writeJSONError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if err := a.store.CreateUser(r.Context(), req.Username, pwhash, acct.ID); err != nil {
		a.logger.Error("failed to create user", "err", err, "username", req.Username)
		writeJSONError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"username": req.Username})
}

func (a *API) userCreationAllowed(ctx context.Context, acct *Account) bool {
	if acct.Role == RoleAdmin {
		return true
	}
	val, err := a.store.GetSetting(ctx, SettingAllowUserCreation)
	if err != nil {
		return true
	}
	return val == "true"
}

func (a *API) checkPasswordLength(ctx context.Context, password string) string {
	minLen := minPasswordLength(ctx, a.store)
	if int64(len(password)) < minLen {
		return fmt.Sprintf("password must be at least %d characters", minLen)
	}
	if len(password) > maxPasswordBytes {
		return fmt.Sprintf("password must be at most %d bytes long", maxPasswordBytes)
	}
	return ""
}

func (a *API) handleAPIDeleteUser(w http.ResponseWriter, r *http.Request) {
	username, ok := a.requireOwnedUser(w, r)
	if !ok {
		return
	}
	deleteUserCascade(r.Context(), a.store, username)
	w.WriteHeader(http.StatusNoContent)
}

type apiDeviceResponse struct {
	ID            string  `json:"id"`
	Caption       string  `json:"caption"`
	Type          string  `json:"type"`
	Subscriptions int64   `json:"subscriptions"`
	LastActivity  *string `json:"last_activity,omitempty"`
}

func (a *API) handleAPIListDevices(w http.ResponseWriter, r *http.Request) {
	username, ok := a.requireOwnedUser(w, r)
	if !ok {
		return
	}
	devices, err := a.store.ListDevices(r.Context(), username)
	if err != nil {
		a.logger.Error("failed to list devices", "err", err, "username", username)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	subs, _ := a.store.GetSubscriptions(r.Context(), username)
	subCount := int64(len(subs))

	result := make([]apiDeviceResponse, 0, len(devices))
	for _, d := range devices {
		resp := apiDeviceResponse{
			ID:            d.ID,
			Caption:       d.Caption,
			Type:          d.Type,
			Subscriptions: subCount,
		}
		if d.LastActivity != nil {
			resp.LastActivity = new(d.LastActivity.UTC().Format(isoFormat))
		}
		result = append(result, resp)
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) handleAPIGetSubscriptions(w http.ResponseWriter, r *http.Request) {
	username, ok := a.requireOwnedUser(w, r)
	if !ok {
		return
	}
	subs, err := a.store.GetSubscriptions(r.Context(), username)
	if err != nil {
		a.logger.Error("failed to get subscriptions", "err", err, "username", username)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if subs == nil {
		subs = []string{}
	}
	writeJSON(w, http.StatusOK, subs)
}

func (a *API) handleAPIGetSubscriptionsOPML(w http.ResponseWriter, r *http.Request) {
	username, ok := a.requireOwnedUser(w, r)
	if !ok {
		return
	}
	subs, err := a.store.GetSubscriptions(r.Context(), username)
	if err != nil {
		a.logger.Error("failed to get subscriptions", "err", err, "username", username)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeOPML(w, fmt.Sprintf("goPodder subscriptions for %s", username), subs)
}

func (a *API) handleAPIUpdateSubscriptions(w http.ResponseWriter, r *http.Request) {
	username, ok := a.requireOwnedUser(w, r)
	if !ok {
		return
	}

	var req struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Add = filterValidURLs(req.Add)
	req.Remove = filterValidURLs(req.Remove)

	if hasOverlap(req.Add, req.Remove) {
		writeJSONError(w, http.StatusBadRequest, "same URL in add and remove")
		return
	}

	now := time.Now().Unix()
	for _, url := range req.Add {
		_ = a.store.ReactivateSubscription(r.Context(), username, url, now)
	}
	if err := a.store.UpdateSubscriptions(r.Context(), username, req.Add, req.Remove, now); err != nil {
		a.logger.Error("failed to update subscriptions", "err", err, "username", username)
		writeJSONError(w, http.StatusInternalServerError, "failed to update subscriptions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"timestamp": now})
}

// Admin key handlers

func (a *API) handleAPIListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := a.store.ListAccounts(r.Context())
	if err != nil {
		a.logger.Error("failed to list accounts", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type acctResp struct {
		ID           string  `json:"id"`
		Username     string  `json:"username"`
		Role         string  `json:"role"`
		CreatedAt    string  `json:"created_at"`
		LastLogin    *string `json:"last_login,omitempty"`
		LastActivity *string `json:"last_activity,omitempty"`
	}
	result := make([]acctResp, 0, len(accounts))
	for _, acct := range accounts {
		resp := acctResp{
			ID:        acct.ID,
			Username:  acct.Username,
			Role:      acct.Role,
			CreatedAt: acct.CreatedAt.UTC().Format(isoFormat),
		}
		if acct.LastLogin != nil {
			resp.LastLogin = new(acct.LastLogin.UTC().Format(isoFormat))
		}
		if acct.LastActivity != nil {
			resp.LastActivity = new(acct.LastActivity.UTC().Format(isoFormat))
		}
		result = append(result, resp)
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) handleAPICreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if !isValidUsername(req.Username) {
		writeJSONError(w, http.StatusBadRequest, "invalid username")
		return
	}
	if msg := a.checkPasswordLength(r.Context(), req.Password); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}
	if req.Role != RoleAdmin {
		req.Role = RoleStandard
	}

	if _, err := a.store.GetAccount(r.Context(), req.Username); err == nil {
		writeJSONError(w, http.StatusConflict, "account already exists")
		return
	}
	pwhash, err := hashPassword(req.Password)
	if err != nil {
		a.logger.Error("failed to hash password", "err", err, "username", req.Username)
		writeJSONError(w, http.StatusInternalServerError, "failed to create account")
		return
	}
	id := uuid.New().String()
	if err := a.store.CreateAccount(r.Context(), id, req.Username, pwhash, req.Role, time.Now()); err != nil {
		a.logger.Error("failed to create account", "err", err, "username", req.Username)
		writeJSONError(w, http.StatusInternalServerError, "failed to create account")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "username": req.Username, "role": req.Role})
}

func (a *API) handleAPIDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	acct, err := a.store.GetAccountByID(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "account not found")
		return
	}
	if acct.Role == RoleAdmin {
		writeJSONError(w, http.StatusBadRequest, "cannot delete admin accounts via API")
		return
	}
	deleteAccountCascade(r.Context(), a.store, id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleAPIListAccountUsers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.store.GetAccountByID(r.Context(), id); err != nil {
		writeJSONError(w, http.StatusNotFound, "account not found")
		return
	}
	users, err := a.store.ListUsersByAccount(r.Context(), id)
	if err != nil {
		a.logger.Error("failed to list account users", "err", err, "account_id", id)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, usersToResponse(users))
}
