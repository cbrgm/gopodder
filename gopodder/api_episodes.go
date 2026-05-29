package gopodder

import (
	"encoding/json"
	"net/http"
	"time"
)

type episodeResponse struct {
	Actions   []Episode `json:"actions"`
	Timestamp int64     `json:"timestamp"`
}

type episodeUploadResponse struct {
	Timestamp  int64      `json:"timestamp"`
	UpdateURLs [][]string `json:"update_urls"`
}

func (a *API) handleGetEpisodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := parseUserPath(r); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := UsernameFromContext(r.Context())
	now := time.Now().Unix()

	query := EpisodeQuery{
		Username: username,
		Since:    parseQueryInt64(r, "since", 0),
	}

	if p := r.URL.Query().Get("podcast"); p != "" {
		query.Podcast = &p
	}
	if d := r.URL.Query().Get("device"); d != "" {
		query.Device = &d
	}

	episodes, err := a.store.GetEpisodes(r.Context(), query)
	if err != nil {
		a.logger.Error("failed to get episodes", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if episodes == nil {
		episodes = []Episode{}
	}

	a.logger.Debug("device pulled episode actions", "username", username, "count", len(episodes))
	writeJSON(w, http.StatusOK, episodeResponse{
		Actions:   episodes,
		Timestamp: now,
	})
}

func (a *API) handleUploadEpisodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := parseUserPath(r); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := UsernameFromContext(r.Context())

	var episodes []Episode
	if err := json.NewDecoder(r.Body).Decode(&episodes); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now().Unix()
	if err := a.store.UpdateEpisodes(r.Context(), username, episodes, now); err != nil {
		a.logger.Error("failed to update episodes", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	a.logger.Debug("device pushed episode actions", "username", username, "count", len(episodes))

	writeJSON(w, http.StatusOK, episodeUploadResponse{
		Timestamp:  now,
		UpdateURLs: [][]string{},
	})
}
