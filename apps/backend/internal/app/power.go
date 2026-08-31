package app

import (
	"context"
	"encoding/hex"
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

type powerPayload struct {
	Action              string `json:"action"`
	Confirmation        string `json:"confirmation"`
	ExpectedFingerprint string `json:"expectedFingerprint"`
}

func (server *Server) powerStatus(writer http.ResponseWriter, request *http.Request) {
	status, err := server.readPowerStatus(request.Context())
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "power-unavailable", "Host power management is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (server *Server) previewPower(writer http.ResponseWriter, request *http.Request) {
	payload, ok := decodePowerPayload(writer, request, false)
	if !ok {
		return
	}
	status, err := server.readPowerStatus(request.Context())
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "power-unavailable", "Host power management is unavailable")
		return
	}
	selected := status.Reboot
	if payload.Action == "shutdown" {
		selected = status.Shutdown
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"action":               payload.Action,
		"state":                selected.State,
		"available":            selected.Available,
		"reason":               selected.Reason,
		"inhibitors":           status.Inhibitors,
		"fingerprint":          status.Fingerprint,
		"requiresConfirmation": true,
	})
}

func (server *Server) requestPower(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	if current.Identity.AdminToken == "" || !time.Now().Before(current.AdminUntil) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access first")
		return
	}
	payload, ok := decodePowerPayload(writer, request, true)
	if !ok {
		return
	}
	status, err := server.readPowerStatus(request.Context())
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "power-unavailable", "Host power management is unavailable")
		return
	}
	if status.Fingerprint != payload.ExpectedFingerprint {
		problem(writer, http.StatusConflict, "power-conflict", "Power state changed; refresh and try again")
		return
	}
	selected := status.Reboot
	if payload.Action == "shutdown" {
		selected = status.Shutdown
	}
	if !selected.Available {
		code := "power-" + selected.State
		statusCode := http.StatusForbidden
		if selected.State == "unavailable" {
			statusCode = http.StatusServiceUnavailable
		} else if selected.State == "inhibited" {
			statusCode = http.StatusConflict
		}
		problem(writer, statusCode, code, selected.Reason)
		return
	}
	startedAt := time.Now().UTC()
	err = server.hostBroker().RequestPower(request.Context(), auth.PowerRequest{
		AdminToken:          current.Identity.AdminToken,
		Action:              payload.Action,
		Confirmation:        payload.Confirmation,
		ExpectedFingerprint: payload.ExpectedFingerprint,
	})
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, "host/"+payload.Action, startedAt, "failed", "power request failed", true)
		switch {
		case strings.Contains(err.Error(), "power-conflict"):
			problem(writer, http.StatusConflict, "power-conflict", "Power state changed; refresh and try again")
		case strings.Contains(err.Error(), "power-challenge"):
			problem(writer, http.StatusForbidden, "power-challenged", "The host requires an interactive authorization challenge")
		case strings.Contains(err.Error(), "power-inhibited"):
			problem(writer, http.StatusConflict, "power-inhibited", "An active inhibitor blocked the power request")
		case errors.Is(err, auth.ErrServiceUnavailable):
			problem(writer, http.StatusServiceUnavailable, "power-unavailable", "Privileged host power service is unavailable")
		default:
			problem(writer, http.StatusBadGateway, "power-request-failed", "Host power request failed")
		}
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, "host/"+payload.Action, startedAt, "succeeded", "", true)
	writeJSON(writer, http.StatusAccepted, map[string]any{"action": payload.Action, "message": "Power request accepted; the host may disconnect shortly."})
}

func decodePowerPayload(writer http.ResponseWriter, request *http.Request, requireConfirmation bool) (powerPayload, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 8<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload powerPayload
	if err := decoder.Decode(&payload); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-power-request", "Power action payload is invalid")
		return powerPayload{}, false
	}
	if payload.Action != "reboot" && payload.Action != "shutdown" {
		problem(writer, http.StatusBadRequest, "invalid-power-action", "Choose reboot or shutdown")
		return powerPayload{}, false
	}
	if requireConfirmation {
		want := strings.ToUpper(payload.Action)
		if payload.Confirmation != want {
			problem(writer, http.StatusBadRequest, "invalid-power-confirmation", "Type "+want+" to confirm this action")
			return powerPayload{}, false
		}
	}
	if len(payload.ExpectedFingerprint) != 64 {
		problem(writer, http.StatusBadRequest, "invalid-power-fingerprint", "Current power state fingerprint is required")
		return powerPayload{}, false
	}
	if _, err := hex.DecodeString(payload.ExpectedFingerprint); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-power-fingerprint", "Current power state fingerprint is invalid")
		return powerPayload{}, false
	}
	return payload, true
}

func (server *Server) readPowerStatus(ctx context.Context) (platform.PowerStatus, error) {
	if server.readPowerStatusFn == nil {
		return platform.PowerStatus{}, auth.ErrServiceUnavailable
	}
	return server.readPowerStatusFn(ctx)
}
