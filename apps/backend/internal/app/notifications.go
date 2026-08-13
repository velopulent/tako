package app

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/platform"
)

type Notification struct {
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Severity   string    `json:"severity"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source,omitempty"`
	Summary    string    `json:"summary"`
	State      string    `json:"state"`
	MutedUntil time.Time `json:"mutedUntil,omitempty"`
}

type notificationStore struct {
	mu    sync.Mutex
	items map[string]Notification
}

func newNotificationStore() *notificationStore {
	return &notificationStore{items: make(map[string]Notification)}
}

func (store *notificationStore) correlate(items []platform.IncidentEvent) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, item := range items {
		current := store.items[item.ID]
		state := current.State
		if state == "" {
			state = "open"
		}
		store.items[item.ID] = Notification{ID: item.ID, Timestamp: item.Timestamp, Severity: item.Severity, Kind: item.Kind, Source: item.Source, Summary: item.Summary, State: state, MutedUntil: current.MutedUntil}
	}
	if len(store.items) > 1000 {
		keys := make([]string, 0, len(store.items))
		for key := range store.items {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys[:len(keys)-1000] {
			delete(store.items, key)
		}
	}
}

func (store *notificationStore) list(state string) []Notification {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]Notification, 0, len(store.items))
	for _, item := range store.items {
		if state == "" || item.State == state {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Timestamp.After(result[right].Timestamp) })
	return result
}

func (store *notificationStore) transition(id, state string, muteUntil time.Time) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	item, ok := store.items[id]
	if !ok {
		return false
	}
	item.State = state
	item.MutedUntil = muteUntil
	store.items[id] = item
	return true
}

func (server *Server) incidents(writer http.ResponseWriter, request *http.Request) {
	since, until := time.Time{}, time.Time{}
	var err error
	if value := request.URL.Query().Get("since"); value != "" {
		since, err = time.Parse(time.RFC3339, value)
	}
	if value := request.URL.Query().Get("until"); value != "" && err == nil {
		until, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		problem(writer, http.StatusBadRequest, "invalid-incident-window", "Incident window must be RFC3339")
		return
	}
	timeline, err := platform.CorrelateIncidents(request.Context(), since, until)
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "incidents-unavailable", "The bounded journal correlation could not be completed")
		return
	}
	if server.notifications != nil {
		server.notifications.correlate(timeline.Items)
	}
	writeJSON(writer, http.StatusOK, timeline)
}

func (server *Server) notificationsList(writer http.ResponseWriter, request *http.Request) {
	if server.notifications == nil {
		writeJSON(writer, http.StatusOK, map[string]any{"items": []Notification{}})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": server.notifications.list(request.URL.Query().Get("state"))})
}

func (server *Server) notificationTransition(writer http.ResponseWriter, request *http.Request) {
	if server.notifications == nil {
		problem(writer, http.StatusNotFound, "notification-not-found", "Notification was not found")
		return
	}
	state := chi.URLParam(request, "state")
	if state != "acknowledged" && state != "muted" && state != "resolved" {
		problem(writer, http.StatusBadRequest, "invalid-notification-state", "Unsupported notification state")
		return
	}
	muteUntil := time.Time{}
	if state == "muted" {
		muteUntil = time.Now().Add(24 * time.Hour)
	}
	if !server.notifications.transition(chi.URLParam(request, "id"), state, muteUntil) {
		problem(writer, http.StatusNotFound, "notification-not-found", "Notification was not found")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"state": state, "mutedUntil": muteUntil})
}
