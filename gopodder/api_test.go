package gopodder

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestStripJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"login.json", "login"},
		{"testuser.json", "testuser"},
		{"phone1.json", "phone1"},
		{"noextension", "noextension"},
		{"", ""},
		{"multiple.dots.json", "multiple.dots"},
		{".json", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripExtension(tt.input)
			if got != tt.want {
				t.Errorf("stripExtension(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single segment", "testuser", []string{"testuser"}},
		{"two segments", "testuser/phone1.json", []string{"testuser", "phone1.json"}},
		{"leading slash", "/testuser/login.json", []string{"testuser", "login.json"}},
		{"trailing slash", "testuser/phone1/", []string{"testuser", "phone1"}},
		{"both slashes", "/testuser/phone1/", []string{"testuser", "phone1"}},
		{"three segments", "a/b/c", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitPath(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitPath(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitPath(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDiffSubscriptions(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		desired  []string
		wantAdd  []string
		wantRm   []string
	}{
		{
			name:     "no changes",
			existing: []string{"http://a.com", "http://b.com"},
			desired:  []string{"http://a.com", "http://b.com"},
			wantAdd:  nil,
			wantRm:   nil,
		},
		{
			name:     "add only",
			existing: []string{"http://a.com"},
			desired:  []string{"http://a.com", "http://b.com"},
			wantAdd:  []string{"http://b.com"},
			wantRm:   nil,
		},
		{
			name:     "remove only",
			existing: []string{"http://a.com", "http://b.com"},
			desired:  []string{"http://a.com"},
			wantAdd:  nil,
			wantRm:   []string{"http://b.com"},
		},
		{
			name:     "add and remove",
			existing: []string{"http://a.com", "http://b.com"},
			desired:  []string{"http://b.com", "http://c.com"},
			wantAdd:  []string{"http://c.com"},
			wantRm:   []string{"http://a.com"},
		},
		{
			name:     "empty existing",
			existing: nil,
			desired:  []string{"http://a.com"},
			wantAdd:  []string{"http://a.com"},
			wantRm:   nil,
		},
		{
			name:     "empty desired",
			existing: []string{"http://a.com"},
			desired:  nil,
			wantAdd:  nil,
			wantRm:   []string{"http://a.com"},
		},
		{
			name:     "both empty",
			existing: nil,
			desired:  nil,
			wantAdd:  nil,
			wantRm:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			add, rm := diffSubscriptions(tt.existing, tt.desired)
			if !slices.Equal(add, tt.wantAdd) {
				t.Errorf("add = %v, want %v", add, tt.wantAdd)
			}
			if !slices.Equal(rm, tt.wantRm) {
				t.Errorf("remove = %v, want %v", rm, tt.wantRm)
			}
		})
	}
}

func TestParseQueryInt64(t *testing.T) {
	tests := []struct {
		name  string
		query string
		key   string
		def   int64
		want  int64
	}{
		{"present", "since=100", "since", 0, 100},
		{"missing uses default", "", "since", 42, 42},
		{"invalid uses default", "since=abc", "since", 0, 0},
		{"negative", "since=-5", "since", 0, -5},
		{"zero", "since=0", "since", 99, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			got := parseQueryInt64(r, tt.key, tt.def)
			if got != tt.want {
				t.Errorf("parseQueryInt64() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"hello": "world"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	want := `{"hello":"world"}` + "\n"
	if w.Body.String() != want {
		t.Errorf("body = %q, want %q", w.Body.String(), want)
	}
}

func TestMetricsPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/v1/users", "/api/v1"},
		{"/api/v1/accounts/123/users", "/api/v1"},
		{"/api/2/subscriptions/alice/phone.json", "/api/v2"},
		{"/api/2/episodes/alice.json", "/api/v2"},
		{"/api/2/auth/alice/login.json", "/api/v2"},
		{"/users", "/users"},
		{"/admin/accounts", "/admin"},
		{"/login", "/login"},
		{"/healthz", "/healthz"},
		{"/", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := metricsPath(tt.path)
			if got != tt.want {
				t.Errorf("metricsPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestHandleClientConfig(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	r := httptest.NewRequest(http.MethodGet, "/clientconfig.json", nil)
	r.Host = "example.com"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["mygpo"] != "http://example.com/api/2" {
		t.Errorf("mygpo = %q, want %q", resp["mygpo"], "http://example.com/api/2")
	}
	if resp["mygpo-feedservice"] != "http://example.com" {
		t.Errorf("mygpo-feedservice = %q, want %q", resp["mygpo-feedservice"], "http://example.com")
	}
}

func TestHandleClientConfig_HTTPS(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	r := httptest.NewRequest(http.MethodGet, "/clientconfig.json", nil)
	r.Host = "example.com"
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.HasPrefix(resp["mygpo"], "https://") {
		t.Errorf("expected https scheme, got %q", resp["mygpo"])
	}
}

func TestHandleEmptyOPML(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	paths := []string{"/toplist/opml", "/search.opml", "/suggestions/opml", "/toplist.opml"}
	for _, path := range paths {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want %d", path, w.Code, http.StatusOK)
		}
		if ct := w.Header().Get("Content-Type"); ct != "text/x-opml+xml" {
			t.Errorf("%s: Content-Type = %q, want text/x-opml+xml", path, ct)
		}
		if !strings.Contains(w.Body.String(), "<opml") {
			t.Errorf("%s: body should contain <opml", path)
		}
	}
}

func TestHandleEmptyJSON(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	handler := api.Handler()

	paths := []string{"/api/2/tags/1.json", "/api/2/tag/linux/1.json", "/api/2/data/toplist.json"}
	for _, path := range paths {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want %d", path, w.Code, http.StatusOK)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: Content-Type = %q, want application/json", path, ct)
		}
		if w.Body.String() != "[]" {
			t.Errorf("%s: body = %q, want %q", path, w.Body.String(), "[]")
		}
	}
}

func TestHandleGetAllSubscriptions_OPML(t *testing.T) {
	store := newMockStore()
	store.users["testuser"] = &User{Username: "testuser", PWHash: hashPassword("testpass")}
	store.subscriptions["testuser"] = []string{"https://example.com/feed.xml"}
	api := newTestAPI(store)
	handler := api.Handler()

	r := authedRequest(http.MethodGet, "/subscriptions/testuser.opml", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/x-opml+xml" {
		t.Errorf("Content-Type = %q, want text/x-opml+xml", ct)
	}
	if !strings.Contains(w.Body.String(), "https://example.com/feed.xml") {
		t.Error("OPML should contain the subscription URL")
	}
}

