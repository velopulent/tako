package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func (server *Server) previewLocalAccount(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before managing local accounts")
		return
	}
	operation, ok := decodeLocalAccountOperation(writer, request, true)
	if !ok {
		return
	}
	if server.previewAccountFn == nil {
		problem(writer, http.StatusServiceUnavailable, "local-account-unavailable", "Privileged local account service is unavailable")
		return
	}
	preview, err := server.previewAccountFn(request.Context(), auth.LocalAccountRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	if err != nil {
		writeLocalAccountError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) applyLocalAccount(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before managing local accounts")
		return
	}
	operation, ok := decodeLocalAccountOperation(writer, request, false)
	if !ok {
		return
	}
	operation.Preview = false
	startedAt := time.Now().UTC()
	if server.applyAccountFn == nil {
		problem(writer, http.StatusServiceUnavailable, "local-account-unavailable", "Privileged local account service is unavailable")
		return
	}
	state, err := server.applyAccountFn(request.Context(), auth.LocalAccountRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	target := "user/" + operation.Username + "/" + operation.Action
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "local account operation failed", true)
		writeLocalAccountError(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", true)
	writeJSON(writer, http.StatusOK, state)
}

func decodeLocalAccountOperation(writer http.ResponseWriter, request *http.Request, preview bool) (platform.LocalAccountOperation, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.LocalAccountOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-local-account-operation", "The local account operation is invalid")
		return platform.LocalAccountOperation{}, false
	}
	operation.Preview = preview
	if err := platform.ValidateLocalAccountOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-local-account-operation", "The local account operation is invalid")
		return platform.LocalAccountOperation{}, false
	}
	return operation, true
}

func hasAdministrativeAccess(current session.Session) bool {
	return current.Identity.AdminToken != "" && time.Now().Before(current.AdminUntil)
}

func writeLocalAccountError(writer http.ResponseWriter, err error) {
	message := "Local account management is unavailable"
	code := "local-account-unavailable"
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, platform.ErrInvalidLocalAccountOperation), strings.Contains(err.Error(), "invalid-local-account-operation"):
		code, message, status = "invalid-local-account-operation", "The local account operation is invalid", http.StatusBadRequest
	case errors.Is(err, platform.ErrLocalAccountConflict), strings.Contains(err.Error(), "local-account-conflict"):
		code, message, status = "local-account-conflict", "The account changed; preview again before applying", http.StatusConflict
	case errors.Is(err, platform.ErrLocalAccountNotFound), strings.Contains(err.Error(), "local-account-not-found"):
		code, message, status = "local-account-not-found", "The local account no longer exists", http.StatusConflict
	case errors.Is(err, platform.ErrLocalAccountReadOnly), strings.Contains(err.Error(), "local-account-read-only"):
		code, message, status = "local-account-read-only", "Remote NSS identities are read-only in Tako", http.StatusForbidden
	case errors.Is(err, platform.ErrLocalAccountProtected), strings.Contains(err.Error(), "local-account-protected"):
		code, message, status = "local-account-protected", "The current operator account cannot be locked or deleted", http.StatusForbidden
	case errors.Is(err, platform.ErrLocalAccountVerification), strings.Contains(err.Error(), "local-account-verification-failed"):
		code, message, status = "local-account-verification-failed", "The shadow-utils operation could not be verified", http.StatusBadGateway
	case strings.Contains(err.Error(), "invalid-admin-token"):
		code, message, status = "administrative-access-required", "Administrative access expired", http.StatusForbidden
	case errors.Is(err, auth.ErrServiceUnavailable), strings.Contains(err.Error(), "local-account-unavailable"):
		code, message, status = "local-account-unavailable", "Privileged local account service is unavailable", http.StatusServiceUnavailable
	}
	problem(writer, status, code, message)
}
