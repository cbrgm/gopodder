package gopodder

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleListDevices(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	store.devices["testuser"] = []Device{
		{ID: "phone1", Caption: "My Phone", Type: "mobile"},
	}
	api := newTestAPI(store)
	handler := api.Handler()

	r := authedRequest(http.MethodGet, "/api/2/devices/testuser.json", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var devices []Device
	if err := json.Unmarshal(w.Body.Bytes(), &devices); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].ID != "phone1" {
		t.Errorf("id = %q, want %q", devices[0].ID, "phone1")
	}
	if devices[0].Type != "mobile" {
		t.Errorf("type = %q, want %q", devices[0].Type, "mobile")
	}
}

func TestHandleListDevices_Empty(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	r := authedRequest(http.MethodGet, "/api/2/devices/testuser.json", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var devices []Device
	if err := json.Unmarshal(w.Body.Bytes(), &devices); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if devices == nil {
		t.Error("expected non-nil (empty array), got nil")
	}
}

func TestHandleListDevices_Forbidden(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	r := authedRequest(http.MethodGet, "/api/2/devices/otheruser.json", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleUpdateDevice(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	tests := []struct {
		name       string
		path       string
		body       string
		wantStatus int
	}{
		{
			name:       "valid update with caption and type",
			path:       "/api/2/devices/testuser/laptop1.json",
			body:       `{"caption":"Work Laptop","type":"laptop"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "partial update caption only",
			path:       "/api/2/devices/testuser/laptop1.json",
			body:       `{"caption":"New Caption"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "partial update type only",
			path:       "/api/2/devices/testuser/laptop1.json",
			body:       `{"type":"desktop"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty body creates device with defaults",
			path:       "/api/2/devices/testuser/newdev.json",
			body:       `{}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid json",
			path:       "/api/2/devices/testuser/laptop1.json",
			body:       `{invalid`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "other user forbidden",
			path:       "/api/2/devices/otheruser/laptop1.json",
			body:       `{"caption":"Hack"}`,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := authedRequest(http.MethodPost, tt.path, tt.body)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleUpdateDevice_PersistsData(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	r := authedRequest(http.MethodPost, "/api/2/devices/testuser/myphone.json", `{"caption":"My Phone","type":"mobile"}`)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	devices := store.devices["testuser"]
	if len(devices) != 1 {
		t.Fatalf("expected 1 device stored, got %d", len(devices))
	}
	if devices[0].ID != "myphone" {
		t.Errorf("device id = %q, want %q", devices[0].ID, "myphone")
	}
	if devices[0].Caption != "My Phone" {
		t.Errorf("caption = %q, want %q", devices[0].Caption, "My Phone")
	}
	if devices[0].Type != "mobile" {
		t.Errorf("type = %q, want %q", devices[0].Type, "mobile")
	}
}
