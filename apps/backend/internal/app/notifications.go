package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
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
	timeline, err := platform.CorrelateIncidentsWithLogs(request.Context(), since, until, server.queryLogs)
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

func (server *Server) certificates(writer http.ResponseWriter, request *http.Request) {
	var (
		status platform.CertificateStatus
		err    error
	)
	if path, owned := server.gatewayCertificatePath(); owned {
		status, err = platform.InspectCertificate(path)
	} else {
		status, err = server.hostBroker().ReadCertificate(request.Context(), credentialsFromContext(request.Context()))
	}
	// Certificate status is intentionally a degraded 200 response. The UI can
	// explain a missing or unreadable certificate without turning the settings
	// page into a gateway error, while the actual external-path read remains in
	// sessiond.
	_ = err
	writeJSON(writer, http.StatusOK, status)
}

func (server *Server) gatewayCertificatePath() (string, bool) {
	path := server.config.Certificate
	if path == "" {
		return filepath.Join(server.config.DataDir, "tako.crt"), true
	}
	dataDir, err := filepath.Abs(server.config.DataDir)
	if err != nil {
		return "", false
	}
	certificate, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	if !certificatePathWithin(dataDir, certificate) {
		return "", false
	}
	return certificate, true
}

// certificatePathWithin applies the gateway-owned DataDir boundary after
// resolving symlinks. A configured path outside that boundary is intentionally
// left for root sessiond, even when the symlink itself is stored in DataDir.
func certificatePathWithin(dataDir, certificate string) bool {
	resolvedDataDir, dataErr := filepath.EvalSymlinks(dataDir)
	if dataErr != nil {
		resolvedDataDir = dataDir
	}

	// A dangling symlink must not become gateway-owned merely because its
	// lexical parent is inside DataDir. Let sessiond report its stable status.
	if info, err := os.Lstat(certificate); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolvedCertificate, err := filepath.EvalSymlinks(certificate)
		if err != nil {
			return false
		}
		certificate = resolvedCertificate
	} else if err == nil {
		resolvedCertificate, resolveErr := filepath.EvalSymlinks(certificate)
		if resolveErr != nil {
			return false
		}
		certificate = resolvedCertificate
	} else if !os.IsNotExist(err) {
		return false
	} else {
		// The leaf may not exist yet. Resolve its parent so a symlinked parent
		// cannot move an apparently local path outside the owned directory.
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(certificate))
		if parentErr == nil {
			certificate = filepath.Join(parent, filepath.Base(certificate))
		}
	}

	relative, err := filepath.Rel(resolvedDataDir, certificate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
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

func (server *Server) supportReport(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if current.Identity.AdminToken == "" || !time.Now().Before(current.AdminUntil) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before collecting a support report")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.SupportReportOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-support-report", "Support report confirmation is required")
		return
	}
	report, err := server.hostBroker().CollectSupportReport(request.Context(), auth.SupportReportRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	if err != nil {
		if errors.Is(err, platform.ErrInvalidSupportReport) {
			problem(writer, http.StatusBadRequest, "invalid-support-report", "Support report confirmation is required")
		} else {
			problem(writer, http.StatusBadGateway, "support-report-failed", "The support report could not be collected")
		}
		return
	}
	writeJSON(writer, http.StatusAccepted, report)
}
