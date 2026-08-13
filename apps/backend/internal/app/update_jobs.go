package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/velopulent/tako/internal/auth"
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
		return nil, err
	}
	if err := update(20, "Applying selected software updates"); err != nil {
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
		return nil, err
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		server.recordOperation(context.Background(), job.Actor, target, startedAt, "failed", "update receipt could not be serialized", true)
		return nil, marshalErr
	}
	server.recordOperation(context.Background(), job.Actor, target, startedAt, "succeeded", "", true)
	if err := update(100, "Updates applied and verified"); err != nil {
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
