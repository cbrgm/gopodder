package gopodder

import (
	"encoding/json"
	"net/http"
)

func (a *API) handleListDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := parseUserPath(r); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	username := UsernameFromContext(r.Context())

	devices, err := a.store.ListDevices(r.Context(), username)
	if err != nil {
		a.logger.Error("failed to list devices", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if devices == nil {
		devices = []Device{}
	}

	subs, _ := a.store.GetSubscriptions(r.Context(), username)
	subCount := int64(len(subs))
	for i := range devices {
		devices[i].Subscriptions = subCount
	}

	writeJSON(w, http.StatusOK, devices)
}

func (a *API) handleUpdateDevice(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseDevicePath(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var update DeviceUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := a.store.UpsertDevice(r.Context(), UsernameFromContext(r.Context()), deviceID, update); err != nil {
		a.logger.Error("failed to update device", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
