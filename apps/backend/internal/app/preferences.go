package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/velopulent/tako/internal/preferences"
	"go.uber.org/zap"
)

var monitoringIntervals = map[string]bool{
	"off": true,
	"1s":  true,
	"5s":  true,
	"15s": true,
	"30s": true,
	"1m":  true,
	"5m":  true,
}

func (server *Server) monitoringPreference(writer http.ResponseWriter, request *http.Request) {
	value, err := server.preferences.MonitoringInterval(request.Context(), monitoringIntervalName(server.config.MonitoringInterval))
	if err != nil {
		server.logger.Error("monitoring preference unavailable", zap.Error(err))
		problem(writer, http.StatusInternalServerError, "preference-unavailable", "Could not load monitoring preference")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"defaultInterval": value.DefaultInterval, "revision": value.Revision})
}

func monitoringIntervalName(interval time.Duration) string {
	for name := range monitoringIntervals {
		if parsed, err := time.ParseDuration(name); err == nil && parsed == interval {
			return name
		}
	}
	return "1m"
}

func (server *Server) updateMonitoringPreference(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, 4<<10)
	var body struct {
		DefaultInterval  string `json:"defaultInterval"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			problem(writer, http.StatusRequestEntityTooLarge, "monitoring-preference-too-large", "Request body is too large")
			return
		}
		problem(writer, http.StatusBadRequest, "invalid-monitoring-preference", "Default interval is invalid")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF || !monitoringIntervals[body.DefaultInterval] || body.ExpectedRevision < 0 {
		problem(writer, http.StatusBadRequest, "invalid-monitoring-preference", "Default interval is invalid")
		return
	}
	updated, err := server.preferences.SetMonitoringInterval(request.Context(), body.DefaultInterval, body.ExpectedRevision)
	if errors.Is(err, preferences.ErrConflict) {
		problem(writer, http.StatusConflict, "preference-conflict", "Monitoring preference changed; refresh and try again")
		return
	}
	if err != nil {
		server.logger.Error("monitoring preference update failed", zap.Error(err))
		problem(writer, http.StatusInternalServerError, "preference-update-failed", "Could not save monitoring preference")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"defaultInterval": updated.DefaultInterval, "revision": updated.Revision})
}
