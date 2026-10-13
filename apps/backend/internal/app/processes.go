package app

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

func (server *Server) processes(writer http.ResponseWriter, request *http.Request) {
	var items []platform.Process
	if err := server.broker.Call(request.Context(), "processes.list", sessionCredentials(currentSession(request)), nil, &items); err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	if items == nil {
		items = []platform.Process{}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

func processID(writer http.ResponseWriter, request *http.Request) (int, bool) {
	pid, err := strconv.Atoi(chi.URLParam(request, "pid"))
	if err != nil || pid < 1 {
		problem(writer, http.StatusBadRequest, "invalid-process", "Process ID is invalid")
		return 0, false
	}
	return pid, true
}

func (server *Server) processDetail(writer http.ResponseWriter, request *http.Request) {
	pid, ok := processID(writer, request)
	if !ok {
		return
	}
	operation := auth.ProcessReadOperation{PID: pid}
	if raw := request.URL.Query().Get("started"); raw != "" {
		started, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			problem(writer, http.StatusBadRequest, "invalid-process", "Process start identity is invalid")
			return
		}
		operation.Started = started
	}
	var details platform.ProcessDetails
	if err := server.broker.Call(request.Context(), "processes.detail", sessionCredentials(currentSession(request)), operation, &details); err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, details)
}

type processSignalPayload struct {
	Signal          string                   `json:"signal"`
	Tree            bool                     `json:"tree,omitempty"`
	Started         uint64                   `json:"started"`
	ExpectedTargets []platform.ProcessTarget `json:"expectedTargets,omitempty"`
}

// decodeSignal builds the signal operation for pid. Previews carry no
// expected targets; applies must name the exact targets the preview showed.
func decodeSignal(writer http.ResponseWriter, request *http.Request, action string) (platform.SignalOperation, bool) {
	pid, ok := processID(writer, request)
	if !ok {
		return platform.SignalOperation{}, false
	}
	payload, ok := decodeBody[processSignalPayload](writer, request, 16<<10, "invalid-signal-operation")
	if !ok {
		return platform.SignalOperation{}, false
	}
	operation := platform.SignalOperation{Action: action, Signal: platform.SignalName(payload.Signal), Target: platform.ProcessTarget{PID: pid, Started: payload.Started}, Tree: payload.Tree, ExpectedTargets: payload.ExpectedTargets}
	if platform.ValidateSignalOperation(operation) != nil || (action == "preview") != (len(operation.ExpectedTargets) == 0) {
		reject(writer, "invalid-signal-operation")
		return platform.SignalOperation{}, false
	}
	return operation, true
}

func (server *Server) processSignalPreview(writer http.ResponseWriter, request *http.Request) {
	operation, ok := decodeSignal(writer, request, "preview")
	if !ok {
		return
	}
	var preview platform.SignalPreview
	if err := server.broker.Call(request.Context(), "processes.signal-preview", sessionCredentials(currentSession(request)), operation, &preview); err != nil {
		fail(writer, err, "process-signal-failed")
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) processSignal(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	operation, ok := decodeSignal(writer, request, "apply")
	if !ok {
		return
	}
	var result platform.SignalResult
	err := server.broker.Call(request.Context(), "processes.signal", sessionCredentials(current), operation, &result)
	if err == nil && len(result.Failures) > 0 {
		err = fmt.Errorf("%d signal failures", len(result.Failures))
	}
	server.audit(request, fmt.Sprintf("process/%d/%s", operation.Target.PID, operation.Signal), err, hasAdministrativeAccess(current))
	if err != nil && len(result.Failures) == 0 {
		fail(writer, err, "process-signal-failed")
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
