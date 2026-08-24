package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/packagekit"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/preferences"
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
	if !preview && operation.Preview {
		problem(writer, http.StatusBadRequest, "invalid-update-operation", "Update operation is invalid")
		return platform.UpdateOperation{}, false
	}
	operation.Preview = preview
	if err := platform.ValidateUpdateOperation(operation); err != nil {
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
	case errors.Is(err, platform.ErrInvalidAutoUpdatesOperation):
		problem(writer, http.StatusBadRequest, "invalid-auto-updates-operation", "Automatic updates operation is invalid")
	case errors.Is(err, platform.ErrAutoUpdatesUnavailable):
		problem(writer, http.StatusServiceUnavailable, "auto-updates-unavailable", "No supported automatic-update backend is available")
	case errors.Is(err, platform.ErrAutoUpdatesApply):
		problem(writer, http.StatusBadGateway, "auto-updates-apply-failed", "The automatic-update configuration could not be applied")
	default:
		problem(writer, http.StatusBadGateway, "update-preview-failed", "Update preview could not be completed")
	}
}

func (server *Server) runSoftwareUpdateJob(ctx context.Context, job preferences.Job, update func(int, string) error) (json.RawMessage, error) {
	var operation platform.UpdateOperation
	decoder := json.NewDecoder(bytes.NewReader(job.Parameters))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF || platform.ValidateUpdateOperation(operation) != nil || operation.Preview {
		return nil, platform.ErrInvalidUpdateOperation
	}
	adminToken := server.takeUpdateToken(job.ID)
	if adminToken == "" {
		return nil, auth.ErrServiceUnavailable
	}
	startedAt := time.Now().UTC()
	target := "system/updates/" + operation.Scope
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
		if errors.Is(err, preferences.ErrJobTerminal) {
			return nil, context.Canceled
		}
		return nil, err
	}
	if err := update(20, "Applying selected software updates"); err != nil {
		if errors.Is(err, preferences.ErrJobTerminal) {
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
		if errors.Is(err, preferences.ErrJobTerminal) {
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
		if errors.Is(err, preferences.ErrJobTerminal) {
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
	// Refresh requires session, but not necessarily admin
	_ = current
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
	// Use administrative token if available for privileged refresh, else session
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Minute)
	defer cancel()
	if err := platform.RefreshUpdatesCache(ctx, force); err != nil {
		writeUpdateProblem(writer, err)
		return
	}
	// Return fresh status after refresh
	status := platform.Updates(ctx)
	server.syncUpdateNotifications(status)
	writeJSON(writer, http.StatusOK, status)
}

// sharedPackageKitClient lazily creates the long-lived read-only D-Bus client
// used to observe update transactions. PackageKit itself is activated per
// call; only the bus connection is shared.
func (server *Server) sharedPackageKitClient() *packagekit.Client {
	server.sharedClientMu.Lock()
	defer server.sharedClientMu.Unlock()
	if server.sharedUpdateClient == nil {
		client, err := packagekit.New()
		if err != nil {
			return nil
		}
		server.sharedUpdateClient = client
	}
	return server.sharedUpdateClient
}

// transactionWatcher lazily starts the Package-signal collector that powers
// the "view update log" panel.
func (server *Server) transactionWatcher() *packagekit.TransactionWatcher {
	server.sharedClientMu.Lock()
	defer server.sharedClientMu.Unlock()
	if server.updateWatcher == nil {
		watcher, err := packagekit.NewTransactionWatcher()
		if err != nil {
			return nil
		}
		watcher.Start()
		server.updateWatcher = watcher
	}
	return server.updateWatcher
}

func (server *Server) closeSharedUpdateClients() {
	server.sharedClientMu.Lock()
	client, watcher := server.sharedUpdateClient, server.updateWatcher
	server.sharedUpdateClient, server.updateWatcher = nil, nil
	server.sharedClientMu.Unlock()
	if watcher != nil {
		watcher.Close()
	}
	if client != nil {
		client.Close()
	}
}

// updateObservation bundles the live snapshot and recorded action log for one
// observation point.
type updateObservation struct {
	Live platform.UpdateLive         `json:"live"`
	Log  []packagekit.ActionLogEntry `json:"log"`
}

func (server *Server) observeUpdates(ctx context.Context) updateObservation {
	observation := updateObservation{Live: platform.InactiveUpdateLive(), Log: []packagekit.ActionLogEntry{}}
	observeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client := server.sharedPackageKitClient()
	var snapshot *packagekit.LiveUpdateSnapshot
	if client != nil && client.Detect(observeCtx) {
		snapshot = client.UpdateSnapshot(observeCtx)
		observation.Live = platform.UpdateLiveFromSnapshot(snapshot)
	}
	if watcher := server.transactionWatcher(); watcher != nil {
		path := ""
		if snapshot != nil {
			path = snapshot.TransactionPath
		}
		observation.Log = watcher.LatestLog(path)
	}
	return observation
}

func (server *Server) updateLiveStatus(writer http.ResponseWriter, request *http.Request) {
	observation := server.observeUpdates(request.Context())
	writeJSON(writer, http.StatusOK, map[string]any{
		"live": observation.Live,
		"log":  observation.Log,
	})
}

func (server *Server) updateHistory(writer http.ResponseWriter, request *http.Request) {
	historyCtx, cancel := context.WithTimeout(request.Context(), 20*time.Second)
	defer cancel()
	items, err := platform.UpdateHistory(historyCtx)
	if err != nil {
		// History is a nice-to-have; degrade to an empty window instead of
		// failing the page.
		writeJSON(writer, http.StatusOK, map[string]any{"items": []platform.UpdateHistoryEntry{}, "available": false})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "available": true})
}

func (server *Server) cancelRunningUpdate(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access first")
		return
	}
	cancelCtx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	found, err := platform.CancelRunningUpdate(cancelCtx)
	if err != nil {
		writeUpdateProblem(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, "system/updates/cancel", time.Now().UTC(), "succeeded", "", true)
	writeJSON(writer, http.StatusOK, map[string]any{"canceled": found})
}

func (server *Server) automaticUpdatesStatus(writer http.ResponseWriter, request *http.Request) {
	if server.autoUpdatesStatusFn == nil {
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "The update configuration service is unavailable")
		return
	}
	statusCtx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	writeJSON(writer, http.StatusOK, server.autoUpdatesStatusFn(statusCtx))
}

func (server *Server) kpatchStatus(writer http.ResponseWriter, request *http.Request) {
	kpatchCtx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":   platform.InspectKpatchStatus(kpatchCtx),
		"settings": platform.InspectKpatchSettings(kpatchCtx),
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
	writeJSON(writer, http.StatusOK, settings)
}

func (server *Server) applyAutomaticUpdates(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access first")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.AutoUpdatesOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF || platform.ValidateAutoUpdatesOperation(operation) != nil {
		problem(writer, http.StatusBadRequest, "invalid-auto-updates-operation", "Automatic updates operation is invalid")
		return
	}
	if server.autoUpdatesFn == nil {
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "The update configuration service is unavailable")
		return
	}
	startedAt := time.Now().UTC()
	configCtx, cancel := context.WithTimeout(request.Context(), time.Minute)
	defer cancel()
	config, err := server.autoUpdatesFn(configCtx, auth.AutoUpdatesRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, "system/updates/auto", startedAt, "failed", autoUpdatesFailure(err), true)
		writeUpdateProblem(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, "system/updates/auto", startedAt, "succeeded", "", true)
	writeJSON(writer, http.StatusOK, config)
}

func autoUpdatesFailure(err error) string {
	switch {
	case errors.Is(err, platform.ErrInvalidAutoUpdatesOperation):
		return "automatic updates operation invalid"
	case errors.Is(err, platform.ErrAutoUpdatesUnavailable):
		return "automatic updates backend unavailable"
	case errors.Is(err, platform.ErrAutoUpdatesApply):
		return "automatic updates apply failed"
	default:
		return "automatic updates failed"
	}
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
