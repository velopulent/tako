package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

func (server *Server) services(writer http.ResponseWriter, request *http.Request) {
	operation := auth.ServiceReadOperation{Scope: request.URL.Query().Get("scope"), Type: request.URL.Query().Get("type")}
	if operation.Scope == "" {
		operation.Scope = "system"
	}
	if operation.Type == "" {
		operation.Type = "service"
	}
	if (operation.Scope != "system" && operation.Scope != "user") || !platform.ValidUnitType(operation.Type) {
		problem(writer, http.StatusBadRequest, "invalid-unit-filter", "Unsupported unit scope or type")
		return
	}
	var items []platform.Unit
	if err := server.broker.Call(request.Context(), "services.list", scopeCredentials(currentSession(request), operation.Scope), operation, &items); err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	if items == nil {
		items = []platform.Unit{}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

// serviceTarget validates the {scope}/{unit} path parameters.
func serviceTarget(writer http.ResponseWriter, request *http.Request) (auth.ServiceReadOperation, bool) {
	target := auth.ServiceReadOperation{Scope: chi.URLParam(request, "scope"), Unit: chi.URLParam(request, "unit")}
	if platform.ValidateServiceTarget(target.Scope, target.Unit) != nil {
		reject(writer, "invalid-service-operation")
		return target, false
	}
	return target, true
}

func (server *Server) readUnit(request *http.Request, target auth.ServiceReadOperation) (platform.UnitDetail, error) {
	var detail platform.UnitDetail
	err := server.broker.Call(request.Context(), "services.detail", scopeCredentials(currentSession(request), target.Scope), target, &detail)
	return detail, err
}

func (server *Server) serviceDetail(writer http.ResponseWriter, request *http.Request) {
	target, ok := serviceTarget(writer, request)
	if !ok {
		return
	}
	detail, err := server.readUnit(request, target)
	if err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, detail)
}

func (server *Server) serviceConfiguration(writer http.ResponseWriter, request *http.Request) {
	target, ok := serviceTarget(writer, request)
	if !ok {
		return
	}
	var configuration platform.UnitConfiguration
	if err := server.broker.Call(request.Context(), "services.configuration", scopeCredentials(currentSession(request), target.Scope), target, &configuration); err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, configuration)
}

// serviceAuthorized checks the session holds the lane the unit scope needs:
// system units need administrative access, user units the user's own session.
func serviceAuthorized(writer http.ResponseWriter, request *http.Request, scope string) bool {
	current := currentSession(request)
	if scope == "system" {
		return requireAdmin(writer, current)
	}
	if current.Identity.BridgeToken == "" {
		reject(writer, "user-bridge-unavailable")
		return false
	}
	return true
}

func decodeServiceAction(writer http.ResponseWriter, request *http.Request) (platform.ServiceOperation, bool) {
	body, ok := decodeBody[struct {
		Action string `json:"action"`
	}](writer, request, 4<<10, "invalid-service-operation")
	if !ok {
		return platform.ServiceOperation{}, false
	}
	operation, err := platform.ParseServiceOperation(chi.URLParam(request, "scope"), chi.URLParam(request, "unit"), body.Action)
	if err != nil {
		reject(writer, "invalid-service-operation")
		return platform.ServiceOperation{}, false
	}
	return operation, serviceAuthorized(writer, request, operation.Scope)
}

func (server *Server) serviceActionPreview(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeServiceAction(writer, request)
	if !ok {
		return
	}
	detail, err := server.readUnit(request, auth.ServiceReadOperation{Scope: operation.Scope, Unit: operation.Unit})
	if err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, platform.ServiceImpactForAction(detail, operation.Action))
}

func (server *Server) serviceAction(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeServiceAction(writer, request)
	if !ok {
		return
	}
	credentials := scopeCredentials(currentSession(request), operation.Scope)
	err := server.broker.Call(request.Context(), "services.action", credentials, operation, nil)
	server.audit(request, operation.Scope+"/"+operation.Unit+"/"+operation.Action, err, operation.Scope == "system")
	if err != nil {
		fail(writer, err, "service-action-failed")
		return
	}
	detail, err := server.readUnit(request, auth.ServiceReadOperation{Scope: operation.Scope, Unit: operation.Unit})
	if err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, detail)
}

func decodeOverride(writer http.ResponseWriter, request *http.Request) (platform.OverrideOperation, bool) {
	operation, ok := decodeBody[platform.OverrideOperation](writer, request, 32<<10, "invalid-service-override")
	if !ok {
		return operation, false
	}
	if operation.Scope != chi.URLParam(request, "scope") || operation.Unit != chi.URLParam(request, "unit") || platform.ValidateOverrideOperation(operation) != nil {
		reject(writer, "invalid-service-override")
		return operation, false
	}
	return operation, serviceAuthorized(writer, request, operation.Scope)
}

func (server *Server) serviceOverridePreview(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeOverride(writer, request)
	if !ok {
		return
	}
	operation.Action, operation.ExpectedFingerprint = "preview", ""
	var state platform.OverrideState
	if err := server.broker.Call(request.Context(), "overrides.apply", scopeCredentials(currentSession(request), operation.Scope), operation, &state); err != nil {
		fail(writer, err, "service-override-failed")
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (server *Server) serviceOverride(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeOverride(writer, request)
	if !ok {
		return
	}
	if operation.Action == "preview" {
		reject(writer, "invalid-service-override")
		return
	}
	var state platform.OverrideState
	err := server.broker.Call(request.Context(), "overrides.apply", scopeCredentials(currentSession(request), operation.Scope), operation, &state)
	server.audit(request, operation.Scope+"/"+operation.Unit+"/override/"+operation.Action, err, operation.Scope == "system")
	if err != nil {
		fail(writer, err, "service-override-failed")
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func decodeTimer(writer http.ResponseWriter, request *http.Request) (platform.TimerOperation, bool) {
	operation, ok := decodeBody[platform.TimerOperation](writer, request, 16<<10, "invalid-timer-operation")
	if !ok {
		return operation, false
	}
	if platform.ValidateTimerOperation(operation) != nil {
		reject(writer, "invalid-timer-operation")
		return operation, false
	}
	return operation, serviceAuthorized(writer, request, operation.Scope)
}

func (server *Server) timerPreview(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeTimer(writer, request)
	if !ok {
		return
	}
	operation.Action, operation.ExpectedFingerprint = "preview", ""
	var state platform.TimerState
	if err := server.broker.Call(request.Context(), "timers.apply", scopeCredentials(currentSession(request), operation.Scope), operation, &state); err != nil {
		fail(writer, err, "timer-operation-failed")
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (server *Server) timerAction(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeTimer(writer, request)
	if !ok {
		return
	}
	if operation.Action == "preview" {
		reject(writer, "invalid-timer-operation")
		return
	}
	var state platform.TimerState
	err := server.broker.Call(request.Context(), "timers.apply", scopeCredentials(currentSession(request), operation.Scope), operation, &state)
	server.audit(request, operation.Scope+"/"+operation.Name+"/"+operation.Action, err, operation.Scope == "system")
	if err != nil {
		fail(writer, err, "timer-operation-failed")
		return
	}
	writeJSON(writer, http.StatusOK, state)
}
