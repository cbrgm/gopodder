package gopodder

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func seedAPIKey(store *mockStore, accountID, role, rawToken string) {
	prefix := rawToken[:apiKeyPrefixLen]
	hash, _ := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	store.apiKeys = append(store.apiKeys, APIKey{
		ID:        "key-" + prefix,
		AccountID: accountID,
		Name:      "test-key",
		Prefix:    prefix,
		Hash:      string(hash),
		Role:      role,
		CreatedAt: time.Now(),
	})
}

func bearerRequest(method, path, token, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestAPIv1_Unauthorized(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	tests := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/users"},
		{"POST", "/api/v1/users"},
		{"GET", "/api/v1/accounts"},
	}

	for _, tt := range tests {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tt.method, tt.path, nil)
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401, got %d", tt.method, tt.path, w.Code)
		}
	}
}

func TestAPIv1_StandardKeyForbiddenOnAdminEndpoints(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	tests := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/accounts"},
		{"POST", "/api/v1/accounts"},
		{"DELETE", "/api/v1/accounts/some-id"},
		{"GET", "/api/v1/accounts/some-id/users"},
	}

	for _, tt := range tests {
		w := httptest.NewRecorder()
		r := bearerRequest(tt.method, tt.path, token, "")
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403, got %d", tt.method, tt.path, w.Code)
		}
	}
}

func TestAPIv1_ListUsers(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}
	store.users["user2"] = &User{Username: "user2", AccountID: "admin-id"}
	store.users["other"] = &User{Username: "other", AccountID: "other-account"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var users []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestAPIv1_CreateUser(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	body := `{"username":"newuser","password":"secret123"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/users", token, body))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := store.GetUser(nil, "newuser"); err != nil {
		t.Error("user was not created in store")
	}
}

func TestAPIv1_CreateUser_Conflict(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["existing"] = &User{Username: "existing", AccountID: "admin-id"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	body := `{"username":"existing","password":"secret123"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/users", token, body))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_DeleteUser(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["victim"] = &User{Username: "victim", AccountID: "admin-id"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("DELETE", "/api/v1/users/victim", token, ""))

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := store.GetUser(nil, "victim"); err == nil {
		t.Error("user was not deleted from store")
	}
}

func TestAPIv1_DeleteUser_NotOwned(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["other-user"] = &User{Username: "other-user", AccountID: "other-account"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("DELETE", "/api/v1/users/other-user", token, ""))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_ListDevices(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}
	store.devices["user1"] = []Device{
		{ID: "phone", Caption: "My Phone", Type: "mobile"},
		{ID: "desktop", Caption: "My PC", Type: "desktop"},
	}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users/user1/devices", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var devices []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &devices); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(devices) != 2 {
		t.Errorf("expected 2 devices, got %d", len(devices))
	}
}

func TestAPIv1_GetSubscriptions(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}
	store.subscriptions["user1"] = []string{"https://example.com/feed1.xml", "https://example.com/feed2.xml"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users/user1/subscriptions", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var subs []string
	if err := json.Unmarshal(w.Body.Bytes(), &subs); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(subs) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(subs))
	}
}

func TestAPIv1_GetSubscriptionsOPML(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}
	store.subscriptions["user1"] = []string{"https://example.com/feed.xml"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users/user1/subscriptions.opml", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/x-opml+xml" {
		t.Errorf("expected Content-Type text/x-opml+xml, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "https://example.com/feed.xml") {
		t.Error("OPML output does not contain feed URL")
	}
}

func TestAPIv1_UpdateSubscriptions(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}
	store.subscriptions["user1"] = []string{"https://example.com/old.xml"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	body := `{"add":["https://example.com/new.xml"],"remove":["https://example.com/old.xml"]}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/users/user1/subscriptions", token, body))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	subs := store.subscriptions["user1"]
	if len(subs) != 1 || subs[0] != "https://example.com/new.xml" {
		t.Errorf("unexpected subscriptions after update: %v", subs)
	}
}

func TestAPIv1_AdminListAccounts(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/accounts", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var accounts []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &accounts); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(accounts) != 1 {
		t.Errorf("expected 1 account, got %d", len(accounts))
	}
}

func TestAPIv1_AdminCreateAccount(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	body := `{"username":"newaccount","password":"secret123","role":"standard"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/accounts", token, body))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	found := false
	for _, a := range store.accounts {
		if a.Username == "newaccount" {
			found = true
			if a.Role != RoleStandard {
				t.Errorf("expected role %q, got %q", RoleStandard, a.Role)
			}
		}
	}
	if !found {
		t.Error("account was not created in store")
	}
}

func TestAPIv1_AdminDeleteAccount(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.accounts["target-id"] = &Account{ID: "target-id", Username: "target", PWHash: hashPassword("pass"), Role: RoleStandard}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("DELETE", "/api/v1/accounts/target-id", token, ""))

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if _, ok := store.accounts["target-id"]; ok {
		t.Error("account was not deleted from store")
	}
}

func TestAPIv1_AdminDeleteAccount_CannotDeleteAdmin(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("DELETE", "/api/v1/accounts/admin-id", token, ""))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_AdminListAccountUsers(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.accounts["other-id"] = &Account{ID: "other-id", Username: "other", Role: RoleStandard}
	store.users["u1"] = &User{Username: "u1", AccountID: "other-id"}
	store.users["u2"] = &User{Username: "u2", AccountID: "other-id"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/accounts/other-id/users", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var users []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestAPIv1_InvalidToken(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users", "gp_wrongtokenwrongtoken12345678", ""))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAPIv1_CreateUser_PasswordTooShort(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.settings["min_password_length"] = "12"

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	body := `{"username":"shortpw","password":"short"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/users", token, body))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "at least 12 characters") {
		t.Errorf("expected password length error, got: %s", w.Body.String())
	}
}

func TestAPIv1_CreateUser_LimitReached(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.settings["max_users_per_account"] = "1"
	store.users["existing"] = &User{Username: "existing", AccountID: "admin-id"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	body := `{"username":"second","password":"longenoughpassword"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/users", token, body))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "user limit reached") {
		t.Errorf("expected limit error, got: %s", w.Body.String())
	}
}

func TestAPIv1_UpdateSubscriptions_Overlap(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	body := `{"add":["https://example.com/feed"],"remove":["https://example.com/feed"]}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/users/user1/subscriptions", token, body))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "same URL") {
		t.Errorf("expected overlap error, got: %s", w.Body.String())
	}
}

func TestAPIv1_CreateUser_CreationDisabled(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.settings["allow_user_creation"] = "false"
	store.accounts["std-id"] = &Account{ID: "std-id", Username: "stduser", PWHash: hashPassword("pass"), Role: RoleStandard}

	token := "gp_bbccddee11223344aabbccdd11223344"
	seedAPIKey(store, "std-id", RoleStandard, token)

	w := httptest.NewRecorder()
	body := `{"username":"newuser","password":"longenoughpassword"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/users", token, body))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "creation is disabled") {
		t.Errorf("expected creation disabled error, got: %s", w.Body.String())
	}
}

func TestAPIv1_CreateUser_AdminBypassesCreationDisabled(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.settings["allow_user_creation"] = "false"

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	body := `{"username":"newuser","password":"longenoughpassword"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/users", token, body))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 (admin bypasses setting), got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_AdminCreateAccount_Conflict(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	body := `{"username":"admin","password":"secret123"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/accounts", token, body))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_AdminListAccountUsers_NotFound(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/accounts/nonexistent/users", token, ""))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_AdminDeleteAccount_NotFound(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("DELETE", "/api/v1/accounts/nonexistent", token, ""))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_DeleteUser_NotFound(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("DELETE", "/api/v1/users/nonexistent", token, ""))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_GetSubscriptions_UserNotFound(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users/nonexistent/subscriptions", token, ""))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_AdminCreateAccount_PasswordTooShort(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.settings["min_password_length"] = "10"

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	body := `{"username":"newacct","password":"short"}`
	handler.ServeHTTP(w, bearerRequest("POST", "/api/v1/accounts", token, body))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "at least 10 characters") {
		t.Errorf("expected password length error, got: %s", w.Body.String())
	}
}

func TestAPIv1_ListUsers_Empty(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "[]\n" {
		t.Errorf("expected empty array, got %q", w.Body.String())
	}
}

func TestAPIv1_GetSubscriptions_Empty(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users/user1/subscriptions", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "[]\n" {
		t.Errorf("expected empty array (not null), got %q", w.Body.String())
	}
}

func TestAPIv1_AdminCannotDeleteOtherAdmin(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.accounts["other-admin"] = &Account{ID: "other-admin", Username: "otheradmin", PWHash: hashPassword("pass"), Role: RoleAdmin}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("DELETE", "/api/v1/accounts/other-admin", token, ""))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (admin accounts cannot be deleted via API), got %d: %s", w.Code, w.Body.String())
	}
	if _, ok := store.accounts["other-admin"]; !ok {
		t.Error("other admin account should NOT have been deleted")
	}
}

func TestAPIv1_AdminCannotDeleteSelf(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("DELETE", "/api/v1/accounts/admin-id", token, ""))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (cannot self-delete via API), got %d: %s", w.Code, w.Body.String())
	}
	if _, ok := store.accounts["admin-id"]; !ok {
		t.Error("own admin account should NOT have been deleted")
	}
}

func TestAPIv1_StandardKeyCannotAccessOtherUsersResources(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.accounts["other-id"] = &Account{ID: "other-id", Username: "other", Role: RoleStandard}
	store.users["otheruser"] = &User{Username: "otheruser", AccountID: "other-id"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/users/otheruser/devices"},
		{"GET", "/api/v1/users/otheruser/subscriptions"},
		{"GET", "/api/v1/users/otheruser/subscriptions.opml"},
		{"DELETE", "/api/v1/users/otheruser"},
	}

	for _, ep := range endpoints {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, bearerRequest(ep.method, ep.path, token, ""))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403, got %d", ep.method, ep.path, w.Code)
		}
	}
}

func TestAPIv1_AdminKeyCanStillUseStandardEndpoints(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	store.users["myuser"] = &User{Username: "myuser", AccountID: "admin-id"}
	store.subscriptions["myuser"] = []string{"https://example.com/feed.xml"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users/myuser/subscriptions", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("admin key should access standard endpoints for own users, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIv1_ListUsers_IncludesLastActivity(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	activity := time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)
	store.users["active"] = &User{Username: "active", AccountID: "admin-id", LastActivity: &activity}
	store.users["inactive"] = &User{Username: "inactive", AccountID: "admin-id"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var users []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	var activeUser, inactiveUser map[string]any
	for _, u := range users {
		switch u["username"] {
		case "active":
			activeUser = u
		case "inactive":
			inactiveUser = u
		}
	}

	if activeUser["last_activity"] != "2026-05-28T14:30:00" {
		t.Errorf("active user last_activity = %v, want 2026-05-28T14:30:00", activeUser["last_activity"])
	}
	if _, exists := inactiveUser["last_activity"]; exists {
		t.Errorf("inactive user should not have last_activity field, got %v", inactiveUser["last_activity"])
	}
}

func TestAPIv1_ListDevices_IncludesLastActivity(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	synced := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	store.users["user1"] = &User{Username: "user1", AccountID: "admin-id"}
	store.devices["user1"] = []Device{
		{ID: "phone", Caption: "Phone", Type: "mobile", LastActivity: &synced},
		{ID: "tablet", Caption: "Tablet", Type: "other"},
	}
	store.subscriptions["user1"] = []string{"https://example.com/feed.xml"}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleStandard, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/users/user1/devices", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var devices []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &devices); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	var phone, tablet map[string]any
	for _, d := range devices {
		switch d["id"] {
		case "phone":
			phone = d
		case "tablet":
			tablet = d
		}
	}

	if phone["last_activity"] != "2026-05-27T10:00:00" {
		t.Errorf("phone last_activity = %v, want 2026-05-27T10:00:00", phone["last_activity"])
	}
	if phone["subscriptions"] != float64(1) {
		t.Errorf("phone subscriptions = %v, want 1", phone["subscriptions"])
	}
	if _, exists := tablet["last_activity"]; exists {
		t.Errorf("tablet should not have last_activity field, got %v", tablet["last_activity"])
	}
}

func TestAPIv1_ListAccounts_IncludesTimestamps(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	created := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	lastLogin := time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC)
	lastActivity := time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC)
	store.accounts["admin-id"].CreatedAt = created
	store.accounts["admin-id"].LastLogin = &lastLogin
	store.accounts["admin-id"].LastActivity = &lastActivity

	store.accounts["new-id"] = &Account{
		ID: "new-id", Username: "newuser", Role: RoleStandard,
		CreatedAt: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}

	token := "gp_aabbccdd11223344aabbccdd11223344"
	seedAPIKey(store, "admin-id", RoleAdmin, token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerRequest("GET", "/api/v1/accounts", token, ""))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var accounts []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &accounts); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	var admin, newAcct map[string]any
	for _, a := range accounts {
		switch a["username"] {
		case "admin":
			admin = a
		case "newuser":
			newAcct = a
		}
	}

	if admin == nil {
		t.Fatal("admin account not found in response")
	}
	if admin["created_at"] != "2026-01-15T10:00:00" {
		t.Errorf("admin created_at = %v, want 2026-01-15T10:00:00", admin["created_at"])
	}
	if admin["last_login"] != "2026-05-28T09:00:00" {
		t.Errorf("admin last_login = %v, want 2026-05-28T09:00:00", admin["last_login"])
	}
	if admin["last_activity"] != "2026-05-28T14:30:00" {
		t.Errorf("admin last_activity = %v, want 2026-05-28T14:30:00", admin["last_activity"])
	}

	if newAcct == nil {
		t.Fatal("newuser account not found in response")
	}
	if newAcct["created_at"] != "2026-03-01T12:00:00" {
		t.Errorf("newuser created_at = %v, want 2026-03-01T12:00:00", newAcct["created_at"])
	}
	if _, exists := newAcct["last_login"]; exists {
		t.Errorf("newuser should not have last_login, got %v", newAcct["last_login"])
	}
	if _, exists := newAcct["last_activity"]; exists {
		t.Errorf("newuser should not have last_activity, got %v", newAcct["last_activity"])
	}
}

func TestGenerateAPIKey(t *testing.T) {
	raw, prefix, hash, err := generateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(raw, "gp_") {
		t.Errorf("key should start with gp_, got %q", raw[:5])
	}
	if len(raw) != 3+32 {
		t.Errorf("expected key length 35, got %d", len(raw))
	}
	if prefix != raw[:apiKeyPrefixLen] {
		t.Errorf("prefix mismatch: %q vs %q", prefix, raw[:apiKeyPrefixLen])
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(raw)); err != nil {
		t.Error("hash does not validate against raw key")
	}
}
