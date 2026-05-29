package gopodder

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestHasOverlap(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{"no overlap", []string{"http://a.com"}, []string{"http://b.com"}, false},
		{"overlap", []string{"http://a.com"}, []string{"http://a.com"}, true},
		{"partial overlap", []string{"http://a.com", "http://b.com"}, []string{"http://b.com", "http://c.com"}, true},
		{"empty slices", nil, nil, false},
		{"empty a", nil, []string{"http://a.com"}, false},
		{"empty b", []string{"http://a.com"}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("hasOverlap(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestHandleGetSubscriptions(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	store.subscriptions["testuser"] = []string{"http://a.com/feed", "http://b.com/feed"}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("returns subscriptions for device", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/subscriptions/testuser/phone1.json", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var subs []string
		if err := json.Unmarshal(w.Body.Bytes(), &subs); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(subs) != 2 {
			t.Errorf("expected 2 subscriptions, got %d", len(subs))
		}
	})

	t.Run("empty user returns empty array not null", func(t *testing.T) {
		store2 := newMockStore()
		store2.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
		api2 := newTestAPI(store2)
		handler2 := api2.Handler()

		r := authedRequest(http.MethodGet, "/subscriptions/testuser/anydevice.json", "")
		w := httptest.NewRecorder()
		handler2.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if w.Body.String() != "[]\n" {
			t.Errorf("expected empty array, got %q", w.Body.String())
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/subscriptions/otheruser/phone1.json", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})
}

func TestHandleGetAllSubscriptions(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	store.subscriptions["testuser"] = []string{"http://a.com/feed", "http://b.com/feed", "http://c.com/feed"}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("returns deduplicated subscriptions across devices", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/subscriptions/testuser.json", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var subs []string
		if err := json.Unmarshal(w.Body.Bytes(), &subs); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(subs) < 1 {
			t.Error("expected at least 1 subscription in response")
		}
	})

	t.Run("empty returns empty array", func(t *testing.T) {
		store2 := newMockStore()
		store2.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
		api2 := newTestAPI(store2)
		handler2 := api2.Handler()

		r := authedRequest(http.MethodGet, "/subscriptions/testuser.json", "")
		w := httptest.NewRecorder()
		handler2.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if w.Body.String() != "[]\n" {
			t.Errorf("expected empty array, got %q", w.Body.String())
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/subscriptions/otheruser.json", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})
}

func TestHandleUploadSubscriptions(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	store.subscriptions["testuser"] = []string{"http://old.com/feed"}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("replaces subscription list", func(t *testing.T) {
		body := `["http://new.com/feed","http://old.com/feed"]`
		r := authedRequest(http.MethodPut, "/subscriptions/testuser/phone1.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("auto-creates device", func(t *testing.T) {
		body := `["http://new.com/feed"]`
		r := authedRequest(http.MethodPut, "/subscriptions/testuser/newdevice.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if !slices.ContainsFunc(store.devices["testuser"], func(d Device) bool {
			return d.ID == "newdevice"
		}) {
			t.Error("expected device to be auto-created on subscription upload")
		}
	})

	t.Run("invalid json returns 400", func(t *testing.T) {
		r := authedRequest(http.MethodPut, "/subscriptions/testuser/phone1.json", "not json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		r := authedRequest(http.MethodPut, "/subscriptions/otheruser/phone1.json", `["http://a.com"]`)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})
}

func TestHandleUploadSubscriptionChanges(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("valid upload", func(t *testing.T) {
		body := `{"add":["http://new.com/feed"],"remove":[]}`
		r := authedRequest(http.MethodPost, "/api/2/subscriptions/testuser/phone1.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var resp subscriptionChangeResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Timestamp == 0 {
			t.Error("expected non-zero timestamp")
		}
		if resp.UpdateURLs == nil {
			t.Error("update_urls should not be nil")
		}
	})

	t.Run("rejects overlap between add and remove", func(t *testing.T) {
		body := `{"add":["http://example.com/feed"],"remove":["http://example.com/feed"]}`
		r := authedRequest(http.MethodPost, "/api/2/subscriptions/testuser/phone1.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d (same URL in add and remove)", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("invalid json returns 400", func(t *testing.T) {
		r := authedRequest(http.MethodPost, "/api/2/subscriptions/testuser/phone1.json", "{bad")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		body := `{"add":["http://new.com/feed"],"remove":[]}`
		r := authedRequest(http.MethodPost, "/api/2/subscriptions/otheruser/phone1.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})
}

func TestHandleUploadSubscriptionChanges_UnmatchedRemoval(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	store.subscriptions["testuser"] = []string{"http://a.com", "http://b.com"}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("removing non-existent URL succeeds without error", func(t *testing.T) {
		body := `{"add":[],"remove":["http://nonexistent.com"]}`
		r := authedRequest(http.MethodPost, "/api/2/subscriptions/testuser/phone.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if len(store.subscriptions["testuser"]) != 2 {
			t.Errorf("subscriptions should be unchanged, got %v", store.subscriptions["testuser"])
		}
	})

	t.Run("removing existing URL works normally", func(t *testing.T) {
		body := `{"add":[],"remove":["http://a.com"]}`
		r := authedRequest(http.MethodPost, "/api/2/subscriptions/testuser/phone.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if len(store.subscriptions["testuser"]) != 1 {
			t.Errorf("expected 1 sub remaining, got %v", store.subscriptions["testuser"])
		}
	})

	t.Run("mixed valid and invalid removals processes valid ones", func(t *testing.T) {
		store.subscriptions["testuser"] = []string{"http://x.com", "http://y.com"}
		body := `{"add":[],"remove":["http://x.com","http://doesnotexist.com"]}`
		r := authedRequest(http.MethodPost, "/api/2/subscriptions/testuser/phone.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if len(store.subscriptions["testuser"]) != 1 || store.subscriptions["testuser"][0] != "http://y.com" {
			t.Errorf("expected [http://y.com], got %v", store.subscriptions["testuser"])
		}
	})
}

func TestHandleUploadSubscriptionChanges_TimestampAdvances(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	before := time.Now().Unix()
	body := `{"add":["http://new.com/feed"],"remove":[]}`
	r := authedRequest(http.MethodPost, "/api/2/subscriptions/testuser/phone1.json", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp subscriptionChangeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Timestamp <= before {
		t.Errorf("response timestamp (%d) should be strictly greater than request time (%d)", resp.Timestamp, before)
	}
}

func TestHandleGetSubscriptionChanges_TimestampAdvances(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	before := time.Now().Unix()
	r := authedRequest(http.MethodGet, "/api/2/subscriptions/testuser/phone1.json?since=0", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp subscriptionChangesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Timestamp <= before {
		t.Errorf("response timestamp (%d) should be strictly greater than request time (%d)", resp.Timestamp, before)
	}
}

func TestHandleGetSubscriptionChanges(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("returns changes with correct structure", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/api/2/subscriptions/testuser/phone1.json?since=0", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var resp subscriptionChangesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Timestamp == 0 {
			t.Error("expected non-zero timestamp")
		}
		if resp.Add == nil {
			t.Error("add should not be nil")
		}
		if resp.Remove == nil {
			t.Error("remove should not be nil")
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/api/2/subscriptions/otheruser/phone1.json?since=0", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("defaults since to 0 when missing", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/api/2/subscriptions/testuser/phone1.json", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}
