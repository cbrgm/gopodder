package gopodder

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleGetEpisodes(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	device := "phone1"
	store.episodes["testuser"] = []Episode{
		{Podcast: "http://pod.com/feed", Episode: "http://pod.com/ep1.mp3", Device: &device, Action: "play"},
	}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("returns episodes with correct structure", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/api/2/episodes/testuser.json?since=0", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var resp episodeResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Timestamp == 0 {
			t.Error("expected non-zero timestamp")
		}
		if resp.Actions == nil {
			t.Fatal("actions should not be nil")
		}
		if len(resp.Actions) != 1 {
			t.Fatalf("expected 1 action, got %d", len(resp.Actions))
		}
		if resp.Actions[0].Podcast != "http://pod.com/feed" {
			t.Errorf("podcast = %q, want %q", resp.Actions[0].Podcast, "http://pod.com/feed")
		}
	})

	t.Run("empty episodes returns empty array not null", func(t *testing.T) {
		store2 := newMockStore()
		store2.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
		api2 := newTestAPI(store2)
		handler2 := api2.Handler()

		r := authedRequest(http.MethodGet, "/api/2/episodes/testuser.json", "")
		w := httptest.NewRecorder()
		handler2.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var resp episodeResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Actions == nil {
			t.Error("actions should be empty array, not nil")
		}
		if len(resp.Actions) != 0 {
			t.Errorf("expected 0 actions, got %d", len(resp.Actions))
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/api/2/episodes/otheruser.json?since=0", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("supports podcast query parameter", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/api/2/episodes/testuser.json?podcast=http://pod.com/feed", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("supports device query parameter", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/api/2/episodes/testuser.json?device=phone1", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("supports since query parameter", func(t *testing.T) {
		r := authedRequest(http.MethodGet, "/api/2/episodes/testuser.json?since=9999999", "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestHandleUploadEpisodes(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	api := newTestAPI(store)
	handler := api.Handler()

	t.Run("valid upload", func(t *testing.T) {
		body := `[{"podcast":"http://pod.com/feed","episode":"http://pod.com/ep1.mp3","action":"play","started":0,"position":60,"total":600}]`
		r := authedRequest(http.MethodPost, "/api/2/episodes/testuser.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var resp episodeUploadResponse
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

	t.Run("upload with all episode action types", func(t *testing.T) {
		actions := []string{"download", "play", "delete", "new"}
		for _, action := range actions {
			body := `[{"podcast":"http://pod.com/feed","episode":"http://pod.com/` + action + `.mp3","action":"` + action + `"}]`
			r := authedRequest(http.MethodPost, "/api/2/episodes/testuser.json", body)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Errorf("action %q: status = %d, want %d", action, w.Code, http.StatusOK)
			}
		}
	})

	t.Run("upload with timestamp", func(t *testing.T) {
		body := `[{"podcast":"http://pod.com/feed","episode":"http://pod.com/ts.mp3","action":"play","timestamp":"2024-06-01T14:30:00","position":120,"total":600,"started":0}]`
		r := authedRequest(http.MethodPost, "/api/2/episodes/testuser.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("upload with device field", func(t *testing.T) {
		body := `[{"podcast":"http://pod.com/feed","episode":"http://pod.com/dev.mp3","device":"gpodder_abcdef123","action":"download"}]`
		r := authedRequest(http.MethodPost, "/api/2/episodes/testuser.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("invalid json returns 400", func(t *testing.T) {
		r := authedRequest(http.MethodPost, "/api/2/episodes/testuser.json", "not json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("forbidden for other user", func(t *testing.T) {
		body := `[{"podcast":"http://pod.com/feed","episode":"http://pod.com/ep1.mp3","action":"play"}]`
		r := authedRequest(http.MethodPost, "/api/2/episodes/otheruser.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})

	t.Run("multiple episodes in single upload", func(t *testing.T) {
		body := `[
			{"podcast":"http://pod.com/feed","episode":"http://pod.com/multi1.mp3","action":"download"},
			{"podcast":"http://pod.com/feed","episode":"http://pod.com/multi2.mp3","action":"play","started":0,"position":30,"total":300}
		]`
		r := authedRequest(http.MethodPost, "/api/2/episodes/testuser.json", body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}
