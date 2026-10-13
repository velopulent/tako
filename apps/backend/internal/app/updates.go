package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

// normalizeUpdateStatus keeps required arrays non-null for the dashboard.
func normalizeUpdateStatus(status *platform.UpdateStatus) {
	if status.Fingerprint == "" {
		status.Fingerprint = platform.UpdateFingerprint(*status)
	}
	if status.Packages == nil {
		status.Packages = []platform.UpdatePackage{}
	}
	if status.Recovery.RestartServices == nil {
		status.Recovery.RestartServices = []string{}
	}
	if status.Recovery.Hints == nil {
		status.Recovery.Hints = []string{}
	}
}

func (server *Server) updates(writer http.ResponseWriter, request *http.Request) {
	var status platform.UpdateStatus
	// A backend failure is never reported as "up to date".
	if err := server.broker.Call(request.Context(), "updates.status", sessionCredentials(currentSession(request)), nil, &status); err != nil {
		fail(writer, err, "updates-unavailable")
		return
	}
	normalizeUpdateStatus(&status)
	writeJSON(writer, http.StatusOK, status)
}

func decodeUpdate(writer http.ResponseWriter, request *http.Request, apply bool) (platform.UpdateOperation, bool) {
	operation, ok := decodeBody[platform.UpdateOperation](writer, request, 64<<10, "invalid-update-operation")
	if ok && platform.ValidateUpdateOperation(operation, apply) != nil {
		reject(writer, "invalid-update-operation")
		return operation, false
	}
	return operation, ok
}

func (server *Server) previewUpdates(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeUpdate(writer, request, false)
	if !ok {
		return
	}
	var preview platform.UpdatePreview
	if err := server.broker.Call(request.Context(), "updates.preview", sessionCredentials(currentSession(request)), operation, &preview); err != nil {
		fail(writer, err, "updates-unavailable")
		return
	}
	if preview.Changes == nil {
		preview.Changes = []platform.UpdateChange{}
	}
	if preview.Warnings == nil {
		preview.Warnings = []string{}
	}
	normalizeUpdateStatus(&preview.Current)
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) refreshUpdates(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	operation := auth.UpdateRefreshOperation{Force: true}
	if request.ContentLength != 0 {
		body, ok := decodeBody[struct {
			Force *bool `json:"force"`
		}](writer, request, 1<<10, "invalid-request")
		if !ok {
			return
		}
		if body.Force != nil {
			operation.Force = *body.Force
		}
	}
	var status platform.UpdateStatus
	if err := server.broker.Call(request.Context(), "updates.refresh", adminCredentials(current), operation, &status); err != nil {
		fail(writer, err, "updates-unavailable")
		return
	}
	normalizeUpdateStatus(&status)
	writeJSON(writer, http.StatusOK, status)
}

func (server *Server) startUpdateJob(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	operation, ok := decodeUpdate(writer, request, true)
	if !ok {
		return
	}
	credentials := adminCredentials(current)
	job, err := server.jobs.Submit(softwareUpdateJob, current.Identity.Username, func(ctx context.Context, progress func(int, string) error) (json.RawMessage, error) {
		if err := progress(20, "Applying system updates"); err != nil {
			return nil, err
		}
		var result platform.UpdateResult
		err := server.broker.Call(ctx, "updates.apply", credentials, operation, &result)
		server.audit(request, "system/updates", err, true)
		if err != nil {
			return nil, errors.New(problemMessage(err, "updates-unavailable"))
		}
		if err := progress(95, "Verifying installed software"); err != nil {
			return nil, err
		}
		return json.Marshal(result)
	})
	switch {
	case errors.Is(err, ErrJobBusy):
		problem(writer, http.StatusConflict, "jobs-busy", "An update is already running")
	case err != nil:
		problem(writer, http.StatusServiceUnavailable, "jobs-unavailable", "Update jobs are unavailable")
	default:
		writeJSON(writer, http.StatusAccepted, map[string]any{"job": job})
	}
}

// problemMessage returns the operator-facing message for err.
func problemMessage(err error, fallback string) string {
	code := auth.ErrorCode(err, fallback)
	if spec, ok := problems[code]; ok {
		return spec.message
	}
	return problems[fallback].message
}

func (server *Server) updateLiveStatus(writer http.ResponseWriter, request *http.Request) {
	credentials := sessionCredentials(currentSession(request))
	cursor, _ := strconv.ParseUint(request.Header.Get("Last-Event-ID"), 10, 64)
	serveEvents(writer, request, func(ctx context.Context, send func(string, any) error) error {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		first := true
		for {
			var observation platform.UpdateObservation
			if err := server.broker.Call(ctx, "updates.live", credentials, nil, &observation); err == nil {
				if first {
					if err := send("progress", observation.Progress); err != nil {
						return err
					}
					first = false
				}
				for _, event := range orderedUpdateEvents(observation) {
					sequence := event.Progress.Sequence
					if event.Kind == "output" {
						sequence = event.Output.Sequence
					}
					if sequence <= cursor {
						continue
					}
					cursor = sequence
					var payload any = event.Progress
					if event.Kind == "output" {
						payload = event.Output
					}
					if err := send(event.Kind, payload); err != nil {
						return err
					}
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	})
}

func orderedUpdateEvents(observation platform.UpdateObservation) []platform.UpdateStreamEvent {
	events := append([]platform.UpdateStreamEvent(nil), observation.Events...)
	if len(events) == 0 {
		events = append(events, platform.UpdateStreamEvent{Kind: "progress", Progress: observation.Progress})
		for _, output := range observation.Output {
			events = append(events, platform.UpdateStreamEvent{Kind: "output", Output: output})
		}
	}
	sequence := func(event platform.UpdateStreamEvent) uint64 {
		if event.Kind == "output" {
			return event.Output.Sequence
		}
		return event.Progress.Sequence
	}
	sort.SliceStable(events, func(i, j int) bool { return sequence(events[i]) < sequence(events[j]) })
	return events
}

func (server *Server) updateHistory(writer http.ResponseWriter, request *http.Request) {
	var items []platform.UpdateHistoryEntry
	// History is supplementary; an unavailable backend degrades to empty.
	err := server.broker.Call(request.Context(), "updates.history", sessionCredentials(currentSession(request)), nil, &items)
	if items == nil {
		items = []platform.UpdateHistoryEntry{}
	}
	for index := range items {
		if items[index].Packages == nil {
			items[index].Packages = map[string]string{}
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "available": err == nil})
}

func (server *Server) kpatchStatus(writer http.ResponseWriter, request *http.Request) {
	var result auth.KpatchReadResult
	if err := server.broker.Call(request.Context(), "kpatch.read", sessionCredentials(currentSession(request)), nil, &result); err != nil {
		fail(writer, err, "updates-unavailable")
		return
	}
	normalizeKpatch(&result.Settings)
	if result.Status.Loaded == nil {
		result.Status.Loaded = []string{}
	}
	if result.Status.Installed == nil {
		result.Status.Installed = []string{}
	}
	writeJSON(writer, http.StatusOK, result)
}

func normalizeKpatch(settings *platform.KpatchSettingsStatus) {
	if settings.Missing == nil {
		settings.Missing = []string{}
	}
	if settings.Unavailable == nil {
		settings.Unavailable = []string{}
	}
}

func (server *Server) applyKpatchSettings(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	operation, ok := decodeBody[platform.KpatchOperation](writer, request, 1<<10, "invalid-kpatch-operation")
	if !ok {
		return
	}
	if platform.ValidateKpatchOperation(operation) != nil {
		reject(writer, "invalid-kpatch-operation")
		return
	}
	var settings platform.KpatchSettingsStatus
	err := server.broker.Call(request.Context(), "kpatch.apply", adminCredentials(current), operation, &settings)
	server.audit(request, fmt.Sprintf("system/updates/kpatch/%t", *operation.Apply), err, true)
	if err != nil {
		fail(writer, err, "kpatch-apply-failed")
		return
	}
	normalizeKpatch(&settings)
	writeJSON(writer, http.StatusOK, settings)
}
