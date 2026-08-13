package app

import (
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

type hostConfigurationPayload struct {
	Hostname            string `json:"hostname"`
	Timezone            string `json:"timezone"`
	NTPEnabled          bool   `json:"ntpEnabled"`
	ExpectedFingerprint string `json:"expectedFingerprint"`
}

func (server *Server) hostConfiguration(writer http.ResponseWriter, request *http.Request) {
	configuration, err := server.readHostConfiguration(request.Context())
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "host-config-unavailable", "Hostname and time configuration is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, configuration)
}

func (server *Server) previewHostConfiguration(writer http.ResponseWriter, request *http.Request) {
	payload, ok := decodeHostConfigurationPayload(writer, request)
	if !ok {
		return
	}
	current, err := server.readHostConfiguration(request.Context())
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "host-config-unavailable", "Hostname and time configuration is unavailable")
		return
	}
	proposed := platform.NewHostConfiguration(payload.Hostname, payload.Timezone, payload.NTPEnabled)
	changes := make([]string, 0, 3)
	if current.Hostname != proposed.Hostname {
		changes = append(changes, "hostname")
	}
	if current.Timezone != proposed.Timezone {
		changes = append(changes, "timezone")
	}
	if current.NTPEnabled != proposed.NTPEnabled {
		changes = append(changes, "ntp")
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"current":  current,
		"proposed": proposed,
		"changes":  changes,
		"stale":    current.Fingerprint != payload.ExpectedFingerprint,
	})
}

func (server *Server) updateHostConfiguration(writer http.ResponseWriter, request *http.Request) {
	currentSession := request.Context().Value(sessionKey{}).(session.Session)
	if currentSession.Identity.AdminToken == "" || !time.Now().Before(currentSession.AdminUntil) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Gain Administrative access first")
		return
	}
	payload, ok := decodeHostConfigurationPayload(writer, request)
	if !ok {
		return
	}
	current, err := server.readHostConfiguration(request.Context())
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "host-config-unavailable", "Hostname and time configuration is unavailable")
		return
	}
	if current.Fingerprint != payload.ExpectedFingerprint {
		problem(writer, http.StatusConflict, "host-config-conflict", "Host configuration changed; refresh and try again")
		return
	}
	startedAt := time.Now().UTC()
	err = auth.ApplyHostConfiguration(request.Context(), server.config.SessionSocket, auth.HostConfigurationRequest{
		AdminToken:          currentSession.Identity.AdminToken,
		Hostname:            payload.Hostname,
		Timezone:            payload.Timezone,
		NTPEnabled:          payload.NTPEnabled,
		ExpectedFingerprint: payload.ExpectedFingerprint,
	})
	if err != nil {
		server.recordOperation(request.Context(), currentSession.Identity.Username, "host/config", startedAt, "failed", "host configuration failed", true)
		switch {
		case strings.Contains(err.Error(), "host-config-conflict"):
			problem(writer, http.StatusConflict, "host-config-conflict", "Host configuration changed; refresh and try again")
		case strings.Contains(err.Error(), "invalid-admin-token"):
			problem(writer, http.StatusForbidden, "administrative-access-required", "Administrative access expired")
		case errors.Is(err, auth.ErrServiceUnavailable) || strings.Contains(err.Error(), "host-config-unavailable"):
			problem(writer, http.StatusServiceUnavailable, "host-config-unavailable", "Privileged host configuration service is unavailable")
		default:
			problem(writer, http.StatusBadGateway, "host-config-failed", "Host configuration could not be applied")
		}
		return
	}
	updated, err := server.readHostConfiguration(request.Context())
	if err != nil {
		server.recordOperation(request.Context(), currentSession.Identity.Username, "host/config", startedAt, "failed", "host configuration verification failed", true)
		problem(writer, http.StatusBadGateway, "host-config-verification-failed", "Host configuration changed but could not be verified")
		return
	}
	if updated.Hostname != payload.Hostname || updated.Timezone != payload.Timezone || updated.NTPEnabled != payload.NTPEnabled {
		server.recordOperation(request.Context(), currentSession.Identity.Username, "host/config", startedAt, "failed", "host configuration verification failed", true)
		problem(writer, http.StatusBadGateway, "host-config-verification-failed", "Host configuration did not match the requested state")
		return
	}
	server.recordOperation(request.Context(), currentSession.Identity.Username, "host/config", startedAt, "succeeded", "", true)
	writeJSON(writer, http.StatusOK, updated)
}

func decodeHostConfigurationPayload(writer http.ResponseWriter, request *http.Request) (hostConfigurationPayload, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 8<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload hostConfigurationPayload
	if err := decoder.Decode(&payload); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-host-config", "Hostname, timezone, NTP state, and current fingerprint are required")
		return hostConfigurationPayload{}, false
	}
	if payload.Hostname == "" || len(payload.Hostname) > 253 || payload.Timezone == "" || len(payload.Timezone) > 128 || len(payload.ExpectedFingerprint) != 64 {
		problem(writer, http.StatusBadRequest, "invalid-host-config", "Hostname, timezone, NTP state, and current fingerprint are required")
		return hostConfigurationPayload{}, false
	}
	if _, err := hex.DecodeString(payload.ExpectedFingerprint); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-host-config", "Current fingerprint is invalid")
		return hostConfigurationPayload{}, false
	}
	return payload, true
}
