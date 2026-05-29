package gopodder

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if requiredRole == RoleAdmin && key.Role != RoleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
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
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return "", false
	}
	if user.AccountID != acct.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "user does not belong to your account"})
		return "", false
	}
	return username, true
}

func (a *API) registerAPIv1Routes(mux *http.ServeMux) {
	mux.HandleFunc(fmt.Sprintf("GET %s/users", apiV1Prefix), a.withAPIKey(RoleStandard, a.handleAPIListUsers))
	mux.HandleFunc(fmt.Sprintf("POST %s/users", apiV1Prefix), a.withAPIKey(RoleStandard, a.handleAPICreateUser))
	mux.HandleFunc(fmt.Sprintf("DELETE %s/users/{username}", apiV1Prefix), a.withAPIKey(RoleStandard, a.handleAPIDeleteUser))
	mux.HandleFunc(fmt.Sprintf("GET %s/users/{username}/devices", apiV1Prefix), a.withAPIKey(RoleStandard, a.handleAPIListDevices))
	mux.HandleFunc(fmt.Sprintf("GET %s/users/{username}/subscriptions", apiV1Prefix), a.withAPIKey(RoleStandard, a.handleAPIGetSubscriptions))
	mux.HandleFunc(fmt.Sprintf("GET %s/users/{username}/subscriptions.opml", apiV1Prefix), a.withAPIKey(RoleStandard, a.handleAPIGetSubscriptionsOPML))
	mux.HandleFunc(fmt.Sprintf("POST %s/users/{username}/subscriptions", apiV1Prefix), a.withAPIKey(RoleStandard, a.handleAPIUpdateSubscriptions))

	mux.HandleFunc(fmt.Sprintf("GET %s/accounts", apiV1Prefix), a.withAPIKey(RoleAdmin, a.handleAPIListAccounts))
	mux.HandleFunc(fmt.Sprintf("POST %s/accounts", apiV1Prefix), a.withAPIKey(RoleAdmin, a.handleAPICreateAccount))
	mux.HandleFunc(fmt.Sprintf("DELETE %s/accounts/{id}", apiV1Prefix), a.withAPIKey(RoleAdmin, a.handleAPIDeleteAccount))
	mux.HandleFunc(fmt.Sprintf("GET %s/accounts/{id}/users", apiV1Prefix), a.withAPIKey(RoleAdmin, a.handleAPIListAccountUsers))
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if !isValidUsername(req.Username) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid username"})
		return
	}
	if msg := a.checkMinPasswordLength(r.Context(), req.Password); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if !a.userCreationAllowed(r.Context(), acct) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "user creation is disabled"})
		return
	}
	if a.userLimitReached(r.Context(), acct.ID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "user limit reached for this account"})
		return
	}
	if _, err := a.store.GetUser(r.Context(), req.Username); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	if err := a.store.CreateUser(r.Context(), req.Username, hashPassword(req.Password), acct.ID); err != nil {
		a.logger.Error("failed to create user", "err", err, "username", req.Username)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
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

func (a *API) checkMinPasswordLength(ctx context.Context, password string) string {
	val, _ := a.store.GetSetting(ctx, SettingMinPasswordLength)
	minLen, _ := strconv.ParseInt(val, 10, 64)
	minLen = cmp.Or(minLen, 8)
	if int64(len(password)) < minLen {
		return fmt.Sprintf("password must be at least %d characters", minLen)
	}
	return ""
}

func (a *API) userLimitReached(ctx context.Context, accountID string) bool {
	val, _ := a.store.GetSetting(ctx, SettingMaxUsersPerAccount)
	limit, _ := strconv.ParseInt(val, 10, 64)
	if limit <= 0 {
		return false
	}
	users, err := a.store.ListUsersByAccount(ctx, accountID)
	if err != nil {
		return false
	}
	return int64(len(users)) >= limit
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	outlines := make([]opmlOutline, 0, len(subs))
	for _, u := range subs {
		outlines = append(outlines, opmlOutline{Type: "rss", Text: u, Title: u, XMLURL: u})
	}
	doc := opmlDoc{
		Version: "2.0",
		Head:    opmlHead{Title: fmt.Sprintf("goPodder subscriptions for %s", username)},
		Body:    opmlBody{Outlines: outlines},
	}

	w.Header().Set("Content-Type", "text/x-opml+xml")
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(doc)
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	req.Add = filterValidURLs(req.Add)
	req.Remove = filterValidURLs(req.Remove)

	if hasOverlap(req.Add, req.Remove) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "same URL in add and remove"})
		return
	}

	now := time.Now().Unix()
	for _, url := range req.Add {
		_ = a.store.ReactivateSubscription(r.Context(), username, url, now)
	}
	if err := a.store.UpdateSubscriptions(r.Context(), username, req.Add, req.Remove, now); err != nil {
		a.logger.Error("failed to update subscriptions", "err", err, "username", username)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update subscriptions"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"timestamp": now})
}

// Admin key handlers

func (a *API) handleAPIListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := a.store.ListAccounts(r.Context())
	if err != nil {
		a.logger.Error("failed to list accounts", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if !isValidUsername(req.Username) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid username"})
		return
	}
	if msg := a.checkMinPasswordLength(r.Context(), req.Password); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if req.Role != RoleAdmin {
		req.Role = RoleStandard
	}

	if _, err := a.store.GetAccount(r.Context(), req.Username); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "account already exists"})
		return
	}
	id := uuid.New().String()
	if err := a.store.CreateAccount(r.Context(), id, req.Username, hashPassword(req.Password), req.Role, time.Now()); err != nil {
		a.logger.Error("failed to create account", "err", err, "username", req.Username)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create account"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "username": req.Username, "role": req.Role})
}

func (a *API) handleAPIDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	acct, err := a.store.GetAccountByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	if acct.Role == RoleAdmin {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete admin accounts via API"})
		return
	}
	deleteAccountCascade(r.Context(), a.store, id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleAPIListAccountUsers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.store.GetAccountByID(r.Context(), id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	users, err := a.store.ListUsersByAccount(r.Context(), id)
	if err != nil {
		a.logger.Error("failed to list account users", "err", err, "account_id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, usersToResponse(users))
}
