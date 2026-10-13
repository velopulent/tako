package app

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func (server *Server) identities(writer http.ResponseWriter, request *http.Request) (platform.IdentityInventory, bool) {
	var inventory platform.IdentityInventory
	if err := server.broker.Call(request.Context(), "identities.list", sessionCredentials(currentSession(request)), nil, &inventory); err != nil {
		fail(writer, err, "system-backend-unavailable")
		return inventory, false
	}
	return inventory, true
}

func (server *Server) users(writer http.ResponseWriter, request *http.Request) {
	if inventory, ok := server.identities(writer, request); ok {
		if inventory.Users == nil {
			inventory.Users = []platform.User{}
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": inventory.Users})
	}
}

func (server *Server) groups(writer http.ResponseWriter, request *http.Request) {
	if inventory, ok := server.identities(writer, request); ok {
		if inventory.Groups == nil {
			inventory.Groups = []platform.IdentityGroup{}
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": inventory.Groups})
	}
}

// adminChange runs an administrative preview or apply. The operation type
// carries its own Preview flag, which the caller sets before validation.
func adminChange[T any](server *Server, writer http.ResponseWriter, request *http.Request, operation, invalidCode string, limit int64, prepare func(*T, bool) (string, error), preview bool) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	payload, ok := decodeBody[T](writer, request, limit, invalidCode)
	if !ok {
		return
	}
	target, err := prepare(&payload, preview)
	if err != nil {
		reject(writer, invalidCode)
		return
	}
	var result any
	err = server.broker.Call(request.Context(), operation, adminCredentials(current), payload, &result)
	if !preview {
		server.audit(request, target, err, true)
	}
	if err != nil {
		fail(writer, err, invalidCode)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func prepareLocalAccount(operation *platform.LocalAccountOperation, preview bool) (string, error) {
	operation.Preview = preview
	return "user/" + operation.Username + "/" + operation.Action, platform.ValidateLocalAccountOperation(*operation)
}

func (server *Server) previewLocalAccount(writer http.ResponseWriter, request *http.Request) {
	adminChange(server, writer, request, "accounts.local", "invalid-local-account-operation", 16<<10, prepareLocalAccount, true)
}

func (server *Server) applyLocalAccount(writer http.ResponseWriter, request *http.Request) {
	adminChange(server, writer, request, "accounts.local", "invalid-local-account-operation", 16<<10, prepareLocalAccount, false)
}

func prepareGroupMembership(operation *platform.GroupMembershipOperation, preview bool) (string, error) {
	operation.Preview = preview
	return "group/" + operation.Group + "/" + operation.Username + "/" + operation.Action, platform.ValidateGroupMembershipOperation(*operation)
}

func (server *Server) previewGroupMembership(writer http.ResponseWriter, request *http.Request) {
	adminChange(server, writer, request, "groups.membership", "invalid-group-operation", 16<<10, prepareGroupMembership, true)
}

func (server *Server) applyGroupMembership(writer http.ResponseWriter, request *http.Request) {
	adminChange(server, writer, request, "groups.membership", "invalid-group-operation", 16<<10, prepareGroupMembership, false)
}

func prepareAdministrativeRole(operation *platform.AdministrativeRoleOperation, preview bool) (string, error) {
	operation.Preview = preview
	return "admin-role/" + operation.Username + "/" + operation.Action, platform.ValidateAdministrativeRoleOperation(*operation)
}

func (server *Server) previewAdministrativeRole(writer http.ResponseWriter, request *http.Request) {
	adminChange(server, writer, request, "groups.admin-role", "invalid-group-operation", 16<<10, prepareAdministrativeRole, true)
}

func (server *Server) applyAdministrativeRole(writer http.ResponseWriter, request *http.Request) {
	adminChange(server, writer, request, "groups.admin-role", "invalid-group-operation", 16<<10, prepareAdministrativeRole, false)
}

func prepareLocalGroup(operation *platform.LocalGroupOperation, preview bool) (string, error) {
	operation.Preview = preview
	return "group/" + operation.Group + "/" + operation.Action, platform.ValidateLocalGroupOperation(*operation)
}

func (server *Server) previewLocalGroup(writer http.ResponseWriter, request *http.Request) {
	adminChange(server, writer, request, "groups.local", "invalid-local-group-operation", 16<<10, prepareLocalGroup, true)
}

func (server *Server) applyLocalGroup(writer http.ResponseWriter, request *http.Request) {
	adminChange(server, writer, request, "groups.local", "invalid-local-group-operation", 16<<10, prepareLocalGroup, false)
}

func (server *Server) changePassword(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	operation, ok := decodeBody[auth.PasswordChangeOperation](writer, request, 16<<10, "invalid-password-operation")
	defer operation.Clear()
	if !ok {
		return
	}
	if auth.ValidatePasswordChangeOperation(operation) != nil {
		reject(writer, "invalid-password-operation")
		return
	}
	credentials := userCredentials(current)
	target := "user/" + current.Identity.Username + "/password-change"
	if operation.Action == "reset" {
		if !requireAdmin(writer, current) {
			return
		}
		credentials = adminCredentials(current)
		target = "user/" + operation.Username + "/password-reset"
	} else if current.Identity.BridgeToken == "" {
		reject(writer, "user-bridge-unavailable")
		return
	}
	err := server.broker.Call(request.Context(), "passwords.change", credentials, &operation, nil)
	server.audit(request, target, err, operation.Action == "reset")
	if err != nil {
		fail(writer, err, "password-unavailable")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

// sshKeyCredentials uses the user's own bridge for their own keys and the
// administrative lane for anyone else's.
func sshKeyCredentials(writer http.ResponseWriter, current session.Session, username string) (auth.Credentials, bool) {
	if username == current.Identity.Username {
		if current.Identity.BridgeToken != "" {
			return userCredentials(current), true
		}
		if hasAdministrativeAccess(current) {
			return adminCredentials(current), true
		}
		reject(writer, "user-bridge-unavailable")
		return auth.Credentials{}, false
	}
	if !requireAdmin(writer, current) {
		return auth.Credentials{}, false
	}
	return adminCredentials(current), true
}

func (server *Server) sshKeys(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	operation := platform.SSHKeyOperation{Action: "list", Username: request.URL.Query().Get("username"), Preview: true}
	if operation.Username == "" {
		operation.Username = current.Identity.Username
	}
	if platform.ValidateSSHKeyOperation(operation) != nil {
		reject(writer, "invalid-ssh-key-operation")
		return
	}
	credentials, ok := sshKeyCredentials(writer, current, operation.Username)
	if !ok {
		return
	}
	var preview platform.SSHKeyPreview
	if err := server.broker.Call(request.Context(), "ssh-keys.apply", credentials, operation, &preview); err != nil {
		fail(writer, err, "ssh-key-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, preview.Current)
}

func (server *Server) changeSSHKeys(writer http.ResponseWriter, request *http.Request, preview bool) {
	current := currentSession(request)
	operation, ok := decodeBody[platform.SSHKeyOperation](writer, request, 32<<10, "invalid-ssh-key-operation")
	if !ok {
		return
	}
	operation.Preview = preview
	if platform.ValidateSSHKeyOperation(operation) != nil {
		reject(writer, "invalid-ssh-key-operation")
		return
	}
	credentials, ok := sshKeyCredentials(writer, current, operation.Username)
	if !ok {
		return
	}
	var result any
	err := server.broker.Call(request.Context(), "ssh-keys.apply", credentials, operation, &result)
	if !preview {
		server.audit(request, "user/"+operation.Username+"/ssh-keys/"+operation.Action, err, credentials.AdminToken != "")
	}
	if err != nil {
		fail(writer, err, "ssh-key-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) previewSSHKeys(writer http.ResponseWriter, request *http.Request) {
	server.changeSSHKeys(writer, request, true)
}

func (server *Server) applySSHKeys(writer http.ResponseWriter, request *http.Request) {
	server.changeSSHKeys(writer, request, false)
}

func (server *Server) loginHistory(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	username := chi.URLParam(request, "username")
	if username != current.Identity.Username && !requireAdmin(writer, current) {
		return
	}
	query, err := parseLoginHistoryQuery(request, username)
	if err != nil {
		reject(writer, "invalid-login-history-query")
		return
	}
	credentials := sessionCredentials(current)
	if username == current.Identity.Username && current.Identity.BridgeToken != "" {
		credentials = userCredentials(current)
	}
	var page platform.LoginHistoryPage
	if err := server.broker.Call(request.Context(), "login-history.read", credentials, query, &page); err != nil {
		fail(writer, err, "login-history-unavailable")
		return
	}
	if page.Items == nil {
		page.Items = []platform.LoginHistoryEntry{}
	}
	writeJSON(writer, http.StatusOK, page)
}

func parseLoginHistoryQuery(request *http.Request, username string) (platform.LoginHistoryQuery, error) {
	values := request.URL.Query()
	query := platform.LoginHistoryQuery{Username: username, Limit: 50, Outcome: values.Get("outcome")}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return query, err
		}
		query.Limit = limit
	}
	if raw := values.Get("cursor"); raw != "" {
		cursor, err := platform.DecodeJournalCursor(raw)
		if err != nil {
			return query, err
		}
		query.Cursor = cursor
	}
	for key, destination := range map[string]*time.Time{"since": &query.Since, "until": &query.Until} {
		if raw := values.Get(key); raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return query, err
			}
			*destination = parsed
		}
	}
	return query, query.Validate()
}
