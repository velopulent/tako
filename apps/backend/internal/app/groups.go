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

func (server *Server) previewGroupMembership(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before changing group membership")
		return
	}
	operation, ok := decodeGroupMembershipOperation(writer, request, true)
	if !ok {
		return
	}
	if server.previewGroupFn == nil {
		problem(writer, http.StatusServiceUnavailable, "group-unavailable", "Privileged group service is unavailable")
		return
	}
	preview, err := server.previewGroupFn(request.Context(), auth.GroupMembershipRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	if err != nil {
		writeGroupError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) applyGroupMembership(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before changing group membership")
		return
	}
	operation, ok := decodeGroupMembershipOperation(writer, request, false)
	if !ok {
		return
	}
	if server.applyGroupFn == nil {
		problem(writer, http.StatusServiceUnavailable, "group-unavailable", "Privileged group service is unavailable")
		return
	}
	startedAt := time.Now().UTC()
	state, err := server.applyGroupFn(request.Context(), auth.GroupMembershipRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	target := "group/" + operation.Group + "/" + operation.Username + "/" + operation.Action
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "group membership operation failed", true)
		writeGroupError(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", true)
	writeJSON(writer, http.StatusOK, state)
}

func decodeGroupMembershipOperation(writer http.ResponseWriter, request *http.Request, preview bool) (platform.GroupMembershipOperation, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.GroupMembershipOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-group-operation", "The group membership operation is invalid")
		return platform.GroupMembershipOperation{}, false
	}
	operation.Preview = preview
	if err := platform.ValidateGroupMembershipOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-group-operation", "The group membership operation is invalid")
		return platform.GroupMembershipOperation{}, false
	}
	return operation, true
}

func (server *Server) previewAdministrativeRole(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before changing administrator membership")
		return
	}
	operation, ok := decodeAdministrativeRoleOperation(writer, request, true)
	if !ok {
		return
	}
	if server.previewAdminRoleFn == nil {
		problem(writer, http.StatusServiceUnavailable, "administrative-role-unavailable", "Administrator role service is unavailable")
		return
	}
	preview, err := server.previewAdminRoleFn(request.Context(), auth.AdministrativeRoleRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	if err != nil {
		writeGroupError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) applyAdministrativeRole(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access before changing administrator membership")
		return
	}
	operation, ok := decodeAdministrativeRoleOperation(writer, request, false)
	if !ok {
		return
	}
	if server.applyAdminRoleFn == nil {
		problem(writer, http.StatusServiceUnavailable, "administrative-role-unavailable", "Administrator role service is unavailable")
		return
	}
	startedAt := time.Now().UTC()
	state, err := server.applyAdminRoleFn(request.Context(), auth.AdministrativeRoleRequest{AdminToken: current.Identity.AdminToken, Operation: operation})
	target := "admin-role/" + operation.Username + "/" + operation.Action
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "administrator role operation failed", true)
		writeGroupError(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", true)
	writeJSON(writer, http.StatusOK, state)
}

func decodeAdministrativeRoleOperation(writer http.ResponseWriter, request *http.Request, preview bool) (platform.AdministrativeRoleOperation, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.AdministrativeRoleOperation
	if err := decoder.Decode(&operation); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-admin-role-operation", "The administrator role operation is invalid")
		return platform.AdministrativeRoleOperation{}, false
	}
	operation.Preview = preview
	if err := platform.ValidateAdministrativeRoleOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-admin-role-operation", "The administrator role operation is invalid")
		return platform.AdministrativeRoleOperation{}, false
	}
	return operation, true
}

func writeGroupError(writer http.ResponseWriter, err error) {
	code := "group-unavailable"
	message := "Group management is unavailable"
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, platform.ErrInvalidGroupOperation), strings.Contains(err.Error(), "invalid-group"):
		code, message, status = "invalid-group-operation", "The group operation is invalid", http.StatusBadRequest
	case errors.Is(err, platform.ErrGroupConflict), strings.Contains(err.Error(), "group-conflict"):
		code, message, status = "group-conflict", "Group membership changed; preview again before applying", http.StatusConflict
	case errors.Is(err, platform.ErrGroupNotFound), strings.Contains(err.Error(), "group-not-found"):
		code, message, status = "group-not-found", "The local group no longer exists", http.StatusConflict
	case errors.Is(err, platform.ErrGroupUserNotFound), strings.Contains(err.Error(), "group-user-not-found"):
		code, message, status = "group-user-not-found", "The local user no longer exists", http.StatusConflict
	case errors.Is(err, platform.ErrGroupReadOnly), strings.Contains(err.Error(), "group-read-only"):
		code, message, status = "group-read-only", "Remote identities and groups are read-only in Tako", http.StatusForbidden
	case errors.Is(err, platform.ErrGroupVerification), strings.Contains(err.Error(), "group-verification"):
		code, message, status = "group-verification-failed", "The group operation could not be verified", http.StatusBadGateway
	case errors.Is(err, platform.ErrAdministrativeRoleUnavailable), strings.Contains(err.Error(), "administrative-role-unavailable"):
		code, message, status = "administrative-role-unavailable", "No supported local sudo or wheel group is available", http.StatusServiceUnavailable
	case errors.Is(err, platform.ErrAdministrativeRoleProtected), strings.Contains(err.Error(), "administrative-role-protected"):
		code, message, status = "administrative-role-protected", "The current or last administrator cannot be removed", http.StatusForbidden
	case errors.Is(err, auth.ErrServiceUnavailable), strings.Contains(err.Error(), "group-unavailable"):
		code, message, status = "group-unavailable", "Privileged group service is unavailable", http.StatusServiceUnavailable
	}
	problem(writer, status, code, message)
}
