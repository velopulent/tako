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

func (server *Server) sshKeys(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	target := request.URL.Query().Get("username")
	if target == "" {
		target = current.Identity.Username
	}
	operation := platform.SSHKeyOperation{Action: "list", Username: target, Preview: true}
	if err := platform.ValidateSSHKeyOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-ssh-key-operation", "The SSH key operation is invalid")
		return
	}
	keyRequest, status, code, message, ok := sshKeyRequestFor(current, operation)
	if !ok {
		problem(writer, status, code, message)
		return
	}
	if server.previewSSHKeysFn == nil {
		problem(writer, http.StatusServiceUnavailable, "ssh-key-unavailable", "The SSH key service is unavailable")
		return
	}
	preview, err := server.previewSSHKeysFn(request.Context(), keyRequest)
	if err != nil {
		writeSSHKeyError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview.Current)
}

func (server *Server) previewSSHKeys(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	operation, ok := decodeSSHKeyOperation(writer, request, true)
	if !ok {
		return
	}
	keyRequest, status, code, message, authorized := sshKeyRequestFor(current, operation)
	if !authorized {
		problem(writer, status, code, message)
		return
	}
	if server.previewSSHKeysFn == nil {
		problem(writer, http.StatusServiceUnavailable, "ssh-key-unavailable", "The SSH key service is unavailable")
		return
	}
	preview, err := server.previewSSHKeysFn(request.Context(), keyRequest)
	if err != nil {
		writeSSHKeyError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) applySSHKeys(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	operation, ok := decodeSSHKeyOperation(writer, request, false)
	if !ok {
		return
	}
	keyRequest, status, code, message, authorized := sshKeyRequestFor(current, operation)
	if !authorized {
		problem(writer, status, code, message)
		return
	}
	if server.applySSHKeysFn == nil {
		problem(writer, http.StatusServiceUnavailable, "ssh-key-unavailable", "The SSH key service is unavailable")
		return
	}
	startedAt := time.Now().UTC()
	state, err := server.applySSHKeysFn(request.Context(), keyRequest)
	target := "user/" + operation.Username + "/ssh-keys/" + operation.Action
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "SSH key operation failed", keyRequest.Administrative)
		writeSSHKeyError(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", keyRequest.Administrative)
	writeJSON(writer, http.StatusOK, state)
}

func decodeSSHKeyOperation(writer http.ResponseWriter, request *http.Request, preview bool) (platform.SSHKeyOperation, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 32<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.SSHKeyOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-ssh-key-operation", "The SSH key operation is invalid")
		return platform.SSHKeyOperation{}, false
	}
	operation.Preview = preview
	if err := platform.ValidateSSHKeyOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-ssh-key-operation", "The SSH key operation is invalid")
		return platform.SSHKeyOperation{}, false
	}
	return operation, true
}

func sshKeyRequestFor(current session.Session, operation platform.SSHKeyOperation) (auth.SSHKeyRequest, int, string, string, bool) {
	if operation.Username == current.Identity.Username {
		if current.Identity.BridgeToken != "" {
			return auth.SSHKeyRequest{Token: current.Identity.BridgeToken, Operation: operation}, 0, "", "", true
		}
		if hasAdministrativeAccess(current) {
			return auth.SSHKeyRequest{AdminToken: current.Identity.AdminToken, Administrative: true, Operation: operation}, 0, "", "", true
		}
		return auth.SSHKeyRequest{}, http.StatusForbidden, "user-bridge-unavailable", "Your authenticated user session is unavailable", false
	}
	if !hasAdministrativeAccess(current) {
		return auth.SSHKeyRequest{}, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before managing another user's SSH keys", false
	}
	return auth.SSHKeyRequest{AdminToken: current.Identity.AdminToken, Administrative: true, Operation: operation}, 0, "", "", true
}

func writeSSHKeyError(writer http.ResponseWriter, err error) {
	code := "ssh-key-unavailable"
	message := "The SSH key service is unavailable"
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, platform.ErrInvalidSSHKeyOperation), strings.Contains(err.Error(), "invalid-ssh-key-operation"):
		code, message, status = "invalid-ssh-key-operation", "The SSH key operation is invalid", http.StatusBadRequest
	case errors.Is(err, platform.ErrSSHKeyConflict), strings.Contains(err.Error(), "ssh-key-conflict"):
		code, message, status = "ssh-key-conflict", "Authorized keys changed; refresh before applying", http.StatusConflict
	case errors.Is(err, platform.ErrSSHKeyNotFound), strings.Contains(err.Error(), "ssh-key-not-found"):
		code, message, status = "ssh-key-not-found", "The selected authorized key no longer exists", http.StatusConflict
	case errors.Is(err, platform.ErrSSHKeyReadOnly), strings.Contains(err.Error(), "ssh-key-read-only"):
		code, message, status = "ssh-key-read-only", "Remote NSS identities are read-only in Tako", http.StatusForbidden
	case errors.Is(err, platform.ErrSSHKeyUnauthorized), strings.Contains(err.Error(), "ssh-key-unauthorized"):
		code, message, status = "ssh-key-unauthorized", "You are not authorized to manage this user's SSH keys", http.StatusForbidden
	case errors.Is(err, platform.ErrSSHKeyProtected), strings.Contains(err.Error(), "ssh-key-protected"):
		code, message, status = "ssh-key-protected", "The authorized_keys path is protected and was not changed", http.StatusForbidden
	case errors.Is(err, platform.ErrSSHKeyVerification), strings.Contains(err.Error(), "ssh-key-verification"):
		code, message, status = "ssh-key-verification-failed", "The authorized_keys change could not be verified", http.StatusBadGateway
	case errors.Is(err, auth.ErrServiceUnavailable), errors.Is(err, platform.ErrSSHKeyUnavailable), strings.Contains(err.Error(), "ssh-key-unavailable"):
		code, message, status = "ssh-key-unavailable", "The SSH key service is unavailable", http.StatusServiceUnavailable
	case strings.Contains(err.Error(), "invalid-bridge-token"):
		code, message, status = "session-expired", "Your authenticated session has expired", http.StatusUnauthorized
	case strings.Contains(err.Error(), "invalid-admin-token"):
		code, message, status = "administrative-access-required", "Administrative access expired", http.StatusForbidden
	}
	problem(writer, status, code, message)
}
