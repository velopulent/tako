package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func (server *Server) previewLocalGroup(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before managing local groups")
		return
	}
	operation, ok := decodeLocalGroupOperation(writer, request, true)
	if !ok {
		return
	}
	preview, err := server.hostBroker().PreviewLocalGroup(request.Context(), current.Identity.AdminToken, operation)
	if err != nil {
		writeLocalGroupError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) applyLocalGroup(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before managing local groups")
		return
	}
	operation, ok := decodeLocalGroupOperation(writer, request, false)
	if !ok {
		return
	}
	operation.Preview = false
	startedAt := time.Now().UTC()
	state, err := server.hostBroker().ApplyLocalGroup(request.Context(), current.Identity.AdminToken, operation)
	target := "group/" + operation.Group + "/" + operation.Action
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "local group operation failed", true)
		writeLocalGroupError(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", true)
	writeJSON(writer, http.StatusOK, state)
}

func decodeLocalGroupOperation(writer http.ResponseWriter, request *http.Request, preview bool) (platform.LocalGroupOperation, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.LocalGroupOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-local-group-operation", "The local group operation is invalid")
		return platform.LocalGroupOperation{}, false
	}
	operation.Preview = preview
	if err := platform.ValidateLocalGroupOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-local-group-operation", "The local group operation is invalid")
		return platform.LocalGroupOperation{}, false
	}
	return operation, true
}

func writeLocalGroupError(writer http.ResponseWriter, err error) {
	code := "local-group-unavailable"
	message := "Local group management is unavailable"
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, platform.ErrInvalidLocalGroupOperation), strings.Contains(err.Error(), "invalid local group operation"):
		code, message, status = "invalid-local-group-operation", "The local group operation is invalid", http.StatusBadRequest
	case errors.Is(err, platform.ErrLocalGroupConflict), strings.Contains(err.Error(), "local group conflict"):
		code, message, status = "local-group-conflict", "The group changed; preview again before applying", http.StatusConflict
	case errors.Is(err, platform.ErrLocalGroupNotFound), strings.Contains(err.Error(), "local group not found"):
		code, message, status = "local-group-not-found", "The local group no longer exists", http.StatusConflict
	case errors.Is(err, platform.ErrLocalGroupReadOnly), strings.Contains(err.Error(), "local group is read-only"):
		code, message, status = "local-group-read-only", "Remote NSS groups are read-only in Tako", http.StatusForbidden
	case errors.Is(err, platform.ErrLocalGroupProtected), strings.Contains(err.Error(), "local group is protected"):
		code, message, status = "local-group-protected", "This system group cannot be deleted", http.StatusForbidden
	case errors.Is(err, platform.ErrLocalGroupVerification), strings.Contains(err.Error(), "local group verification failed"):
		code, message, status = "local-group-verification-failed", "The group operation could not be verified", http.StatusBadGateway
	}
	problem(writer, status, code, message)
}
