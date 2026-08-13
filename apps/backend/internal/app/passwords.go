package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/session"
)

func (server *Server) changePassword(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	operation, ok := decodePasswordOperation(writer, request)
	if !ok {
		return
	}
	defer operation.Clear()

	passwordRequest := auth.PasswordChangeRequest{Operation: operation}
	defer passwordRequest.Operation.Clear()
	if operation.Action == "change" {
		if current.Identity.BridgeToken == "" {
			problem(writer, http.StatusForbidden, "user-bridge-unavailable", "Your authenticated user session is unavailable")
			return
		}
		passwordRequest.Token = current.Identity.BridgeToken
	} else {
		if !hasAdministrativeAccess(current) {
			problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before resetting another password")
			return
		}
		passwordRequest.AdminToken = current.Identity.AdminToken
	}
	if server.changePasswordFn == nil {
		problem(writer, http.StatusServiceUnavailable, "password-unavailable", "The password service is unavailable")
		return
	}

	username := current.Identity.Username
	if operation.Action == "reset" {
		username = operation.Username
	}
	target := "user/" + username + "/password-" + operation.Action
	startedAt := time.Now().UTC()
	err := server.changePasswordFn(request.Context(), passwordRequest)
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "password operation failed", operation.Action == "reset")
		writePasswordError(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", operation.Action == "reset")
	writer.WriteHeader(http.StatusNoContent)
}

func decodePasswordOperation(writer http.ResponseWriter, request *http.Request) (auth.PasswordChangeOperation, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation auth.PasswordChangeOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-password-operation", "The password operation is invalid")
		return auth.PasswordChangeOperation{}, false
	}
	if err := auth.ValidatePasswordChangeOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-password-operation", "The password operation is invalid")
		return auth.PasswordChangeOperation{}, false
	}
	return operation, true
}

func writePasswordError(writer http.ResponseWriter, err error) {
	code := "password-unavailable"
	message := "The password service is unavailable"
	status := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, auth.ErrPasswordInvalid), strings.Contains(err.Error(), "invalid-password-operation"):
		code, message, status = "invalid-password-operation", "The password operation is invalid", http.StatusBadRequest
	case errors.Is(err, auth.ErrPasswordAuthenticationFailed), strings.Contains(err.Error(), "password-authentication-failed"):
		code, message, status = "password-authentication-failed", "The current password was not accepted", http.StatusForbidden
	case errors.Is(err, auth.ErrPasswordExpired), strings.Contains(err.Error(), "password-expired"):
		code, message, status = "password-expired", "The password has expired and must be changed through the host policy", http.StatusConflict
	case errors.Is(err, auth.ErrPasswordPolicy), strings.Contains(err.Error(), "password-policy-failed"):
		code, message, status = "password-policy-failed", "The new password was rejected by host policy", http.StatusUnprocessableEntity
	case strings.Contains(err.Error(), "invalid-bridge-token"):
		code, message, status = "session-expired", "Your authenticated session has expired", http.StatusUnauthorized
	case strings.Contains(err.Error(), "invalid-admin-token"):
		code, message, status = "administrative-access-required", "Administrative access expired", http.StatusForbidden
	case errors.Is(err, auth.ErrServiceUnavailable), errors.Is(err, auth.ErrPasswordUnavailable), strings.Contains(err.Error(), "password-unavailable"):
		code, message, status = "password-unavailable", "The password service is unavailable", http.StatusServiceUnavailable
	}
	problem(writer, status, code, message)
}
