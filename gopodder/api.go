package gopodder

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const apiPrefix = "/api/2"

type BuildInfo struct {
	Version   string
	Revision  string
	BuildDate string
	GoVersion string
	Platform  string
}

type API struct {
	logger     *slog.Logger
	store      Store
	metrics    Metrics
	build      BuildInfo
	listenAddr string
	dbBackend  string
	startedAt  time.Time
}

func NewAPI(logger *slog.Logger, store Store, metrics Metrics, build BuildInfo, listenAddr, dbBackend string) *API {
	return &API{
		logger:     logger,
		store:      store,
		metrics:    metrics,
		build:      build,
		listenAddr: listenAddr,
		dbBackend:  dbBackend,
		startedAt:  time.Now(),
	}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /healthz", a.handleHealthz)

	// Client configuration
	mux.HandleFunc("GET /clientconfig.json", a.handleClientConfig)

	// gPodder API v2 routes
	mux.HandleFunc(fmt.Sprintf("POST %s/auth/{username...}", apiPrefix), a.routeAuth)
	mux.HandleFunc(fmt.Sprintf("GET %s/devices/{username...}", apiPrefix), a.withAuth(a.handleListDevices))
	mux.HandleFunc(fmt.Sprintf("POST %s/devices/{rest...}", apiPrefix), a.withAuth(a.handleUpdateDevice))
	mux.HandleFunc(fmt.Sprintf("GET %s/subscriptions/{rest...}", apiPrefix), a.withAuth(a.handleGetSubscriptionChanges))
	mux.HandleFunc(fmt.Sprintf("POST %s/subscriptions/{rest...}", apiPrefix), a.withAuth(a.handleUploadSubscriptionChanges))
	mux.HandleFunc("GET /subscriptions/{rest...}", a.withAuth(a.routeGetSubscriptions))
	mux.HandleFunc("PUT /subscriptions/{rest...}", a.withAuth(a.handleUploadSubscriptions))
	mux.HandleFunc(fmt.Sprintf("PUT %s/subscriptions/{rest...}", apiPrefix), a.withAuth(a.handleUploadSubscriptions))
	mux.HandleFunc(fmt.Sprintf("GET %s/episodes/{username...}", apiPrefix), a.withAuth(a.handleGetEpisodes))
	mux.HandleFunc(fmt.Sprintf("POST %s/episodes/{username...}", apiPrefix), a.withAuth(a.handleUploadEpisodes))

	// Stub endpoints for directory/discovery features (gPodder desktop compatibility)
	mux.HandleFunc("GET /toplist/opml", a.handleEmptyOPML)
	mux.HandleFunc("GET /search.opml", a.handleEmptyOPML)
	mux.HandleFunc("GET /suggestions/opml", a.handleEmptyOPML)
	mux.HandleFunc("GET /toplist.opml", a.handleEmptyOPML)
	mux.HandleFunc("GET /api/2/tags/{rest...}", a.handleEmptyJSON)
	mux.HandleFunc("GET /api/2/tag/{rest...}", a.handleEmptyJSON)
	mux.HandleFunc("GET /api/2/data/{rest...}", a.handleEmptyJSON)

	// API v1 (API key authenticated)
	a.registerAPIv1Routes(mux)

	// Web UI routes
	webHandler := NewWebHandler(a)
	webHandler.RegisterRoutes(mux)

	return withCORS(withMaxBody(a.withRequestLogging(csrfProtect(webHandler.SetupGuard(mux)))))
}

func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Ping(r.Context()); err != nil {
		http.Error(w, "database unreachable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ok\n"))
}

func (a *API) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	base := scheme + "://" + r.Host

	resp := map[string]string{
		"mygpo":             base + "/api/2",
		"mygpo-feedservice": base,
		"update_timeout":    "604800",
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleEmptyOPML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-opml+xml")
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0"><head><title>goPodder</title></head><body></body></opml>`))
}

func (a *API) handleEmptyJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("[]"))
}

const activityDebounce = 60 * time.Second

func (a *API) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := a.authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="gopodder"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		now := time.Now()
		if user.LastActivity == nil || now.Sub(*user.LastActivity) > activityDebounce {
			_ = a.store.UpdateUserLastActivity(r.Context(), user.Username, now)
		}
		ctx := context.WithValue(r.Context(), contextKeyUsername, user.Username)
		next(w, r.WithContext(ctx))
	}
}

func (a *API) routeAuth(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.PathValue("username"))
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	r.SetPathValue("username", parts[0])
	switch stripExtension(parts[1]) {
	case "login":
		a.handleLogin(w, r)
	case "logout":
		a.handleLogout(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *API) routeGetSubscriptions(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.PathValue("rest"))
	if len(parts) == 1 {
		r.SetPathValue("username", parts[0])
		a.handleGetAllSubscriptions(w, r)
		return
	}
	a.handleGetSubscriptions(w, r)
}

func parseUserPath(r *http.Request) (string, bool) {
	pathUser := stripExtension(r.PathValue("username"))
	return pathUser, pathUser == UsernameFromContext(r.Context())
}

func parseDevicePath(r *http.Request) (deviceID string, ok bool) {
	parts := splitPath(r.PathValue("rest"))
	if len(parts) < 2 {
		return "", false
	}
	pathUser := parts[0]
	if pathUser != UsernameFromContext(r.Context()) {
		return "", false
	}
	return stripExtension(parts[1]), true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func parseQueryInt64(r *http.Request, key string, def int64) int64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return i
}

func stripExtension(s string) string {
	s = strings.TrimSuffix(s, ".json")
	s = strings.TrimSuffix(s, ".opml")
	s = strings.TrimSuffix(s, ".jsonp")
	s = strings.TrimSuffix(s, ".txt")
	return s
}

func splitPath(s string) []string {
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		return nil
	}
	return strings.Split(s, "/")
}
