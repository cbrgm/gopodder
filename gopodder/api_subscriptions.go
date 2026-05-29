package gopodder

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

type subscriptionChangeRequest struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type subscriptionChangeResponse struct {
	Timestamp  int64      `json:"timestamp"`
	UpdateURLs [][]string `json:"update_urls"`
}

type subscriptionChangesResponse struct {
	Add       []string `json:"add"`
	Remove    []string `json:"remove"`
	Timestamp int64    `json:"timestamp"`
}

func (a *API) handleGetAllSubscriptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := parseUserPath(r); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := UsernameFromContext(r.Context())

	subs, err := a.store.GetSubscriptions(r.Context(), username)
	if err != nil {
		a.logger.Error("failed to get all subscriptions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if subs == nil {
		subs = []string{}
	}

	if strings.HasSuffix(r.URL.Path, ".opml") {
		a.writeSubscriptionsOPML(w, username, subs)
		return
	}

	writeJSON(w, http.StatusOK, subs)
}

func (a *API) handleGetSubscriptions(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseDevicePath(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := UsernameFromContext(r.Context())

	subs, err := a.store.GetSubscriptions(r.Context(), username)
	if err != nil {
		a.logger.Error("failed to get subscriptions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if subs == nil {
		subs = []string{}
	}

	a.logger.Debug("device pulled subscriptions", "username", username, "device", deviceID, "count", len(subs))
	_ = a.store.UpdateDeviceLastActivity(r.Context(), username, deviceID, time.Now())
	writeJSON(w, http.StatusOK, subs)
}

func (a *API) handleUploadSubscriptions(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseDevicePath(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := UsernameFromContext(r.Context())

	var urls []string
	if err := json.NewDecoder(r.Body).Decode(&urls); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	urls = filterValidURLs(urls)

	a.logger.Debug("device pushed full subscription list (PUT)", "username", username, "device", deviceID, "count", len(urls))

	if err := a.store.UpsertDevice(r.Context(), username, deviceID, DeviceUpdate{}); err != nil {
		a.logger.Error("failed to ensure device exists", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Unix()
	if err := a.store.ReplaceSubscriptions(r.Context(), username, urls, now); err != nil {
		a.logger.Error("failed to upload subscriptions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_ = a.store.UpdateDeviceLastActivity(r.Context(), username, deviceID, time.Now())
	w.WriteHeader(http.StatusOK)
}

func (a *API) handleGetSubscriptionChanges(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseDevicePath(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := UsernameFromContext(r.Context())

	since := parseQueryInt64(r, "since", 0)

	changes, err := a.store.GetSubscriptionChanges(r.Context(), username, since)
	if err != nil {
		a.logger.Error("failed to get subscription changes", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if changes.Add == nil {
		changes.Add = []string{}
	}
	if changes.Remove == nil {
		changes.Remove = []string{}
	}

	a.logger.Debug("device pulled subscription changes", "username", username, "device", deviceID, "since", since, "add", len(changes.Add), "remove", len(changes.Remove))
	a.metrics.IncSyncOperation("pull")
	_ = a.store.UpdateDeviceLastActivity(r.Context(), username, deviceID, time.Now())
	writeJSON(w, http.StatusOK, subscriptionChangesResponse{
		Add:       changes.Add,
		Remove:    changes.Remove,
		Timestamp: time.Now().Unix() + 1,
	})
}

func (a *API) handleUploadSubscriptionChanges(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseDevicePath(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := UsernameFromContext(r.Context())

	var req subscriptionChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	beforeFilter := len(req.Add)
	req.Add = filterValidURLs(req.Add)
	if len(req.Add) < beforeFilter {
		a.logger.Warn("some subscription URLs were rejected (only http/https allowed)", "username", username, "device", deviceID, "rejected", beforeFilter-len(req.Add))
	}
	a.logger.Debug("device pushed subscription changes", "username", username, "device", deviceID, "add", len(req.Add), "remove", len(req.Remove))

	if hasOverlap(req.Add, req.Remove) {
		http.Error(w, "same feed in add and remove", http.StatusBadRequest)
		return
	}

	if err := a.store.UpsertDevice(r.Context(), username, deviceID, DeviceUpdate{}); err != nil {
		a.logger.Error("failed to ensure device exists", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	a.warnUnmatchedRemovals(r.Context(), username, deviceID, req.Remove)

	now := time.Now().Unix()
	if err := a.store.UpdateSubscriptions(r.Context(), username, req.Add, req.Remove, now); err != nil {
		a.logger.Error("failed to update subscriptions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	a.metrics.IncSyncOperation("push")
	_ = a.store.UpdateDeviceLastActivity(r.Context(), username, deviceID, time.Now())
	writeJSON(w, http.StatusOK, subscriptionChangeResponse{
		Timestamp:  now + 1,
		UpdateURLs: [][]string{},
	})
}

func (a *API) writeSubscriptionsOPML(w http.ResponseWriter, username string, subs []string) {
	outlines := make([]opmlOutline, 0, len(subs))
	for _, u := range subs {
		outlines = append(outlines, opmlOutline{Type: "rss", Text: u, Title: u, XMLURL: u})
	}
	doc := opmlDoc{
		Version: "2.0",
		Head:    opmlHead{Title: fmt.Sprintf("%s subscriptions", username)},
		Body:    opmlBody{Outlines: outlines},
	}

	w.Header().Set("Content-Type", "text/x-opml+xml")
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(doc)
}

func (a *API) warnUnmatchedRemovals(ctx context.Context, username, device string, removed []string) {
	if len(removed) == 0 {
		return
	}
	active, err := a.store.GetSubscriptions(ctx, username)
	if err != nil {
		return
	}
	activeSet := make(map[string]struct{}, len(active))
	for _, u := range active {
		activeSet[u] = struct{}{}
	}
	var unmatched []string
	for _, u := range removed {
		if _, ok := activeSet[u]; !ok {
			unmatched = append(unmatched, u)
		}
	}
	if len(unmatched) > 0 {
		a.logger.Warn("subscription removals had no effect (URLs not found, possibly stored under different URLs due to HTTP redirects)",
			"username", username, "device", device, "count", len(unmatched), "urls", unmatched)
	}
}

func hasOverlap(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, v := range a {
		set[v] = struct{}{}
	}
	return slices.ContainsFunc(b, func(v string) bool {
		_, ok := set[v]
		return ok
	})
}
