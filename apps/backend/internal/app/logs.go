package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
	"go.uber.org/zap"
)

const maxJournalExportBytes = 8 << 20

// queryJournal reads the whole journal while elevated and the user's own view
// otherwise.
func (server *Server) queryJournal(ctx context.Context, current session.Session, query platform.JournalQuery) (platform.JournalPage, error) {
	var page platform.JournalPage
	err := server.broker.Call(ctx, "journal.query", sessionCredentials(current), query, &page)
	if page.Items == nil {
		page.Items = []platform.LogEntry{}
	}
	return page, err
}

func (server *Server) logs(writer http.ResponseWriter, request *http.Request) {
	query, err := parseJournalQuery(request)
	if err != nil {
		reject(writer, "invalid-journal-query")
		return
	}
	page, err := server.queryJournal(request.Context(), currentSession(request), query)
	if err != nil {
		if request.Context().Err() == nil {
			fail(writer, err, "logs-unavailable")
		}
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func parseJournalQuery(request *http.Request) (platform.JournalQuery, error) {
	values := request.URL.Query()
	query := platform.JournalQuery{
		Limit:      200,
		Boot:       values.Get("boot"),
		Priority:   values.Get("priority"),
		Units:      values["unit"],
		Executable: values.Get("executable"),
		Text:       values.Get("text"),
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return query, err
		}
		query.Limit = limit
	}
	cursor, err := platform.DecodeJournalCursor(values.Get("cursor"))
	if err != nil {
		return query, err
	}
	query.Cursor = cursor
	if raw := values.Get("details"); raw != "" {
		if query.Details, err = strconv.ParseBool(raw); err != nil {
			return query, err
		}
	}
	for key, destination := range map[string]*time.Time{"since": &query.Since, "until": &query.Until} {
		if raw := values.Get(key); raw != "" {
			parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
			if parseErr != nil {
				return query, parseErr
			}
			*destination = parsed
		}
	}
	return query, query.Validate()
}

func (server *Server) logExport(writer http.ResponseWriter, request *http.Request) {
	query, err := parseJournalQuery(request)
	if request.URL.Query().Get("limit") == "" {
		query.Limit = platform.MaxJournalPageSize
	}
	if err != nil || query.Validate() != nil {
		reject(writer, "invalid-journal-query")
		return
	}
	format := request.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		problem(writer, http.StatusBadRequest, "invalid-export-format", "Export format must be csv or json")
		return
	}
	page, err := server.queryJournal(request.Context(), currentSession(request), query)
	if err != nil {
		if request.Context().Err() == nil {
			fail(writer, err, "logs-unavailable")
		}
		return
	}
	var payload []byte
	contentType := "application/json"
	if format == "json" {
		payload, err = json.Marshal(page)
	} else {
		payload, err = encodeJournalCSV(page.Items)
		contentType = "text/csv; charset=utf-8"
	}
	if err != nil || len(payload) > maxJournalExportBytes {
		problem(writer, http.StatusRequestEntityTooLarge, "journal-export-too-large", "The filtered journal export is too large; narrow the filter")
		return
	}
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Content-Disposition", `attachment; filename="tako-journal-`+time.Now().UTC().Format("20060102-150405")+`.`+format+`"`)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(payload)
}

func encodeJournalCSV(entries []platform.LogEntry) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"timestamp", "priority", "unit", "message", "details"}); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		details, err := json.Marshal(entry.Details)
		if err != nil {
			return nil, err
		}
		if err := writer.Write([]string{entry.Timestamp, entry.Priority, entry.Unit, csvSafe(entry.Message), string(details)}); err != nil {
			return nil, err
		}
		if output.Len() > maxJournalExportBytes {
			return nil, errors.New("journal export exceeds bounded output")
		}
	}
	writer.Flush()
	return output.Bytes(), writer.Error()
}

// csvSafe neutralizes spreadsheet formula injection in exported log text.
func csvSafe(value string) string {
	if value != "" && strings.ContainsAny(value[:1], "=+-@\t\r") {
		return "'" + value
	}
	return value
}

func (server *Server) logStream(writer http.ResponseWriter, request *http.Request) {
	query, err := parseJournalQuery(request)
	if err != nil || query.Cursor != "" {
		reject(writer, "invalid-journal-query")
		return
	}
	credentials := sessionCredentials(currentSession(request))
	serveEvents(writer, request, func(ctx context.Context, send func(string, any) error) error {
		return streamAs(ctx, server.broker, "journal.follow", credentials, query, func(entry platform.LogEntry) error {
			return send("log", entry)
		})
	})
}

const (
	// incidentAckRetention outlasts the longest incident window (seven days),
	// so older acknowledgements can never match a listed incident.
	incidentAckRetention = 8 * 24 * time.Hour
	maxIncidentAcks      = 4096
)

// incidentAcks remembers when operators acknowledged each incident. It is
// shared by all operators and saved in the data directory, so it survives
// gateway restarts. An incident that recurs after its acknowledgement opens
// again.
type incidentAcks struct {
	mu    sync.Mutex
	path  string
	times map[string]time.Time
}

// newIncidentAcks loads saved acknowledgements from path. An empty path keeps
// them in memory only.
func newIncidentAcks(path string) *incidentAcks {
	store := &incidentAcks{path: path, times: make(map[string]time.Time)}
	if path == "" {
		return store
	}
	if content, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(content, &store.times); err != nil {
			zap.L().Warn("ignoring unreadable incident acknowledgements", zap.String("path", path), zap.Error(err))
			store.times = make(map[string]time.Time)
		}
	}
	store.pruneLocked(time.Now())
	return store
}

func (store *incidentAcks) pruneLocked(now time.Time) {
	for id, at := range store.times {
		if now.Sub(at) > incidentAckRetention {
			delete(store.times, id)
		}
	}
	for len(store.times) >= maxIncidentAcks {
		oldest, oldestAt := "", time.Time{}
		for id, at := range store.times {
			if oldest == "" || at.Before(oldestAt) {
				oldest, oldestAt = id, at
			}
		}
		delete(store.times, oldest)
	}
}

func (store *incidentAcks) saveLocked() error {
	if store.path == "" {
		return nil
	}
	content, err := json.Marshal(store.times)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(store.path, content, 0o600)
}

func (store *incidentAcks) apply(items []platform.Incident) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range items {
		if at, ok := store.times[items[index].ID]; ok && !items[index].LastSeen.After(at) {
			items[index].State = "acknowledged"
		}
	}
}

func (store *incidentAcks) set(id string, acknowledged bool, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if acknowledged {
		store.pruneLocked(now)
		store.times[id] = now
	} else {
		delete(store.times, id)
	}
	return store.saveLocked()
}

func (server *Server) incidentTimeline(writer http.ResponseWriter, request *http.Request) {
	var since, until time.Time
	var err error
	if value := request.URL.Query().Get("since"); value != "" {
		since, err = time.Parse(time.RFC3339, value)
	}
	if value := request.URL.Query().Get("until"); value != "" && err == nil {
		until, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		problem(writer, http.StatusBadRequest, "invalid-incident-window", "The incident window must be RFC 3339 timestamps")
		return
	}
	current := currentSession(request)
	timeline, err := platform.CorrelateIncidents(request.Context(), since, until, func(ctx context.Context, query platform.JournalQuery) (platform.JournalPage, error) {
		return server.queryJournal(ctx, current, query)
	})
	if errors.Is(err, platform.ErrInvalidIncidentWindow) {
		problem(writer, http.StatusBadRequest, "invalid-incident-window", "The incident window must be at most seven days")
		return
	}
	if err != nil {
		fail(writer, err, "logs-unavailable")
		return
	}
	server.incidents.apply(timeline.Items)
	writeJSON(writer, http.StatusOK, timeline)
}

func (server *Server) incidentTransition(writer http.ResponseWriter, request *http.Request) {
	if !requireAdmin(writer, currentSession(request)) {
		return
	}
	id, state := chi.URLParam(request, "id"), chi.URLParam(request, "state")
	if id == "" || len(id) > 128 || (state != "open" && state != "acknowledged") {
		problem(writer, http.StatusBadRequest, "invalid-incident-state", "Unsupported incident state")
		return
	}
	if err := server.incidents.set(id, state == "acknowledged", time.Now()); err != nil {
		// The change holds in memory; only its persistence failed.
		server.logger.Warn("incident acknowledgement not saved", zap.Error(err))
	}
	writeJSON(writer, http.StatusOK, map[string]any{"id": id, "state": state})
}
