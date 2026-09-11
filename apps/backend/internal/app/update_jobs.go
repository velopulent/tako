package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
	"go.uber.org/zap"
)

func (server *Server) previewUpdates(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeUpdateOperation(writer, request, true)
	if !ok {
		return
	}
	if server.previewUpdatesFn == nil {
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "The update inventory service is unavailable")
		return
	}
	preview, err := server.previewUpdatesFn(request.Context(), operation)
	if err != nil {
		writeUpdateProblem(writer, err)
		return
	}
	if preview.Changes == nil {
		preview.Changes = []platform.UpdateChange{}
	}
	if preview.Warnings == nil {
		preview.Warnings = []string{}
	}
	if preview.Current.Packages == nil {
		preview.Current.Packages = []platform.UpdatePackage{}
	}
	if preview.Current.Recovery.RestartServices == nil {
		preview.Current.Recovery.RestartServices = []string{}
	}
	if preview.Current.Recovery.Hints == nil {
		preview.Current.Recovery.Hints = []string{}
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) startUpdateJob(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access first")
		return
	}
	operation, ok := decodeUpdateOperation(writer, request, false)
	if !ok {
		return
	}
	if server.jobs == nil {
		problem(writer, http.StatusServiceUnavailable, "jobs-unavailable", "Update jobs are unavailable")
		return
	}
	parameters, err := json.Marshal(operation)
	if err != nil {
		problem(writer, http.StatusBadRequest, "invalid-update-operation", "Update operation is invalid")
		return
	}
	var submittedID string
	job, err := server.jobs.SubmitParameterizedWithSetup(request.Context(), softwareUpdateJob, current.Identity.Username, true, parameters, func(id string) {
		submittedID = id
		server.updateTokensMu.Lock()
		server.updateTokens[id] = current.Identity.AdminToken
		server.updateTokensMu.Unlock()
	})
	if errors.Is(err, ErrJobQueueFull) {
		server.deleteUpdateToken(submittedID)
		problem(writer, http.StatusTooManyRequests, "jobs-busy", "An update job is already queued")
		return
	}
	if errors.Is(err, ErrJobManagerClosed) {
		server.deleteUpdateToken(submittedID)
		problem(writer, http.StatusServiceUnavailable, "jobs-unavailable", "Update jobs are unavailable")
		return
	}
	if err != nil {
		server.deleteUpdateToken(submittedID)
		server.logger.Error("update job submission failed", zap.Error(err))
		problem(writer, http.StatusInternalServerError, "jobs-unavailable", "Could not start update job")
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]any{"job": job})
}

func decodeUpdateOperation(writer http.ResponseWriter, request *http.Request, preview bool) (platform.UpdateOperation, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 64<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.UpdateOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-update-operation", "Update operation is invalid")
		return platform.UpdateOperation{}, false
	}
	if err := platform.ValidateUpdateOperation(operation, !preview); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-update-operation", "Update operation is invalid")
		return platform.UpdateOperation{}, false
	}
	return operation, true
}

func writeUpdateProblem(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, platform.ErrInvalidUpdateOperation):
		problem(writer, http.StatusBadRequest, "invalid-update-operation", "Update operation is invalid")
	case errors.Is(err, platform.ErrUpdateConflict):
		problem(writer, http.StatusConflict, "update-conflict", "The available update inventory changed; refresh before applying")
	case errors.Is(err, platform.ErrUpdateLocked):
		problem(writer, http.StatusConflict, "update-locked", "Another package operation currently holds the package-manager lock")
	case errors.Is(err, platform.ErrUpdateUnavailable):
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "No supported update backend is available")
	case errors.Is(err, platform.ErrUpdateRiskNotAccepted):
		problem(writer, http.StatusConflict, "update-risk-not-accepted", "Risky update changes require explicit confirmation")
	default:
		problem(writer, http.StatusBadGateway, "update-preview-failed", "Update preview could not be completed")
	}
}

func (server *Server) runSoftwareUpdateJob(ctx context.Context, job Job, update func(int, string) error) (json.RawMessage, error) {
	var operation platform.UpdateOperation
	decoder := json.NewDecoder(bytes.NewReader(job.Parameters))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF || platform.ValidateUpdateOperation(operation, true) != nil {
		return nil, platform.ErrInvalidUpdateOperation
	}
	adminToken := server.takeUpdateToken(job.ID)
	if adminToken == "" {
		return nil, auth.ErrServiceUnavailable
	}
	operation.JobID = job.ID
	startedAt := time.Now().UTC()
	target := "system/updates"
	defer func() {
		server.deleteUpdateToken(job.ID)
	}()
	if server.updateSlots == nil {
		return nil, errors.New("update concurrency control unavailable")
	}
	select {
	case server.updateSlots <- struct{}{}:
		defer func() { <-server.updateSlots }()
	case <-ctx.Done():
		server.recordOperation(context.Background(), job.Actor, target, startedAt, "canceled", "update canceled", true)
		return nil, ctx.Err()
	}
	if err := update(5, "Checking package-manager state"); err != nil {
		if errors.Is(err, ErrJobTerminal) {
			return nil, context.Canceled
		}
		return nil, err
	}
	if err := update(20, "Applying full system update"); err != nil {
		if errors.Is(err, ErrJobTerminal) {
			return nil, context.Canceled
		}
		return nil, err
	}
	if server.applyUpdatesFn == nil {
		return nil, auth.ErrServiceUnavailable
	}
	result, err := server.applyUpdatesFn(ctx, auth.UpdateRequest{AdminToken: adminToken, Operation: operation})
	if err != nil {
		failure := updateFailureMessage(err)
		status := "failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			status = "canceled"
			failure = "update canceled"
		}
		server.recordOperation(context.Background(), job.Actor, target, startedAt, status, failure, true)
		return nil, err
	}
	if err := update(85, "Verifying installed-software state"); err != nil {
		if errors.Is(err, ErrJobTerminal) {
			return nil, context.Canceled
		}
		return nil, err
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		server.recordOperation(context.Background(), job.Actor, target, startedAt, "failed", "update receipt could not be serialized", true)
		return nil, marshalErr
	}
	server.recordOperation(context.Background(), job.Actor, target, startedAt, "succeeded", "", true)
	if err := update(100, "Updates applied and verified"); err != nil {
		if errors.Is(err, ErrJobTerminal) {
			return nil, context.Canceled
		}
		return nil, err
	}
	return payload, nil
}

func updateFailureMessage(err error) string {
	switch {
	case errors.Is(err, platform.ErrUpdateConflict):
		return "update inventory changed"
	case errors.Is(err, platform.ErrUpdateLocked):
		return "package manager lock is held"
	case errors.Is(err, platform.ErrUpdateVerification):
		return "update verification failed"
	case errors.Is(err, platform.ErrUpdateApply):
		return "update command failed"
	case errors.Is(err, platform.ErrUpdateUnavailable), errors.Is(err, auth.ErrServiceUnavailable):
		return "update backend unavailable"
	default:
		return "update failed"
	}
}

func (server *Server) takeUpdateToken(id string) string {
	server.updateTokensMu.Lock()
	defer server.updateTokensMu.Unlock()
	return server.updateTokens[id]
}

func (server *Server) deleteUpdateToken(id string) {
	if id == "" {
		return
	}
	server.updateTokensMu.Lock()
	delete(server.updateTokens, id)
	server.updateTokensMu.Unlock()
}

func (server *Server) clearUpdateTokens() {
	server.updateTokensMu.Lock()
	server.updateTokens = make(map[string]string)
	server.updateTokensMu.Unlock()
}

func (server *Server) refreshUpdates(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access first")
		return
	}
	var body struct {
		Force *bool `json:"force"`
	}
	// Allow empty body; default force=true
	force := true
	if request.ContentLength > 0 {
		request.Body = http.MaxBytesReader(writer, request.Body, 1<<10)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err == nil && body.Force != nil {
			force = *body.Force
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Minute)
	defer cancel()
	if server.refreshUpdatesFn == nil {
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "The update inventory service is unavailable")
		return
	}
	status, err := server.refreshUpdatesFn(ctx, force)
	if err != nil {
		writeUpdateProblem(writer, err)
		return
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
	server.syncUpdateNotifications(status)
	writeJSON(writer, http.StatusOK, status)
}

func (server *Server) updateLiveStatus(writer http.ResponseWriter, request *http.Request) {
	if server.readUpdateLiveFn == nil {
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "Live update status is unavailable")
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		problem(writer, http.StatusInternalServerError, "stream-unavailable", "Streaming is unavailable")
		return
	}
	cursor, _ := strconv.ParseUint(request.Header.Get("Last-Event-ID"), 10, 64)
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("X-Accel-Buffering", "no")
	writeSnapshot := func(progress platform.UpdateProgress) bool {
		payload, err := json.Marshal(progress)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(writer, "event: progress\ndata: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	emit := func(kind string, sequence uint64, value any) bool {
		if sequence <= cursor {
			return true
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(writer, "id: %d\nevent: %s\ndata: %s\n\n", sequence, kind, payload); err != nil {
			return false
		}
		cursor = sequence
		flusher.Flush()
		return true
	}
	initial := true
	poll := func() bool {
		observation, err := server.readUpdateLiveFn(request.Context())
		if err != nil {
			return true
		}
		if initial {
			initial = false
			if !writeSnapshot(observation.Progress) {
				return false
			}
		}
		events := append([]platform.UpdateStreamEvent(nil), observation.Events...)
		if len(events) == 0 {
			events = append(events, platform.UpdateStreamEvent{Kind: "progress", Progress: observation.Progress})
			for _, output := range observation.Output {
				events = append(events, platform.UpdateStreamEvent{Kind: "output", Output: output})
			}
		}
		sort.Slice(events, func(left, right int) bool {
			sequence := func(event platform.UpdateStreamEvent) uint64 {
				if event.Kind == "output" {
					return event.Output.Sequence
				}
				return event.Progress.Sequence
			}
			return sequence(events[left]) < sequence(events[right])
		})
		for _, event := range events {
			if event.Kind == "output" {
				if !emit("output", event.Output.Sequence, event.Output) {
					return false
				}
			} else if !emit("progress", event.Progress.Sequence, event.Progress) {
				return false
			}
		}
		return true
	}
	if !poll() {
		return
	}
	pollTicker := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(15 * time.Second)
	defer pollTicker.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-pollTicker.C:
			if !poll() {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (server *Server) updateHistory(writer http.ResponseWriter, request *http.Request) {
	if server.readUpdateHistoryFn == nil {
		writeJSON(writer, http.StatusOK, map[string]any{"items": []platform.UpdateHistoryEntry{}, "available": false})
		return
	}
	items, err := server.readUpdateHistoryFn(request.Context())
	if err != nil {
		// History is a nice-to-have; degrade to an empty window instead of
		// failing the page.
		writeJSON(writer, http.StatusOK, map[string]any{"items": []platform.UpdateHistoryEntry{}, "available": false})
		return
	}
	if items == nil {
		items = []platform.UpdateHistoryEntry{}
	}
	for index := range items {
		if items[index].Packages == nil {
			items[index].Packages = map[string]string{}
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "available": true})
}

func (server *Server) kpatchStatus(writer http.ResponseWriter, request *http.Request) {
	kpatchCtx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	status, settings, err := server.hostBroker().ReadKpatch(kpatchCtx, credentialsFromContext(request.Context()))
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "Kernel live-patch status is unavailable")
		return
	}
	if status.Loaded == nil {
		status.Loaded = []string{}
	}
	if status.Installed == nil {
		status.Installed = []string{}
	}
	if settings.Missing == nil {
		settings.Missing = []string{}
	}
	if settings.Unavailable == nil {
		settings.Unavailable = []string{}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":   status,
		"settings": settings,
	})
}

func (server *Server) applyKpatchSettings(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access first")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.KpatchOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF || platform.ValidateKpatchOperation(operation) != nil {
		problem(writer, http.StatusBadRequest, "invalid-kpatch-operation", "Kpatch operation is invalid")
		return
	}
	if server.kpatchSettingsFn == nil {
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "The update configuration service is unavailable")
		return
	}
	startedAt := time.Now().UTC()
	kpatchCtx, cancel := context.WithTimeout(request.Context(), 5*time.Minute)
	defer cancel()
	settings, err := server.kpatchSettingsFn(kpatchCtx, auth.KpatchRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, "system/updates/kpatch", startedAt, "failed", "kpatch configuration failed", true)
		switch {
		case errors.Is(err, platform.ErrInvalidKpatchOperation):
			problem(writer, http.StatusBadRequest, "invalid-kpatch-operation", "Kpatch operation is invalid")
		default:
			problem(writer, http.StatusBadGateway, "kpatch-apply-failed", "The kernel live-patch configuration could not be applied")
		}
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, "system/updates/kpatch", startedAt, "succeeded", "", true)
	if settings.Missing == nil {
		settings.Missing = []string{}
	}
	if settings.Unavailable == nil {
		settings.Unavailable = []string{}
	}
	writeJSON(writer, http.StatusOK, settings)
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// syncUpdateNotifications surfaces a global notification while security (or
// any) updates are pending and resolves it once the inventory is clean.
func (server *Server) syncUpdateNotifications(status platform.UpdateStatus) {
	if server.notifications == nil || !status.Available || status.ExternalLock {
		return
	}
	now := time.Now().UTC()
	security := 0
	for _, item := range status.Packages {
		if item.Severity == "security" {
			security++
		}
	}
	switch {
	case security > 0:
		server.notifications.correlate([]platform.IncidentEvent{{
			ID:        "software-update-security",
			Timestamp: now,
			Severity:  "warning",
			Kind:      "software-update",
			Source:    status.Backend,
			Summary:   fmt.Sprintf("%d security update%s available", security, plural(security)),
		}})
		server.notifications.transition("software-update-info", "resolved", time.Time{})
	case len(status.Packages) > 0:
		server.notifications.correlate([]platform.IncidentEvent{{
			ID:        "software-update-info",
			Timestamp: now,
			Severity:  "info",
			Kind:      "software-update",
			Source:    status.Backend,
			Summary:   fmt.Sprintf("%d software update%s available", len(status.Packages), plural(len(status.Packages))),
		}})
		server.notifications.transition("software-update-security", "resolved", time.Time{})
	default:
		server.notifications.transition("software-update-security", "resolved", time.Time{})
		server.notifications.transition("software-update-info", "resolved", time.Time{})
	}
}
