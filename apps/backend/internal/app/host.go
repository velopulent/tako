package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
	"go.uber.org/zap"
)

func (server *Server) dashboard(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	credentials := sessionCredentials(current)
	payload := map[string]any{"host": server.hostInfo(request.Context(), credentials), "metrics": metrics.Sample{}}
	var samples []metrics.Sample
	if err := server.broker.Call(request.Context(), "metrics.history", credentials, auth.MetricsHistoryOperation{Since: time.Now().Add(-time.Hour), Maximum: 1}, &samples); err == nil && len(samples) > 0 {
		payload["metrics"] = samples[len(samples)-1]
	}
	var snapshot platform.StorageSnapshot
	if err := server.broker.Call(request.Context(), "storage.read", credentials, nil, &snapshot); err == nil {
		payload["storage"] = platform.SummarizeStorage(snapshot.Filesystems)
	}
	writeJSON(writer, http.StatusOK, payload)
}

// hostInfo caches the host identity briefly; it is the same for every caller.
func (server *Server) hostInfo(ctx context.Context, credentials auth.Credentials) host.Info {
	server.hostMu.Lock()
	defer server.hostMu.Unlock()
	if !server.hostSnapshotAt.IsZero() && time.Since(server.hostSnapshotAt) < 15*time.Second {
		return server.hostSnapshot
	}
	var info host.Info
	if err := server.broker.Call(ctx, "host.info", credentials, nil, &info); err == nil {
		server.hostSnapshot = info
		server.hostSnapshotAt = time.Now()
	}
	return server.hostSnapshot
}

func (server *Server) capabilities(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	var capabilities []platform.Capability
	if err := server.broker.Call(request.Context(), "capabilities.read", sessionCredentials(current), nil, &capabilities); err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	if current.Identity.BridgeToken != "" {
		// The terminal and file browser run in the user's own bridge, which
		// sessiond's root probe cannot observe.
		for index := range capabilities {
			capability := &capabilities[index]
			switch capability.ID {
			case "terminal":
				capability.State, capability.Readable, capability.Mutable = platform.StateReady, true, true
				capability.ReadAuthority, capability.MutationAuthority, capability.Contract = "user", "user", "user-bridge"
				capability.Reason, capability.SetupGuidance = "", ""
			case "files":
				capability.State, capability.Readable, capability.Mutable = platform.StateReady, true, true
				capability.ReadAuthority, capability.MutationAuthority, capability.Contract = "user", "administrative", "user-bridge+sessiond"
				capability.Reason, capability.SetupGuidance = "", ""
			}
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"capabilities": capabilities})
}

var metricRanges = map[string]time.Duration{"15m": 15 * time.Minute, "1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour}

func (server *Server) metricHistory(writer http.ResponseWriter, request *http.Request) {
	duration, ok := metricRanges[request.URL.Query().Get("range")]
	if !ok {
		duration = time.Hour
	}
	var samples []metrics.Sample
	operation := auth.MetricsHistoryOperation{Since: time.Now().Add(-duration), Maximum: 1000}
	if err := server.broker.Call(request.Context(), "metrics.history", sessionCredentials(currentSession(request)), operation, &samples); err != nil {
		fail(writer, err, "system-backend-unavailable")
		return
	}
	if samples == nil {
		samples = []metrics.Sample{}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"samples": samples})
}

var metricIntervals = map[string]time.Duration{"1s": time.Second, "5s": 5 * time.Second, "15s": 15 * time.Second, "30s": 30 * time.Second, "1m": time.Minute, "5m": 5 * time.Minute}

func (server *Server) metricStream(writer http.ResponseWriter, request *http.Request) {
	requested := request.URL.Query().Get("interval")
	interval := metricIntervals[requested]
	if requested != "" && interval == 0 {
		problem(writer, http.StatusBadRequest, "invalid-metric-interval", "Unsupported metric interval")
		return
	}
	if interval == 0 {
		interval = server.config.MonitoringInterval
	}
	credentials := sessionCredentials(currentSession(request))
	serveEvents(writer, request, func(ctx context.Context, send func(string, any) error) error {
		return streamAs(ctx, server.broker, "metrics.follow", credentials, auth.MetricsFollowOperation{Interval: interval}, func(sample metrics.Sample) error {
			return send("metric", sample)
		})
	})
}

// serveEvents runs produce and relays its events as Server-Sent Events with a
// heartbeat. Streams end after 15 minutes; EventSource reconnects on its own.
func serveEvents(writer http.ResponseWriter, request *http.Request, produce func(context.Context, func(string, any) error) error) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		problem(writer, http.StatusInternalServerError, "stream-unsupported", "Streaming is unavailable")
		return
	}
	header := writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	fmt.Fprint(writer, ": connected\n\n")
	flusher.Flush()

	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Minute)
	defer cancel()
	type event struct {
		name    string
		payload []byte
	}
	events := make(chan event, 64)
	go func() {
		defer close(events)
		_ = produce(ctx, func(name string, value any) error {
			payload, err := json.Marshal(value)
			if err != nil {
				return err
			}
			select {
			case events <- event{name, payload}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case item, open := <-events:
			if !open {
				return
			}
			if _, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", item.name, item.payload); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (server *Server) hostConfiguration(writer http.ResponseWriter, request *http.Request) {
	var configuration platform.HostConfiguration
	if err := server.broker.Call(request.Context(), "host.configuration", sessionCredentials(currentSession(request)), nil, &configuration); err != nil {
		fail(writer, err, "host-config-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, configuration)
}

func decodeHostConfiguration(writer http.ResponseWriter, request *http.Request) (auth.HostConfigurationOperation, bool) {
	payload, ok := decodeBody[auth.HostConfigurationOperation](writer, request, 8<<10, "invalid-host-configuration")
	if !ok {
		return payload, false
	}
	if payload.Hostname == "" || len(payload.Hostname) > 253 || payload.Timezone == "" || len(payload.Timezone) > 128 || len(payload.ExpectedFingerprint) != 64 {
		reject(writer, "invalid-host-configuration")
		return payload, false
	}
	return payload, true
}

func (server *Server) updateHostConfiguration(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	payload, ok := decodeHostConfiguration(writer, request)
	if !ok {
		return
	}
	credentials := adminCredentials(current)
	if err := server.broker.Call(request.Context(), "host.configure", credentials, payload, nil); err != nil {
		server.audit(request, "host/config", err, true)
		fail(writer, err, "host-config-failed")
		return
	}
	var updated platform.HostConfiguration
	err := server.broker.Call(request.Context(), "host.configuration", credentials, nil, &updated)
	if err != nil || updated.Hostname != payload.Hostname || updated.Timezone != payload.Timezone || updated.NTPEnabled != payload.NTPEnabled {
		server.audit(request, "host/config", fmt.Errorf("verification failed"), true)
		problem(writer, http.StatusBadGateway, "host-config-verification-failed", "The host did not report the requested configuration")
		return
	}
	server.hostMu.Lock()
	server.hostSnapshotAt = time.Time{}
	server.hostMu.Unlock()
	server.audit(request, "host/config", nil, true)
	writeJSON(writer, http.StatusOK, updated)
}

func (server *Server) powerStatus(writer http.ResponseWriter, request *http.Request) {
	var status platform.PowerStatus
	if err := server.broker.Call(request.Context(), "power.status", sessionCredentials(currentSession(request)), nil, &status); err != nil {
		fail(writer, err, "power-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (server *Server) requestPower(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	payload, ok := decodeBody[auth.PowerOperation](writer, request, 8<<10, "invalid-request")
	if !ok {
		return
	}
	err := server.broker.Call(request.Context(), "power.request", adminCredentials(current), payload, nil)
	server.audit(request, "host/"+payload.Action, err, true)
	if err != nil {
		fail(writer, err, "power-request-failed")
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]any{"action": payload.Action, "message": "Power request accepted; the host may disconnect shortly."})
}

func (server *Server) terminalStatus(writer http.ResponseWriter, request *http.Request) {
	available := currentSession(request).Identity.BridgeToken != ""
	message := "Terminal is ready."
	if !available {
		message = "The terminal needs a full PAM login session."
	}
	writeJSON(writer, http.StatusOK, map[string]any{"available": available, "message": message})
}

func (server *Server) terminalWebSocket(writer http.ResponseWriter, request *http.Request) {
	if !server.originAllowed(request) {
		problem(writer, http.StatusForbidden, "origin-not-allowed", "Request origin is not allowed")
		return
	}
	current := currentSession(request)
	if current.Identity.BridgeToken == "" {
		problem(writer, http.StatusForbidden, "user-bridge-unavailable", "The terminal needs a full PAM login session")
		return
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		server.logger.Warn("WebSocket upgrade failed", zap.Error(err))
		return
	}
	defer connection.CloseNow()
	connection.SetReadLimit(64 << 10)
	ctx, cancel := context.WithDeadline(request.Context(), current.CreatedAt.Add(sessionAbsoluteTimeout))
	defer cancel()
	size := terminalSize(request)
	terminal, err := server.broker.Open(ctx, "terminal.open", userCredentials(current), size)
	if err != nil {
		server.logger.Error("terminal launch failed", zap.String("username", current.Identity.Username), zap.Error(err))
		_ = connection.Close(websocket.StatusInternalError, "terminal launch failed")
		return
	}
	defer terminal.Close()
	server.logger.Info("terminal opened", zap.String("username", current.Identity.Username))
	go func() {
		defer cancel()
		buffer := make([]byte, 32<<10)
		for {
			count, readErr := terminal.Read(buffer)
			if count > 0 && connection.Write(ctx, websocket.MessageBinary, buffer[:count]) != nil {
				return
			}
			if readErr != nil {
				_ = connection.Close(websocket.StatusNormalClosure, "terminal closed")
				return
			}
		}
	}()
	for {
		kind, payload, readErr := connection.Read(ctx)
		if readErr != nil {
			return
		}
		if kind == websocket.MessageBinary {
			if _, err := terminal.Write(payload); err != nil {
				return
			}
			continue
		}
		var resize auth.TerminalSize
		if json.Unmarshal(payload, &resize) == nil && resize.Columns > 0 && resize.Rows > 0 {
			_ = server.broker.Call(ctx, "terminal.resize", userCredentials(current), resize, nil)
		}
	}
}

func terminalSize(request *http.Request) auth.TerminalSize {
	var size auth.TerminalSize
	_, _ = fmt.Sscan(request.URL.Query().Get("columns"), &size.Columns)
	_, _ = fmt.Sscan(request.URL.Query().Get("rows"), &size.Rows)
	return size
}

// audit records the outcome of a state-changing operation in the gateway log.
func (server *Server) audit(request *http.Request, target string, err error, administrative bool) {
	current, _ := request.Context().Value(sessionKey{}).(session.Session)
	result := "succeeded"
	code := ""
	if err != nil {
		result, code = "failed", auth.ErrorCode(err, "failed")
	}
	server.logger.Info("operation completed",
		zap.String("actor", current.Identity.Username),
		zap.String("target", target),
		zap.String("result", result),
		zap.String("error_code", code),
		zap.Bool("administrative", administrative),
		zap.String("remote_ip", request.RemoteAddr),
	)
}
