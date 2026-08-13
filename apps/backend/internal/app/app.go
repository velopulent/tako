package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/dashboard"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/preferences"
	"github.com/velopulent/tako/internal/session"
	"go.uber.org/zap"
)

type Server struct {
	config             config.Config
	http               *http.Server
	sessions           *session.Store
	authenticator      auth.Authenticator
	metrics            *metrics.Sampler
	preferences        *preferences.Store
	loginAttempts      *loginLimiter
	logger             *zap.Logger
	cancel             context.CancelFunc
	cleanupQueue       chan auth.Identity
	pruneDone          chan struct{}
	workersWG          sync.WaitGroup
	cleanupMu          sync.Mutex
	cleanupClosed      bool
	detectCapabilities func(context.Context) []platform.Capability
}

func New(cfg config.Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())
	authenticator := auth.Authenticator(auth.SocketAuthenticator{Path: cfg.SessionSocket})
	if cfg.Development {
		authenticator = auth.DevelopmentAuthenticator{}
	}
	capacity := int(cfg.HistoryRetention/time.Second) + 1
	sampler := metrics.NewSampler(capacity)
	sampler.Configure(cfg.MonitoringInterval, cfg.HistoryRetention)
	preferenceStore, err := preferences.Open(cfg.DataDir)
	if err != nil {
		cancel()
		return nil, err
	}
	sessions := session.NewStore(15*time.Minute, 12*time.Hour)
	go sampler.Run(ctx, cfg.MonitoringInterval)
	server := &Server{
		config:             cfg,
		sessions:           sessions,
		authenticator:      authenticator,
		metrics:            sampler,
		preferences:        preferenceStore,
		loginAttempts:      newLoginLimiter(5, time.Minute),
		logger:             zap.L().Named("gateway"),
		cancel:             cancel,
		cleanupQueue:       make(chan auth.Identity, 64),
		pruneDone:          make(chan struct{}),
		detectCapabilities: platform.Detect,
	}
	sessions.SetDeleteHook(server.enqueueUserSessionClose)
	_, sessionController := authenticator.(auth.SessionController)
	_, administrativeController := authenticator.(auth.AdministrativeController)
	if sessionController || administrativeController {
		for range 4 {
			server.workersWG.Add(1)
			go server.cleanupWorker()
		}
	}
	go func() {
		defer close(server.pruneDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sessions.Prune()
			case <-ctx.Done():
				return
			}
		}
	}()
	server.http = &http.Server{
		Addr:              cfg.Address,
		Handler:           server.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return server, nil
}

func (server *Server) closeUserSession(identity auth.Identity) {
	controller, ok := server.authenticator.(auth.SessionController)
	if ok && identity.BridgeToken != "" {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := controller.CloseUserSession(closeCtx, identity.BridgeToken); err != nil {
			server.logger.Warn("user session cleanup failed", zap.String("username", identity.Username), zap.Error(err))
		}
		closeCancel()
	}
	if controller, ok := server.authenticator.(auth.AdministrativeController); ok && identity.AdminToken != "" {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := controller.RevokeAdministrative(closeCtx, identity.AdminToken); err != nil {
			server.logger.Warn("administrative access cleanup failed", zap.String("username", identity.Username), zap.Error(err))
		}
		closeCancel()
	}
}

func (server *Server) enqueueUserSessionClose(identity auth.Identity) {
	_, sessionController := server.authenticator.(auth.SessionController)
	_, administrativeController := server.authenticator.(auth.AdministrativeController)
	if (!sessionController && !administrativeController) || (identity.BridgeToken == "" && identity.AdminToken == "") {
		return
	}
	server.cleanupMu.Lock()
	defer server.cleanupMu.Unlock()
	if server.cleanupClosed {
		return
	}
	select {
	case server.cleanupQueue <- identity:
	default:
		server.logger.Error("user session cleanup queue full; sessiond grant will expire automatically", zap.String("username", identity.Username))
	}
}

func (server *Server) cleanupWorker() {
	defer server.workersWG.Done()
	for identity := range server.cleanupQueue {
		server.closeUserSession(identity)
	}
}

func (server *Server) ListenAndServe() error {
	if !server.config.Development {
		info, statErr := os.Stat(server.config.SessionSocket)
		switch {
		case statErr != nil:
			server.logger.Warn("authentication service socket unavailable", zap.String("path", server.config.SessionSocket), zap.Error(statErr))
		case info.Mode()&os.ModeSocket == 0:
			server.logger.Warn("authentication service path is not a socket", zap.String("path", server.config.SessionSocket))
		default:
			server.logger.Info("authentication service socket found", zap.String("path", server.config.SessionSocket))
		}
	}
	listener, err := activatedListener()
	if err != nil {
		return err
	}
	if listener == nil {
		listener, err = net.Listen("tcp", server.config.Address)
		if err != nil {
			return err
		}
		server.logger.Info("listening", zap.String("network", "tcp"), zap.String("address", listener.Addr().String()))
	} else {
		server.logger.Info("using systemd listener", zap.String("address", listener.Addr().String()))
	}
	return server.Serve(listener)
}

func (server *Server) Serve(listener net.Listener) error {
	if server.config.Development {
		return server.http.Serve(listener)
	}
	certificate, key, err := server.ensureCertificate()
	if err != nil {
		return err
	}
	return server.http.ServeTLS(listener, certificate, key)
}

func (server *Server) Shutdown(ctx context.Context) error {
	server.cancel()
	shutdownErr := server.http.Shutdown(ctx)
	<-server.pruneDone
	server.sessions.Close()
	server.cleanupMu.Lock()
	if !server.cleanupClosed {
		server.cleanupClosed = true
		close(server.cleanupQueue)
	}
	server.cleanupMu.Unlock()
	cleaned := make(chan struct{})
	go func() {
		server.workersWG.Wait()
		close(cleaned)
	}()
	select {
	case <-cleaned:
	case <-ctx.Done():
		shutdownErr = errors.Join(shutdownErr, ctx.Err())
	}
	closeErr := server.preferences.Close()
	return errors.Join(shutdownErr, closeErr)
}

func (server *Server) routes() http.Handler {
	router := chi.NewRouter()
	// Do not trust forwarding headers until an explicit trusted-proxy policy is
	// configured; RemoteAddr feeds authentication rate limits and audit logs.
	router.Use(middleware.RequestID, middleware.Recoverer, server.logRequest)
	router.Use(server.securityHeaders)

	router.Route("/api/v1", func(router chi.Router) {
		router.Post("/auth/login", server.login)
		router.Group(func(router chi.Router) {
			router.Use(server.requireSession)
			router.Get("/auth/session", server.currentSession)
			router.With(server.requireCSRF).Post("/auth/logout", server.logout)
			router.Get("/admin", server.adminStatus)
			router.With(server.requireCSRF).Post("/admin/elevate", server.adminElevate)
			router.With(server.requireCSRF).Post("/admin/drop", server.adminDrop)
			router.Get("/capabilities", server.capabilities)
			router.Get("/dashboard", server.dashboard)
			router.Get("/metrics", server.metricHistory)
			router.Get("/metrics/stream", server.metricStream)
			router.Get("/preferences/monitoring", server.monitoringPreference)
			router.With(server.requireCSRF).Put("/preferences/monitoring", server.updateMonitoringPreference)
			router.Get("/terminal/ws", server.terminalWebSocket)
			router.Get("/logs", server.logs)
			router.Get("/logs/stream", server.logStream)
			router.Get("/operations", server.operationReceipts)
			router.Get("/users", server.users)
			router.Get("/updates", server.updates)
			router.Get("/services", server.services)
			router.Get("/services/{scope}/{unit}", server.serviceDetail)
			router.With(server.requireCSRF).Post("/services/{scope}/{unit}/actions", server.serviceAction)
			router.Get("/storage", server.storage)
			router.Get("/network", server.network)
			router.Get("/processes", server.processes)
			router.Get("/terminal", server.terminalStatus)
		})
	})
	router.Handle("/*", spaHandler(dashboard.Files()))
	return router
}

func (server *Server) login(writer http.ResponseWriter, request *http.Request) {
	if !server.originAllowed(request) {
		server.logger.Warn("login rejected", zap.String("reason", "origin-not-allowed"), zap.String("origin", request.Header.Get("Origin")))
		problem(writer, http.StatusForbidden, "origin-not-allowed", "Request origin is not allowed")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	var credentials auth.ConversationRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credentials); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		server.logger.Warn("login rejected", zap.String("reason", "invalid-request"), zap.Error(err))
		problem(writer, http.StatusBadRequest, "invalid-request", "Invalid login request")
		return
	}
	developmentStart := server.config.Development && credentials.ConversationID == "" && len(credentials.Responses) == 0 && !credentials.Cancel
	if !credentials.Valid() && !developmentStart {
		problem(writer, http.StatusBadRequest, "invalid-request", "Invalid login request")
		return
	}
	conversationAuthenticator, conversational := server.authenticator.(auth.ConversationAuthenticator)
	if credentials.Cancel {
		if !conversational {
			problem(writer, http.StatusBadRequest, "invalid-request", "Invalid login cancellation")
			return
		}
		if err := conversationAuthenticator.CancelConversation(request.Context(), credentials.ConversationID); err != nil {
			problem(writer, http.StatusServiceUnavailable, "authentication-unavailable", "Authentication service is unavailable")
			return
		}
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if credentials.ConversationID == "" && !server.loginAttempts.Allow(request.RemoteAddr) {
		server.logger.Warn("login rejected", zap.String("reason", "rate-limited"), zap.String("remote_ip", request.RemoteAddr))
		problem(writer, http.StatusTooManyRequests, "rate-limited", "Too many sign-in attempts")
		return
	}
	var identity auth.Identity
	var err error
	if conversational {
		result, conversationErr := conversationAuthenticator.AdvanceConversation(request.Context(), &credentials)
		if conversationErr != nil {
			err = conversationErr
		} else if result.Error != "" {
			if result.Error == "invalid-conversation" {
				problem(writer, http.StatusBadRequest, "invalid-conversation", "Authentication conversation is invalid or expired")
				return
			}
			if result.Error == "authentication-busy" {
				problem(writer, http.StatusServiceUnavailable, "authentication-busy", "Authentication service is busy; try again shortly")
				return
			}
			err = auth.ErrAuthenticationFailed
		} else if len(result.Prompts) > 0 {
			writeJSON(writer, http.StatusAccepted, map[string]any{"conversationId": result.ConversationID, "prompts": result.Prompts})
			return
		} else if result.Identity != nil {
			identity = *result.Identity
			identity.BridgeToken = result.BridgeToken
		} else {
			err = auth.ErrAuthenticationFailed
		}
	} else {
		identity, err = server.authenticator.Authenticate(request.Context(), credentials.Username, credentials.Password)
		credentials.Password = ""
	}
	if err != nil {
		time.Sleep(300 * time.Millisecond)
		if errors.Is(err, auth.ErrServiceUnavailable) {
			server.loginAttempts.Reset(request.RemoteAddr)
			server.logger.Error("authentication service unavailable", zap.String("username", credentials.Username), zap.Error(err))
			problem(writer, http.StatusServiceUnavailable, "authentication-unavailable", "Authentication service is unavailable")
			return
		}
		server.logger.Warn("authentication failed", zap.String("username", credentials.Username), zap.Error(err))
		problem(writer, http.StatusUnauthorized, "authentication-failed", "Authentication failed")
		return
	}
	server.loginAttempts.Reset(request.RemoteAddr)
	created, err := server.sessions.Create(identity)
	if err != nil {
		server.closeUserSession(identity)
		server.logger.Error("session creation failed", zap.String("username", identity.Username), zap.Error(err))
		problem(writer, http.StatusInternalServerError, "session-failed", "Could not create session")
		return
	}
	if controller, ok := server.authenticator.(auth.SessionController); ok && identity.BridgeToken != "" {
		confirmCtx, confirmCancel := context.WithTimeout(request.Context(), 5*time.Second)
		confirmErr := controller.ConfirmUserSession(confirmCtx, identity.BridgeToken)
		confirmCancel()
		if confirmErr != nil {
			if failedIdentity, ok := server.sessions.DeleteWithoutNotify(created.ID); ok {
				server.closeUserSession(failedIdentity)
			}
			server.logger.Error("user session confirmation failed", zap.String("username", identity.Username), zap.Error(confirmErr))
			problem(writer, http.StatusServiceUnavailable, "authentication-unavailable", "Authentication service is unavailable")
			return
		}
	}
	http.SetCookie(writer, &http.Cookie{Name: session.CookieName, Value: created.ID, Path: "/", HttpOnly: true, Secure: !server.config.Development, SameSite: http.SameSiteStrictMode, MaxAge: int((12 * time.Hour).Seconds())})
	server.logger.Info("login succeeded", zap.String("username", identity.Username), zap.Int("uid", identity.UID))
	writeJSON(writer, http.StatusOK, map[string]any{"user": identity, "csrfToken": created.CSRF})
}

func (server *Server) currentSession(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	writeJSON(writer, http.StatusOK, map[string]any{"user": current.Identity, "csrfToken": current.CSRF, "administrative": time.Now().Before(current.AdminUntil), "adminUntil": current.AdminUntil, "adminIdleTimeoutSeconds": int(server.config.AdminIdleTimeout.Seconds())})
}

func (server *Server) adminStatus(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	writeJSON(writer, http.StatusOK, map[string]any{"administrative": time.Now().Before(current.AdminUntil), "until": current.AdminUntil, "idleTimeoutSeconds": int(server.config.AdminIdleTimeout.Seconds())})
}
func (server *Server) adminElevate(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	controller, ok := server.authenticator.(auth.AdministrativeController)
	if !ok {
		problem(writer, http.StatusServiceUnavailable, "administrative-access-unavailable", "Administrative policy service is unavailable")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(body.Password) > 4096 {
		problem(writer, 400, "invalid-request", "Password is required")
		return
	}
	access, err := controller.AuthorizeAdministrative(request.Context(), current.Identity.BridgeToken, body.Password, server.config.AdminIdleTimeout)
	body.Password = ""
	if err != nil {
		if errors.Is(err, auth.ErrServiceUnavailable) {
			problem(writer, http.StatusServiceUnavailable, "administrative-access-unavailable", "Administrative policy service is unavailable")
			return
		}
		problem(writer, 403, "administrative-access-denied", "Could not gain administrative access")
		return
	}
	if previous := current.Identity.AdminToken; previous != "" {
		if revokeErr := controller.RevokeAdministrative(request.Context(), previous); revokeErr != nil {
			server.logger.Warn("previous administrative grant cleanup failed", zap.String("username", current.Identity.Username), zap.Error(revokeErr))
		}
	}
	if !server.sessions.SetAdministrative(current.ID, access.Token, access.Until) {
		_ = controller.RevokeAdministrative(request.Context(), access.Token)
		problem(writer, http.StatusUnauthorized, "session-expired", "Session expired")
		return
	}
	server.logger.Info("administrative access gained", zap.String("username", current.Identity.Username))
	writeJSON(writer, 200, map[string]any{"administrative": true, "until": access.Until})
}
func (server *Server) adminDrop(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	token := server.sessions.DropAdministrative(current.ID)
	if controller, ok := server.authenticator.(auth.AdministrativeController); ok && token != "" {
		closeCtx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		if err := controller.RevokeAdministrative(closeCtx, token); err != nil {
			server.logger.Warn("administrative access revoke failed", zap.String("username", current.Identity.Username), zap.Error(err))
		}
		cancel()
	}
	server.logger.Info("administrative access dropped", zap.String("username", current.Identity.Username))
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) operationReceipts(writer http.ResponseWriter, request *http.Request) {
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			problem(writer, http.StatusBadRequest, "invalid-limit", "Limit must be a positive integer")
			return
		}
		limit = parsed
	}
	items, err := server.preferences.OperationReceipts(request.Context(), limit)
	if err != nil {
		server.logger.Error("operation receipts failed", zap.Error(err))
		problem(writer, http.StatusInternalServerError, "operations-unavailable", "Operation history is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

func (server *Server) logout(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	identity, deleted := server.sessions.DeleteWithoutNotify(current.ID)
	if deleted {
		server.closeUserSession(identity)
	}
	server.logger.Info("logout succeeded", zap.String("username", current.Identity.Username))
	http.SetCookie(writer, &http.Cookie{Name: session.CookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: !server.config.Development, SameSite: http.SameSiteStrictMode})
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		wrapped := middleware.NewWrapResponseWriter(writer, request.ProtoMajor)
		next.ServeHTTP(wrapped, request)
		server.logger.Info("HTTP request",
			zap.String("request_id", middleware.GetReqID(request.Context())),
			zap.String("method", request.Method),
			zap.String("path", request.URL.Path),
			zap.Int("status", wrapped.Status()),
			zap.Int("bytes", wrapped.BytesWritten()),
			zap.String("remote_ip", request.RemoteAddr),
			zap.Duration("duration", time.Since(started)),
		)
	})
}

func (server *Server) capabilities(writer http.ResponseWriter, request *http.Request) {
	capabilities := server.detectCapabilities(request.Context())
	current := request.Context().Value(sessionKey{}).(session.Session)
	for index := range capabilities {
		if capabilities[index].ID == "terminal" && current.Identity.BridgeToken != "" {
			capabilities[index].State = platform.StateReady
			capabilities[index].Readable = true
			capabilities[index].ReadAuthority = "user"
			capabilities[index].MutationAuthority = "user"
			capabilities[index].Contract = "user-bridge"
			capabilities[index].Reason = ""
			capabilities[index].SetupGuidance = ""
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"capabilities": capabilities})
}

func (server *Server) dashboard(writer http.ResponseWriter, _ *http.Request) {
	current, _ := server.metrics.Current()
	writeJSON(writer, http.StatusOK, map[string]any{"host": host.Read(), "metrics": current})
}

func (server *Server) metricHistory(writer http.ResponseWriter, request *http.Request) {
	ranges := map[string]time.Duration{"15m": 15 * time.Minute, "1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour}
	duration := ranges[request.URL.Query().Get("range")]
	if duration == 0 {
		duration = time.Hour
	}
	writeJSON(writer, http.StatusOK, map[string]any{"samples": server.metrics.HistorySince(time.Now().Add(-duration), 1000)})
}

func (server *Server) metricStream(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		problem(writer, http.StatusInternalServerError, "stream-unsupported", "Streaming is unavailable")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-transform")
	writer.Header().Set("X-Accel-Buffering", "no")
	intervals := map[string]time.Duration{"1s": time.Second, "5s": 5 * time.Second, "15s": 15 * time.Second, "30s": 30 * time.Second, "1m": time.Minute, "5m": 5 * time.Minute}
	requested := request.URL.Query().Get("interval")
	interval := intervals[requested]
	if requested != "" && interval == 0 {
		problem(writer, http.StatusBadRequest, "invalid-metric-interval", "Unsupported metric interval")
		return
	}
	if interval == 0 {
		interval = server.config.MonitoringInterval
	}
	stream, unsubscribe := server.metrics.SubscribeEvery(interval)
	defer unsubscribe()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	maximumLifetime := time.NewTimer(15 * time.Minute)
	defer maximumLifetime.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-maximumLifetime.C:
			return
		case sample, ok := <-stream:
			if !ok {
				return
			}
			payload, _ := json.Marshal(sample)
			fmt.Fprintf(writer, "event: metric\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(writer, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (server *Server) terminalWebSocket(writer http.ResponseWriter, request *http.Request) {
	if !server.originAllowed(request) {
		problem(writer, http.StatusForbidden, "origin-not-allowed", "Request origin is not allowed")
		return
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		server.logger.Warn("WebSocket upgrade failed", zap.Error(err))
		return
	}
	defer connection.CloseNow()
	current := request.Context().Value(sessionKey{}).(session.Session)
	if current.Identity.BridgeToken == "" {
		_ = connection.Close(websocket.StatusPolicyViolation, "authenticated user bridge is unavailable")
		return
	}
	columns, _ := strconv.ParseUint(request.URL.Query().Get("columns"), 10, 16)
	rows, _ := strconv.ParseUint(request.URL.Query().Get("rows"), 10, 16)
	terminal, err := auth.OpenTerminal(request.Context(), server.config.SessionSocket, current.Identity.BridgeToken, uint16(columns), uint16(rows))
	if err != nil {
		server.logger.Error("terminal launch failed", zap.String("username", current.Identity.Username), zap.Error(err))
		_ = connection.Close(websocket.StatusInternalError, "terminal launch failed")
		return
	}
	defer terminal.Close()
	done := make(chan struct{}, 2)
	go func() {
		buffer := make([]byte, 32<<10)
		for {
			count, readErr := terminal.Read(buffer)
			if count > 0 && connection.Write(request.Context(), websocket.MessageBinary, buffer[:count]) != nil {
				break
			}
			if readErr != nil {
				break
			}
		}
		done <- struct{}{}
	}()
	go func() {
		for {
			kind, payload, readErr := connection.Read(request.Context())
			if readErr != nil {
				break
			}
			if kind == websocket.MessageBinary {
				if _, writeErr := terminal.Write(payload); writeErr != nil {
					break
				}
				continue
			}
			var resize struct {
				Columns uint16 `json:"columns"`
				Rows    uint16 `json:"rows"`
			}
			if json.Unmarshal(payload, &resize) == nil && resize.Columns > 0 && resize.Rows > 0 {
				_ = auth.ResizeTerminal(request.Context(), server.config.SessionSocket, current.Identity.BridgeToken, resize.Columns, resize.Rows)
			}
		}
		done <- struct{}{}
	}()
	absoluteExpiry := time.NewTimer(time.Until(current.CreatedAt.Add(12 * time.Hour)))
	defer absoluteExpiry.Stop()
	select {
	case <-done:
	case <-absoluteExpiry.C:
		_ = connection.Close(websocket.StatusPolicyViolation, "session expired")
	}
}

func (server *Server) processes(writer http.ResponseWriter, _ *http.Request) {
	items, err := platform.Processes()
	server.writeModule(writer, "processes", items, err)
}

func (server *Server) users(writer http.ResponseWriter, _ *http.Request) {
	items, err := platform.Users()
	server.writeModule(writer, "users", items, err)
}

func (server *Server) storage(writer http.ResponseWriter, _ *http.Request) {
	items, err := platform.Mounts()
	server.writeModule(writer, "storage", items, err)
}

func (server *Server) network(writer http.ResponseWriter, _ *http.Request) {
	items, err := platform.Interfaces()
	server.writeModule(writer, "network", items, err)
}

func (server *Server) services(writer http.ResponseWriter, request *http.Request) {
	scope, unitType := request.URL.Query().Get("scope"), request.URL.Query().Get("type")
	if scope == "" {
		scope = "system"
	}
	if unitType == "" {
		unitType = "service"
	}
	validType := map[string]bool{"service": true, "target": true, "socket": true, "timer": true, "path": true}
	if (scope != "system" && scope != "user") || !validType[unitType] {
		problem(writer, 400, "invalid-unit-filter", "Unsupported unit scope or type")
		return
	}
	items, err := platform.Units(request.Context(), scope, unitType)
	server.writeModule(writer, "services", items, err)
}

func (server *Server) serviceDetail(writer http.ResponseWriter, request *http.Request) {
	item, err := platform.UnitDetails(request.Context(), chi.URLParam(request, "scope"), chi.URLParam(request, "unit"))
	if err != nil {
		server.writeModule(writer, "services", nil, err)
		return
	}
	writeJSON(writer, 200, item)
}
func (server *Server) serviceAction(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	scope := chi.URLParam(request, "scope")
	startedAt := time.Now().UTC()
	target := scope + "/" + chi.URLParam(request, "unit")
	if scope == "system" && (current.Identity.AdminToken == "" || !time.Now().Before(current.AdminUntil)) {
		problem(writer, 403, "administrative-access-required", "Gain Administrative access first")
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, 400, "invalid-request", "Action is required")
		return
	}
	target = scope + "/" + chi.URLParam(request, "unit") + "/" + body.Action
	var actionErr error
	if scope == "system" {
		actionErr = auth.ServiceActionAsAdmin(request.Context(), server.config.SessionSocket, current.Identity.AdminToken, scope, chi.URLParam(request, "unit"), body.Action)
	} else {
		actionErr = auth.ServiceAction(request.Context(), server.config.SessionSocket, current.Identity.BridgeToken, scope, chi.URLParam(request, "unit"), body.Action)
	}
	if actionErr != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "service action failed", scope == "system")
		problem(writer, 502, "service-action-failed", actionErr.Error())
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", scope == "system")
	item, err := platform.UnitDetails(request.Context(), chi.URLParam(request, "scope"), chi.URLParam(request, "unit"))
	if err != nil {
		problem(writer, 502, "service-refresh-failed", err.Error())
		return
	}
	writeJSON(writer, 200, item)
}

func (server *Server) recordOperation(ctx context.Context, actor, target string, startedAt time.Time, result, failure string, administrative bool) {
	_, err := server.preferences.RecordOperation(ctx, preferences.OperationReceipt{
		Actor:          actor,
		Target:         target,
		StartedAt:      startedAt,
		CompletedAt:    time.Now().UTC(),
		Result:         result,
		Error:          failure,
		Administrative: administrative,
	})
	if err != nil {
		server.logger.Warn("operation receipt failed", zap.String("target", target), zap.Error(err))
	}
}

func (server *Server) logs(writer http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	items, err := platform.Logs(request.Context(), limit)
	if unit := request.URL.Query().Get("unit"); unit != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.Unit == unit {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	server.writeModule(writer, "logs", items, err)
}

func (server *Server) logStream(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		problem(writer, http.StatusInternalServerError, "stream-unsupported", "Streaming is unavailable")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-transform")
	writer.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(writer, ": connected\n\n")
	flusher.Flush()
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	entries := make(chan platform.LogEntry, 64)
	go func() {
		defer close(entries)
		err := platform.FollowLogs(ctx, func(entry platform.LogEntry) error {
			if unit := request.URL.Query().Get("unit"); unit != "" && entry.Unit != unit {
				return nil
			}
			select {
			case entries <- entry:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil && ctx.Err() == nil {
			server.logger.Error("log stream stopped", zap.Error(err))
		}
	}()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case entry, open := <-entries:
			if !open {
				return
			}
			payload, _ := json.Marshal(entry)
			fmt.Fprintf(writer, "event: log\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(writer, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (server *Server) updates(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, platform.Updates(request.Context()))
}

func (server *Server) terminalStatus(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	available := current.Identity.BridgeToken != ""
	message := "Terminal is ready for this authenticated UNIX user."
	if !available {
		message = "Terminal requires the production PAM session service; it is disabled by the development authenticator."
	}
	writeJSON(writer, http.StatusOK, map[string]any{"available": available, "message": message})
}

func (server *Server) writeModule(writer http.ResponseWriter, module string, items any, err error) {
	if err != nil {
		server.logger.Error("module unavailable", zap.String("module", module), zap.Error(err))
		problem(writer, http.StatusServiceUnavailable, module+"-unavailable", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

type sessionKey struct{}

func (server *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(session.CookieName)
		if err != nil {
			problem(writer, http.StatusUnauthorized, "no-session", "Authentication required")
			return
		}
		current, ok := server.sessions.Get(cookie.Value)
		if !ok {
			problem(writer, http.StatusUnauthorized, "session-expired", "Session expired")
			return
		}
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), sessionKey{}, current)))
	})
}

func (server *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := request.Context().Value(sessionKey{}).(session.Session)
		if request.Header.Get("X-CSRF-Token") != current.CSRF || !server.originAllowed(request) {
			problem(writer, http.StatusForbidden, "csrf-failed", "Request verification failed")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) originAllowed(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" && server.config.Development {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	for _, allowed := range server.config.AllowedOrigins {
		if strings.EqualFold(strings.TrimSuffix(allowed, "/"), strings.TrimSuffix(origin, "/")) {
			return true
		}
	}
	return false
}

func (server *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if !server.config.Development {
			writer.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) ensureCertificate() (string, string, error) {
	certificate := server.config.Certificate
	key := server.config.CertificateKey
	if certificate != "" || key != "" {
		if certificate == "" || key == "" {
			return "", "", errors.New("certificate and certificate_key must be configured together")
		}
		return certificate, key, nil
	}
	if err := os.MkdirAll(server.config.DataDir, 0o700); err != nil {
		return "", "", err
	}
	certificate = filepath.Join(server.config.DataDir, "tako.crt")
	key = filepath.Join(server.config.DataDir, "tako.key")
	if _, err := os.Stat(certificate); err == nil {
		return certificate, key, nil
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	dnsNames := []string{"localhost"}
	if hostname, hostnameErr := os.Hostname(); hostnameErr == nil && hostname != "" && hostname != "localhost" {
		dnsNames = append(dnsNames, hostname)
	}
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if addresses, addressErr := net.InterfaceAddrs(); addressErr == nil {
		for _, address := range addresses {
			if ip, _, parseErr := net.ParseCIDR(address.String()); parseErr == nil && !ip.IsUnspecified() {
				ipAddresses = append(ipAddresses, ip)
			}
		}
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Tako local certificate"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: dnsNames, IPAddresses: ipAddresses}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", err
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}), 0o600); err != nil {
		return "", "", err
	}
	return certificate, key, nil
}

func spaHandler(files fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := strings.TrimPrefix(request.URL.Path, "/")
		if path != "" {
			if file, err := files.Open(path); err == nil {
				_ = file.Close()
				if strings.Contains(filepath.Base(path), ".") && path != "index.html" {
					writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(writer, request)
				return
			}
		}
		request.URL.Path = "/"
		writer.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(writer, request)
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func problem(writer http.ResponseWriter, status int, code, detail string) {
	writeJSON(writer, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
}

type loginAttempt struct {
	count int
	until time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string]loginAttempt
}

func newLoginLimiter(limit int, window time.Duration) *loginLimiter {
	return &loginLimiter{limit: limit, window: window, attempts: make(map[string]loginAttempt)}
}

func (limiter *loginLimiter) Allow(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	now := time.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	attempt := limiter.attempts[host]
	if now.After(attempt.until) {
		attempt = loginAttempt{until: now.Add(limiter.window)}
	}
	if attempt.count >= limiter.limit {
		return false
	}
	attempt.count++
	limiter.attempts[host] = attempt
	return true
}

func (limiter *loginLimiter) Reset(address string) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	limiter.mu.Lock()
	delete(limiter.attempts, host)
	limiter.mu.Unlock()
}

func activatedListener() (net.Listener, error) {
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
		return nil, nil
	}
	file := os.NewFile(3, "systemd-listener")
	if file == nil {
		return nil, errors.New("invalid systemd listener")
	}
	defer file.Close()
	listener, err := net.FileListener(file)
	if err != nil {
		return nil, err
	}
	_ = os.Unsetenv("LISTEN_PID")
	_ = os.Unsetenv("LISTEN_FDS")
	return listener, nil
}
