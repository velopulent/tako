package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/csv"
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
	config                config.Config
	http                  *http.Server
	sessions              *session.Store
	authenticator         auth.Authenticator
	metrics               *metrics.Sampler
	preferences           *preferences.Store
	jobs                  *diagnosticJobManager
	loginAttempts         *loginLimiter
	logger                *zap.Logger
	cancel                context.CancelFunc
	cleanupQueue          chan auth.Identity
	pruneDone             chan struct{}
	workersWG             sync.WaitGroup
	cleanupMu             sync.Mutex
	cleanupClosed         bool
	hostMu                sync.Mutex
	hostSnapshot          host.Info
	hostSnapshotAt        time.Time
	readHostConfiguration func(context.Context) (platform.HostConfiguration, error)
	readPowerStatusFn     func(context.Context) (platform.PowerStatus, error)
	readUnitDetails       func(context.Context, string, string) (platform.UnitDetail, error)
	readUnitConfiguration func(context.Context, string, string) (platform.UnitConfiguration, error)
	readUpdatesFn         func(context.Context) platform.UpdateStatus
	previewUpdatesFn      func(context.Context, platform.UpdateOperation) (platform.UpdatePreview, error)
	applyUpdatesFn        func(context.Context, auth.UpdateRequest) (platform.UpdateResult, error)
	updateTokensMu        sync.Mutex
	updateTokens          map[string]string
	updateSlots           chan struct{}
	applyTimer            func(context.Context, auth.TimerRequest) (platform.TimerState, error)
	applyOverride         func(context.Context, auth.OverrideRequest) (platform.OverrideState, error)
	previewAccountFn      func(context.Context, auth.LocalAccountRequest) (platform.LocalAccountPreview, error)
	applyAccountFn        func(context.Context, auth.LocalAccountRequest) (platform.LocalAccountState, error)
	previewGroupFn        func(context.Context, auth.GroupMembershipRequest) (platform.GroupMembershipPreview, error)
	applyGroupFn          func(context.Context, auth.GroupMembershipRequest) (platform.GroupMembershipState, error)
	previewAdminRoleFn    func(context.Context, auth.AdministrativeRoleRequest) (platform.AdministrativeRolePreview, error)
	applyAdminRoleFn      func(context.Context, auth.AdministrativeRoleRequest) (platform.AdministrativeRoleState, error)
	changePasswordFn      func(context.Context, auth.PasswordChangeRequest) error
	previewSSHKeysFn      func(context.Context, auth.SSHKeyRequest) (platform.SSHKeyPreview, error)
	applySSHKeysFn        func(context.Context, auth.SSHKeyRequest) (platform.SSHKeyState, error)
	queryLogs             func(context.Context, platform.JournalQuery) (platform.JournalPage, error)
	queryLoginHistory     func(context.Context, platform.LoginHistoryQuery) (platform.LoginHistoryPage, error)
	followLogs            func(context.Context, platform.JournalQuery, func(platform.LogEntry) error) error
	processTracker        *platform.ProcessTracker
	readProcessDetails    func(context.Context, int, uint64) (platform.ProcessDetails, error)
	signalProcesses       func(context.Context, auth.SignalRequest) (platform.SignalResult, error)
	detectCapabilities    func(context.Context) []platform.Capability
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
	if err := preferenceStore.RecoverJobs(context.Background()); err != nil {
		_ = preferenceStore.Close()
		cancel()
		return nil, err
	}
	sessions := session.NewStore(15*time.Minute, 12*time.Hour)
	processTracker := platform.NewProcessTracker()
	go sampler.Run(ctx, cfg.MonitoringInterval)
	server := &Server{
		config:                cfg,
		sessions:              sessions,
		authenticator:         authenticator,
		metrics:               sampler,
		preferences:           preferenceStore,
		loginAttempts:         newLoginLimiter(5, time.Minute),
		logger:                zap.L().Named("gateway"),
		cancel:                cancel,
		cleanupQueue:          make(chan auth.Identity, 64),
		pruneDone:             make(chan struct{}),
		detectCapabilities:    platform.Detect,
		readHostConfiguration: platform.ReadHostConfiguration,
		readPowerStatusFn:     platform.ReadPowerStatus,
		readUnitDetails:       platform.UnitDetails,
		readUnitConfiguration: platform.ReadUnitConfiguration,
		readUpdatesFn:         platform.Updates,
		previewUpdatesFn: func(ctx context.Context, operation platform.UpdateOperation) (platform.UpdatePreview, error) {
			return platform.PreviewUpdates(ctx, operation, platform.Updates)
		},
		applyUpdatesFn: func(ctx context.Context, request auth.UpdateRequest) (platform.UpdateResult, error) {
			return auth.ApplyUpdates(ctx, cfg.SessionSocket, request)
		},
		updateTokens: make(map[string]string),
		updateSlots:  make(chan struct{}, 1),
		applyTimer: func(ctx context.Context, request auth.TimerRequest) (platform.TimerState, error) {
			return auth.ApplyTimer(ctx, cfg.SessionSocket, request)
		},
		applyOverride: func(ctx context.Context, request auth.OverrideRequest) (platform.OverrideState, error) {
			return auth.ApplyOverride(ctx, cfg.SessionSocket, request)
		},
		previewAccountFn: func(ctx context.Context, request auth.LocalAccountRequest) (platform.LocalAccountPreview, error) {
			return auth.PreviewLocalAccount(ctx, cfg.SessionSocket, request)
		},
		applyAccountFn: func(ctx context.Context, request auth.LocalAccountRequest) (platform.LocalAccountState, error) {
			return auth.ApplyLocalAccount(ctx, cfg.SessionSocket, request)
		},
		previewGroupFn: func(ctx context.Context, request auth.GroupMembershipRequest) (platform.GroupMembershipPreview, error) {
			return auth.PreviewGroupMembership(ctx, cfg.SessionSocket, request)
		},
		applyGroupFn: func(ctx context.Context, request auth.GroupMembershipRequest) (platform.GroupMembershipState, error) {
			return auth.ApplyGroupMembership(ctx, cfg.SessionSocket, request)
		},
		previewAdminRoleFn: func(ctx context.Context, request auth.AdministrativeRoleRequest) (platform.AdministrativeRolePreview, error) {
			return auth.PreviewAdministrativeRole(ctx, cfg.SessionSocket, request)
		},
		applyAdminRoleFn: func(ctx context.Context, request auth.AdministrativeRoleRequest) (platform.AdministrativeRoleState, error) {
			return auth.ApplyAdministrativeRole(ctx, cfg.SessionSocket, request)
		},
		changePasswordFn: func(ctx context.Context, request auth.PasswordChangeRequest) error {
			return auth.ChangePassword(ctx, cfg.SessionSocket, request)
		},
		previewSSHKeysFn: func(ctx context.Context, request auth.SSHKeyRequest) (platform.SSHKeyPreview, error) {
			return auth.PreviewSSHKeys(ctx, cfg.SessionSocket, request)
		},
		applySSHKeysFn: func(ctx context.Context, request auth.SSHKeyRequest) (platform.SSHKeyState, error) {
			return auth.ApplySSHKeys(ctx, cfg.SessionSocket, request)
		},
		queryLogs:          platform.QueryLogs,
		queryLoginHistory:  platform.QueryLoginHistory,
		processTracker:     processTracker,
		readProcessDetails: processTracker.Inspect,
		signalProcesses: func(ctx context.Context, request auth.SignalRequest) (platform.SignalResult, error) {
			return auth.SignalProcesses(ctx, cfg.SessionSocket, request)
		},
		followLogs: func(ctx context.Context, query platform.JournalQuery, emit func(platform.LogEntry) error) error {
			return platform.FollowJournal(ctx, query, emit)
		},
	}
	server.jobs = newDiagnosticJobManager(ctx, preferenceStore, server.runDiagnosticJob)
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
	jobErr := server.jobs.Close(ctx)
	server.clearUpdateTokens()
	closeErr := server.preferences.Close()
	return errors.Join(shutdownErr, closeErr, jobErr)
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
			router.Get("/logs/export", server.logExport)
			router.Get("/log-views", server.savedLogViews)
			router.With(server.requireCSRF).Post("/log-views", server.createSavedLogView)
			router.With(server.requireCSRF).Put("/log-views/{id}", server.updateSavedLogView)
			router.With(server.requireCSRF).Delete("/log-views/{id}", server.deleteSavedLogView)
			router.Get("/operations", server.operationReceipts)
			router.Get("/jobs", server.jobsList)
			router.With(server.requireCSRF).Post("/jobs/host-inventory", server.startHostInventoryJob)
			router.Get("/jobs/{id}", server.jobDetail)
			router.With(server.requireCSRF).Post("/jobs/{id}/cancel", server.cancelJob)
			router.Get("/host/config", server.hostConfiguration)
			router.Post("/host/config/preview", server.previewHostConfiguration)
			router.With(server.requireCSRF).Put("/host/config", server.updateHostConfiguration)
			router.Get("/host/power", server.powerStatus)
			router.Post("/host/power/preview", server.previewPower)
			router.With(server.requireCSRF).Post("/host/power", server.requestPower)
			router.Get("/users", server.users)
			router.Get("/users/{username}/login-history", server.loginHistory)
			router.Get("/groups", server.groups)
			router.Post("/users/account/preview", server.previewLocalAccount)
			router.With(server.requireCSRF).Post("/users/account", server.applyLocalAccount)
			router.With(server.requireCSRF).Post("/users/password", server.changePassword)
			router.Get("/users/ssh-keys", server.sshKeys)
			router.Post("/users/ssh-keys/preview", server.previewSSHKeys)
			router.With(server.requireCSRF).Post("/users/ssh-keys", server.applySSHKeys)
			router.Post("/groups/membership/preview", server.previewGroupMembership)
			router.With(server.requireCSRF).Post("/groups/membership", server.applyGroupMembership)
			router.Post("/groups/admin-role/preview", server.previewAdministrativeRole)
			router.With(server.requireCSRF).Post("/groups/admin-role", server.applyAdministrativeRole)
			router.Get("/updates", server.updates)
			router.Post("/updates/preview", server.previewUpdates)
			router.With(server.requireCSRF).Post("/updates", server.startUpdateJob)
			router.Get("/services", server.services)
			router.Get("/services/{scope}/{unit}", server.serviceDetail)
			router.Get("/services/{scope}/{unit}/configuration", server.serviceConfiguration)
			router.With(server.requireCSRF).Post("/services/{scope}/{unit}/actions/preview", server.serviceActionPreview)
			router.With(server.requireCSRF).Post("/services/{scope}/{unit}/actions", server.serviceAction)
			router.With(server.requireCSRF).Post("/services/{scope}/{unit}/overrides/preview", server.serviceOverridePreview)
			router.With(server.requireCSRF).Post("/services/{scope}/{unit}/overrides", server.serviceOverride)
			router.With(server.requireCSRF).Post("/timers/preview", server.timerPreview)
			router.With(server.requireCSRF).Post("/timers", server.timerAction)
			router.Get("/storage", server.storage)
			router.Get("/network", server.network)
			router.Get("/files", server.filesList)
			router.Get("/files/content", server.fileContent)
			router.Get("/files/text-window", server.fileTextWindow)
			router.With(server.requireCSRF).Post("/files", server.fileOperation)
			router.Get("/processes", server.processes)
			router.Get("/processes/{pid}", server.processDetail)
			router.With(server.requireCSRF).Post("/processes/{pid}/signal/preview", server.processSignalPreview)
			router.With(server.requireCSRF).Post("/processes/{pid}/signal", server.processSignal)
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

func (server *Server) dashboard(writer http.ResponseWriter, request *http.Request) {
	current, _ := server.metrics.Current()
	writeJSON(writer, http.StatusOK, map[string]any{"host": server.hostInfo(request.Context()), "metrics": current})
}

func (server *Server) hostInfo(ctx context.Context) host.Info {
	server.hostMu.Lock()
	defer server.hostMu.Unlock()
	if !server.hostSnapshotAt.IsZero() && time.Since(server.hostSnapshotAt) < 15*time.Second {
		return server.hostSnapshot
	}
	server.hostSnapshot = host.ReadContext(ctx)
	server.hostSnapshotAt = time.Now()
	return server.hostSnapshot
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
	var (
		items []platform.Process
		err   error
	)
	if server.processTracker != nil {
		items, err = server.processTracker.Snapshot()
	} else {
		items, err = platform.Processes()
	}
	server.writeModule(writer, "processes", items, err)
}

func (server *Server) processDetail(writer http.ResponseWriter, request *http.Request) {
	pid, err := strconv.Atoi(chi.URLParam(request, "pid"))
	if err != nil || pid < 1 {
		problem(writer, http.StatusBadRequest, "invalid-process", "Process ID is invalid")
		return
	}
	var started uint64
	if raw := request.URL.Query().Get("started"); raw != "" {
		started, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			problem(writer, http.StatusBadRequest, "invalid-process", "Process start identity is invalid")
			return
		}
	}
	if server.readProcessDetails == nil {
		problem(writer, http.StatusServiceUnavailable, "processes-unavailable", "Process details are unavailable")
		return
	}
	details, err := server.readProcessDetails(request.Context(), pid, started)
	if err != nil {
		switch {
		case errors.Is(err, platform.ErrProcessNotFound):
			problem(writer, http.StatusNotFound, "process-not-found", "The process no longer exists")
		case errors.Is(err, platform.ErrProcessReused):
			problem(writer, http.StatusConflict, "process-reused", "The process ID now refers to a different process")
		default:
			problem(writer, http.StatusServiceUnavailable, "processes-unavailable", "Process details are unavailable")
		}
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

func decodeProcessSignalPayload(writer http.ResponseWriter, request *http.Request, pid int, action string) (platform.SignalOperation, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload processSignalPayload
	if err := decoder.Decode(&payload); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-signal-operation", "Signal or process identity is invalid")
		return platform.SignalOperation{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-signal-operation", "Signal request contains trailing data")
		return platform.SignalOperation{}, false
	}
	operation := platform.SignalOperation{Action: action, Signal: platform.SignalName(payload.Signal), Target: platform.ProcessTarget{PID: pid, Started: payload.Started}, Tree: payload.Tree, ExpectedTargets: payload.ExpectedTargets}
	if err := platform.ValidateSignalOperation(operation); err != nil || (action == "preview" && len(operation.ExpectedTargets) != 0) || (action == "apply" && len(operation.ExpectedTargets) == 0) {
		problem(writer, http.StatusBadRequest, "invalid-signal-operation", "Signal or process identity is invalid")
		return platform.SignalOperation{}, false
	}
	return operation, true
}

func (server *Server) processSignalPreview(writer http.ResponseWriter, request *http.Request) {
	pid, err := strconv.Atoi(chi.URLParam(request, "pid"))
	if err != nil || pid < 1 {
		problem(writer, http.StatusBadRequest, "invalid-process", "Process ID is invalid")
		return
	}
	operation, ok := decodeProcessSignalPayload(writer, request, pid, "preview")
	if !ok {
		return
	}
	preview, err := platform.PreviewSignal(request.Context(), operation)
	if err != nil {
		writeProcessSignalError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (server *Server) processSignal(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	pid, err := strconv.Atoi(chi.URLParam(request, "pid"))
	if err != nil || pid < 1 {
		problem(writer, http.StatusBadRequest, "invalid-process", "Process ID is invalid")
		return
	}
	operation, ok := decodeProcessSignalPayload(writer, request, pid, "apply")
	if !ok {
		return
	}
	if server.signalProcesses == nil {
		problem(writer, http.StatusServiceUnavailable, "process-signal-unavailable", "Process signaling service is unavailable")
		return
	}
	startedAt := time.Now().UTC()
	signalRequest := auth.SignalRequest{Operation: operation}
	if current.Identity.AdminToken != "" {
		signalRequest.AdminToken = current.Identity.AdminToken
	} else {
		signalRequest.Token = current.Identity.BridgeToken
	}
	result, err := server.signalProcesses(request.Context(), signalRequest)
	administrative := signalRequest.AdminToken != ""
	target := fmt.Sprintf("process/%d/%s", pid, operation.Signal)
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "process signal failed", administrative)
		writeProcessSignalError(writer, err)
		return
	}
	if len(result.Failures) > 0 {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "one or more process signals failed", administrative)
	} else {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", administrative)
	}
	writeJSON(writer, http.StatusOK, result)
}

func writeProcessSignalError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, platform.ErrInvalidSignalOperation):
		problem(writer, http.StatusBadRequest, "invalid-signal-operation", "Signal or process identity is invalid")
	case errors.Is(err, platform.ErrProcessNotFound):
		problem(writer, http.StatusNotFound, "process-not-found", "The process no longer exists")
	case errors.Is(err, platform.ErrProcessReused), errors.Is(err, platform.ErrSignalConflict):
		problem(writer, http.StatusConflict, "process-signal-conflict", "The process or tree changed; preview again")
	case errors.Is(err, platform.ErrSignalUnauthorized):
		problem(writer, http.StatusForbidden, "process-signal-unauthorized", "UNIX authority does not permit signaling this process")
	case errors.Is(err, auth.ErrServiceUnavailable):
		problem(writer, http.StatusBadGateway, "process-signal-unavailable", "The process signal service is unavailable")
	default:
		problem(writer, http.StatusBadGateway, "process-signal-failed", "The process signal could not be completed")
	}
}

func (server *Server) users(writer http.ResponseWriter, request *http.Request) {
	inventory, err := platform.ListIdentityInventory(request.Context())
	server.writeModule(writer, "users", inventory.Users, err)
}

func (server *Server) groups(writer http.ResponseWriter, request *http.Request) {
	inventory, err := platform.ListIdentityInventory(request.Context())
	server.writeModule(writer, "groups", inventory.Groups, err)
}

func (server *Server) storage(writer http.ResponseWriter, _ *http.Request) {
	items, err := platform.Mounts()
	server.writeModule(writer, "storage", items, err)
}

func (server *Server) network(writer http.ResponseWriter, _ *http.Request) {
	items, err := platform.Interfaces()
	server.writeModule(writer, "network", items, err)
}

func (server *Server) filesList(writer http.ResponseWriter, request *http.Request) {
	operation := platform.FileOperation{Action: "list", Path: request.URL.Query().Get("path"), ShowHidden: request.URL.Query().Get("hidden") == "true"}
	if operation.Path == "" {
		operation.Path = "."
	}
	result, ok := server.applyFileOperation(writer, request, operation)
	if ok {
		writeJSON(writer, http.StatusOK, result)
	}
}

func (server *Server) fileContent(writer http.ResponseWriter, request *http.Request) {
	path := request.URL.Query().Get("path")
	if path == "" {
		problem(writer, http.StatusBadRequest, "invalid-file-operation", "A file path is required")
		return
	}
	offset, limit := int64(0), int64(platform.MaxFileChunk)
	if raw := request.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			problem(writer, http.StatusBadRequest, "invalid-file-range", "The file offset is invalid")
			return
		}
		offset = parsed
	}
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 || parsed > platform.MaxFileChunk {
			problem(writer, http.StatusBadRequest, "invalid-file-range", "The file range is invalid")
			return
		}
		limit = parsed
	}
	status := http.StatusOK
	if rangeValue := request.Header.Get("Range"); rangeValue != "" {
		start, end, valid := parseFileRange(rangeValue)
		if !valid {
			problem(writer, http.StatusRequestedRangeNotSatisfiable, "invalid-file-range", "Only a single byte range is supported")
			return
		}
		offset, limit = start, end-start+1
		status = http.StatusPartialContent
	}
	result, ok := server.applyFileOperation(writer, request, platform.FileOperation{Action: "read", Path: path, Offset: offset, Limit: limit})
	if !ok {
		return
	}
	writer.Header().Set("Accept-Ranges", "bytes")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if result.Mime != "" {
		writer.Header().Set("Content-Type", result.Mime)
	} else {
		writer.Header().Set("Content-Type", http.DetectContentType(result.Content))
	}
	if status == http.StatusPartialContent {
		end := result.Offset + int64(len(result.Content)) - 1
		if len(result.Content) == 0 {
			end = result.Offset
		}
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", result.Offset, end, result.Total))
	}
	writer.Header().Set("Content-Length", strconv.Itoa(len(result.Content)))
	writer.WriteHeader(status)
	_, _ = writer.Write(result.Content)
}

func (server *Server) fileTextWindow(writer http.ResponseWriter, request *http.Request) {
	path := request.URL.Query().Get("path")
	lineOffset, lineLimit := 0, 200
	var err error
	if raw := request.URL.Query().Get("offset"); raw != "" {
		lineOffset, err = strconv.Atoi(raw)
	}
	if raw := request.URL.Query().Get("limit"); raw != "" {
		lineLimit, err = strconv.Atoi(raw)
	}
	if path == "" || err != nil || lineOffset < 0 || lineLimit < 1 || lineLimit > 1000 {
		problem(writer, http.StatusBadRequest, "invalid-text-window", "The text window is invalid")
		return
	}
	result, ok := server.applyFileOperation(writer, request, platform.FileOperation{Action: "read-window", Path: path, LineOffset: lineOffset, LineLimit: lineLimit})
	if !ok {
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) fileOperation(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, 8<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var operation platform.FileOperation
	if err := decoder.Decode(&operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-file-operation", "File operation is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-file-operation", "File operation contains trailing data")
		return
	}
	if result, ok := server.applyFileOperation(writer, request, operation); ok {
		writeJSON(writer, http.StatusOK, result)
	}
}

func (server *Server) applyFileOperation(writer http.ResponseWriter, request *http.Request, operation platform.FileOperation) (platform.FileResult, bool) {
	current, ok := request.Context().Value(sessionKey{}).(session.Session)
	if !ok {
		problem(writer, http.StatusUnauthorized, "no-session", "Authentication required")
		return platform.FileResult{}, false
	}
	fileRequest := auth.FileRequest{Operation: operation}
	administrative := current.Identity.AdminToken != "" && time.Now().Before(current.AdminUntil)
	if administrative {
		fileRequest.AdminToken = current.Identity.AdminToken
		fileRequest.Administrative = true
	} else if current.Identity.BridgeToken != "" {
		fileRequest.Token = current.Identity.BridgeToken
	} else {
		problem(writer, http.StatusForbidden, "user-session-required", "A live UNIX user session is required for file access")
		return platform.FileResult{}, false
	}
	startedAt := time.Now().UTC()
	result, err := auth.ApplyFileOperation(request.Context(), server.config.SessionSocket, fileRequest)
	if err != nil {
		writeFileOperationError(writer, err)
		server.recordOperation(request.Context(), current.Identity.Username, "file/"+operation.Action, startedAt, "failed", err.Error(), administrative)
		return platform.FileResult{}, false
	}
	if operation.Action != "list" && operation.Action != "stat" && operation.Action != "read" && operation.Action != "read-window" && operation.Action != "search" {
		server.recordOperation(request.Context(), current.Identity.Username, "file/"+operation.Action, startedAt, "succeeded", "", administrative)
	}
	return result, true
}

func writeFileOperationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, platform.ErrInvalidFileOperation):
		problem(writer, http.StatusBadRequest, "invalid-file-operation", "File operation is invalid")
	case errors.Is(err, platform.ErrFileNotFound):
		problem(writer, http.StatusNotFound, "file-not-found", "The file was not found")
	case errors.Is(err, platform.ErrFilePermission):
		problem(writer, http.StatusForbidden, "file-permission-denied", "UNIX authority denied this file operation")
	case errors.Is(err, platform.ErrFileConflict):
		problem(writer, http.StatusConflict, "file-conflict", "The file changed; preview the operation again")
	case errors.Is(err, platform.ErrFileTooLarge):
		problem(writer, http.StatusRequestEntityTooLarge, "file-too-large", "The file exceeds the bounded operation limit")
	case errors.Is(err, platform.ErrUnsafeArchive):
		problem(writer, http.StatusBadRequest, "unsafe-archive", "The archive contains an unsafe path or link")
	case errors.Is(err, platform.ErrArchiveLimit):
		problem(writer, http.StatusRequestEntityTooLarge, "archive-limit", "The archive exceeds the bounded operation limit")
	case errors.Is(err, auth.ErrServiceUnavailable):
		problem(writer, http.StatusBadGateway, "file-service-unavailable", "The file service is unavailable")
	default:
		problem(writer, http.StatusBadGateway, "file-operation-failed", "The file operation failed")
	}
}

func parseFileRange(value string) (int64, int64, bool) {
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "bytes="), "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, false
	}
	start, startErr := strconv.ParseInt(parts[0], 10, 64)
	end, endErr := strconv.ParseInt(parts[1], 10, 64)
	if startErr != nil || endErr != nil || start < 0 || end < start || end-start+1 > platform.MaxFileChunk {
		return 0, 0, false
	}
	return start, end, true
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
	scope := chi.URLParam(request, "scope")
	unit := chi.URLParam(request, "unit")
	if err := platform.ValidateServiceTarget(scope, unit); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-service-operation", "Unsupported service scope or unit")
		return
	}
	item, err := server.readUnitDetails(request.Context(), scope, unit)
	if err != nil {
		server.writeModule(writer, "services", nil, err)
		return
	}
	writeJSON(writer, 200, item)
}

func (server *Server) serviceConfiguration(writer http.ResponseWriter, request *http.Request) {
	scope := chi.URLParam(request, "scope")
	unit := chi.URLParam(request, "unit")
	if err := platform.ValidateServiceTarget(scope, unit); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-service-operation", "Unsupported service scope or unit")
		return
	}
	item, err := server.readUnitConfiguration(request.Context(), scope, unit)
	if err != nil {
		problem(writer, http.StatusServiceUnavailable, "service-configuration-unavailable", "Unit configuration is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

func decodeServiceOperation(writer http.ResponseWriter, request *http.Request) (platform.ServiceOperation, bool) {
	var body struct {
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-request", "Action is required")
		return platform.ServiceOperation{}, false
	}
	operation, err := platform.ParseServiceOperation(chi.URLParam(request, "scope"), chi.URLParam(request, "unit"), body.Action)
	if err != nil {
		problem(writer, http.StatusBadRequest, "invalid-service-operation", "Unsupported service scope, unit, or action")
		return platform.ServiceOperation{}, false
	}
	return operation, true
}

func serviceAuthority(current session.Session, operation platform.ServiceOperation) (int, string, string, bool) {
	if operation.Scope == "system" && (current.Identity.AdminToken == "" || !time.Now().Before(current.AdminUntil) || current.Identity.BridgeToken == "") {
		return http.StatusForbidden, "administrative-access-required", "Gain Administrative access first", false
	}
	if operation.Scope == "user" && current.Identity.BridgeToken == "" {
		return http.StatusForbidden, "user-session-required", "An authenticated UNIX session is required", false
	}
	return 0, "", "", true
}

func (server *Server) serviceActionPreview(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	operation, ok := decodeServiceOperation(writer, request)
	if !ok {
		return
	}
	if status, code, message, authorized := serviceAuthority(current, operation); !authorized {
		problem(writer, status, code, message)
		return
	}
	detail, err := server.readUnitDetails(request.Context(), operation.Scope, operation.Unit)
	if err != nil {
		problem(writer, http.StatusBadGateway, "service-preview-unavailable", "Could not inspect service impact")
		return
	}
	writeJSON(writer, http.StatusOK, platform.ServiceImpactForAction(detail, operation.Action))
}

func (server *Server) serviceAction(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	startedAt := time.Now().UTC()
	operation, ok := decodeServiceOperation(writer, request)
	if !ok {
		return
	}
	if status, code, message, authorized := serviceAuthority(current, operation); !authorized {
		problem(writer, status, code, message)
		return
	}
	target := operation.Scope + "/" + operation.Unit + "/" + operation.Action
	var actionErr error
	if operation.Scope == "system" {
		actionErr = auth.ServiceActionAsAdmin(request.Context(), server.config.SessionSocket, current.Identity.AdminToken, operation.Scope, operation.Unit, operation.Action)
	} else {
		actionErr = auth.ServiceAction(request.Context(), server.config.SessionSocket, current.Identity.BridgeToken, operation.Scope, operation.Unit, operation.Action)
	}
	if actionErr != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "service action failed", operation.Scope == "system")
		problem(writer, http.StatusBadGateway, "service-action-failed", "Service action could not be completed; inspect operation history")
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", operation.Scope == "system")
	item, err := server.readUnitDetails(request.Context(), operation.Scope, operation.Unit)
	if err != nil {
		problem(writer, 502, "service-refresh-failed", err.Error())
		return
	}
	writeJSON(writer, 200, item)
}

func decodeTimerOperation(writer http.ResponseWriter, request *http.Request) (platform.TimerOperation, bool) {
	var operation platform.TimerOperation
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&operation) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-request", "Invalid timer operation")
		return platform.TimerOperation{}, false
	}
	if err := platform.ValidateTimerOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-timer-operation", "Unsupported timer scope, name, schedule, or command")
		return platform.TimerOperation{}, false
	}
	return operation, true
}

func timerAuthority(current session.Session, operation platform.TimerOperation) (int, string, string, bool) {
	if operation.Scope == "system" && (current.Identity.AdminToken == "" || !time.Now().Before(current.AdminUntil) || current.Identity.BridgeToken == "") {
		return http.StatusForbidden, "administrative-access-required", "Gain Administrative access first", false
	}
	if operation.Scope == "user" && current.Identity.BridgeToken == "" {
		return http.StatusForbidden, "user-session-required", "An authenticated UNIX session is required", false
	}
	return 0, "", "", true
}

func timerRequestFor(current session.Session, operation platform.TimerOperation) auth.TimerRequest {
	request := auth.TimerRequest{Operation: operation}
	if operation.Scope == "system" {
		request.AdminToken = current.Identity.AdminToken
	} else {
		request.Token = current.Identity.BridgeToken
	}
	return request
}

func (server *Server) timerPreview(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	operation, ok := decodeTimerOperation(writer, request)
	if !ok {
		return
	}
	if status, code, message, authorized := timerAuthority(current, operation); !authorized {
		problem(writer, status, code, message)
		return
	}
	operation.Action = "preview"
	operation.ExpectedFingerprint = ""
	state, err := server.applyTimer(request.Context(), timerRequestFor(current, operation))
	if err != nil {
		writeTimerError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (server *Server) timerAction(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	startedAt := time.Now().UTC()
	operation, ok := decodeTimerOperation(writer, request)
	if !ok {
		return
	}
	if operation.Action == "preview" {
		problem(writer, http.StatusBadRequest, "invalid-timer-operation", "Use the preview endpoint for read-only inspection")
		return
	}
	if status, code, message, authorized := timerAuthority(current, operation); !authorized {
		problem(writer, status, code, message)
		return
	}
	state, err := server.applyTimer(request.Context(), timerRequestFor(current, operation))
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, operation.Scope+"/"+operation.Name+"/"+operation.Action, startedAt, "failed", "timer operation failed", operation.Scope == "system")
		writeTimerError(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, operation.Scope+"/"+operation.Name+"/"+operation.Action, startedAt, "succeeded", "", operation.Scope == "system")
	writeJSON(writer, http.StatusOK, state)
}

func writeTimerError(writer http.ResponseWriter, err error) {
	code := err.Error()
	switch code {
	case "invalid-timer-operation":
		problem(writer, http.StatusBadRequest, code, "Timer operation is invalid")
	case "timer-conflict":
		problem(writer, http.StatusConflict, code, "Timer changed; refresh it before applying this operation")
	case "timer-not-found", "timer-pair-incomplete":
		problem(writer, http.StatusConflict, code, "Timer files are not in a usable state")
	case "invalid-bridge-token", "user-bridge-unavailable":
		problem(writer, http.StatusForbidden, code, "The authenticated user session is unavailable")
	default:
		problem(writer, http.StatusBadGateway, "timer-operation-failed", "Timer operation could not be completed; inspect operation history")
	}
}

func decodeServiceOverride(writer http.ResponseWriter, request *http.Request) (platform.OverrideOperation, bool) {
	var operation platform.OverrideOperation
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&operation) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		problem(writer, http.StatusBadRequest, "invalid-request", "Invalid service override")
		return platform.OverrideOperation{}, false
	}
	if operation.Scope != chi.URLParam(request, "scope") || operation.Unit != chi.URLParam(request, "unit") {
		problem(writer, http.StatusBadRequest, "invalid-service-override", "Override target does not match the service path")
		return platform.OverrideOperation{}, false
	}
	if err := platform.ValidateOverrideOperation(operation); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-service-override", "Unsupported override field or service target")
		return platform.OverrideOperation{}, false
	}
	return operation, true
}

func serviceOverrideAuthority(current session.Session, operation platform.OverrideOperation) (int, string, string, bool) {
	return serviceAuthority(current, platform.ServiceOperation{Scope: operation.Scope, Unit: operation.Unit, Action: "reload"})
}

func overrideRequestFor(current session.Session, operation platform.OverrideOperation) auth.OverrideRequest {
	request := auth.OverrideRequest{Operation: operation}
	if operation.Scope == "system" {
		request.AdminToken = current.Identity.AdminToken
	} else {
		request.Token = current.Identity.BridgeToken
	}
	return request
}

func (server *Server) serviceOverridePreview(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	operation, ok := decodeServiceOverride(writer, request)
	if !ok {
		return
	}
	if status, code, message, authorized := serviceOverrideAuthority(current, operation); !authorized {
		problem(writer, status, code, message)
		return
	}
	operation.Action = "preview"
	operation.ExpectedFingerprint = ""
	state, err := server.applyOverride(request.Context(), overrideRequestFor(current, operation))
	if err != nil {
		writeOverrideError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (server *Server) serviceOverride(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	startedAt := time.Now().UTC()
	operation, ok := decodeServiceOverride(writer, request)
	if !ok {
		return
	}
	if operation.Action == "preview" {
		problem(writer, http.StatusBadRequest, "invalid-service-override", "Use the preview endpoint for read-only inspection")
		return
	}
	if status, code, message, authorized := serviceOverrideAuthority(current, operation); !authorized {
		problem(writer, status, code, message)
		return
	}
	state, err := server.applyOverride(request.Context(), overrideRequestFor(current, operation))
	target := operation.Scope + "/" + operation.Unit + "/override/" + operation.Action
	if err != nil {
		server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "failed", "service override failed", operation.Scope == "system")
		writeOverrideError(writer, err)
		return
	}
	server.recordOperation(request.Context(), current.Identity.Username, target, startedAt, "succeeded", "", operation.Scope == "system")
	writeJSON(writer, http.StatusOK, state)
}

func writeOverrideError(writer http.ResponseWriter, err error) {
	code := err.Error()
	switch code {
	case "invalid-service-override":
		problem(writer, http.StatusBadRequest, code, "Service override is invalid")
	case "service-override-conflict":
		problem(writer, http.StatusConflict, code, "Override changed; preview it again before applying")
	case "service-override-not-found", "service-override-unmanaged":
		problem(writer, http.StatusConflict, code, "The managed Tako drop-in is unavailable or contains unsupported directives")
	case "invalid-bridge-token", "user-bridge-unavailable":
		problem(writer, http.StatusForbidden, code, "The authenticated user session is unavailable")
	default:
		problem(writer, http.StatusBadGateway, "service-override-failed", "Service override could not be completed; inspect operation history")
	}
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
	query, err := parseJournalQuery(request)
	if err != nil {
		problem(writer, http.StatusBadRequest, "invalid-journal-query", "Unsupported journal filter, cursor, or time range")
		return
	}
	page, err := server.queryLogs(request.Context(), query)
	if err != nil {
		if request.Context().Err() != nil {
			return
		}
		problem(writer, http.StatusServiceUnavailable, "logs-unavailable", "The journal could not be queried")
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func parseJournalQuery(request *http.Request) (platform.JournalQuery, error) {
	values := request.URL.Query()
	limit := 200
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return platform.JournalQuery{}, err
		}
		limit = parsed
	}
	cursor, err := platform.DecodeJournalCursor(values.Get("cursor"))
	if err != nil {
		return platform.JournalQuery{}, err
	}
	query := platform.JournalQuery{
		Limit:      limit,
		Cursor:     cursor,
		Boot:       values.Get("boot"),
		Priority:   values.Get("priority"),
		Unit:       values.Get("unit"),
		Executable: values.Get("executable"),
		Text:       values.Get("text"),
	}
	if raw := values.Get("details"); raw != "" {
		details, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return platform.JournalQuery{}, parseErr
		}
		query.Details = details
	}
	for key, destination := range map[string]*time.Time{"since": &query.Since, "until": &query.Until} {
		if raw := values.Get(key); raw != "" {
			parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
			if parseErr != nil {
				return platform.JournalQuery{}, parseErr
			}
			*destination = parsed
		}
	}
	if err := query.Validate(); err != nil {
		return platform.JournalQuery{}, err
	}
	return query, nil
}

const maxJournalExportBytes = 8 << 20

func (server *Server) logExport(writer http.ResponseWriter, request *http.Request) {
	query, err := parseJournalQuery(request)
	if err != nil {
		problem(writer, http.StatusBadRequest, "invalid-journal-query", "Unsupported journal filter, cursor, or time range")
		return
	}
	if request.URL.Query().Get("limit") == "" {
		query.Limit = platform.MaxJournalPageSize
	}
	if err := query.Validate(); err != nil {
		problem(writer, http.StatusBadRequest, "invalid-journal-query", "Unsupported journal filter, cursor, or time range")
		return
	}
	format := request.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		problem(writer, http.StatusBadRequest, "invalid-export-format", "Export format must be csv or json")
		return
	}
	page, err := server.queryLogs(request.Context(), query)
	if err != nil {
		if request.Context().Err() != nil {
			return
		}
		problem(writer, http.StatusServiceUnavailable, "logs-unavailable", "The journal could not be queried")
		return
	}
	writer.Header().Set("Content-Disposition", `attachment; filename="tako-journal-export.`+format+`"`)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if format == "json" {
		payload, marshalErr := json.Marshal(page)
		if marshalErr != nil || len(payload) > maxJournalExportBytes {
			problem(writer, http.StatusRequestEntityTooLarge, "journal-export-too-large", "The filtered journal export is too large")
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(payload)
		return
	}
	payload, csvErr := encodeJournalCSV(page.Items)
	if csvErr != nil {
		problem(writer, http.StatusRequestEntityTooLarge, "journal-export-too-large", "The filtered journal export is too large")
		return
	}
	writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(payload)
}

func encodeJournalCSV(entries []platform.LogEntry) ([]byte, error) {
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"timestamp", "priority", "unit", "message", "details"}); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		details, err := json.Marshal(entry.Details)
		if err != nil {
			return nil, err
		}
		if err := writer.Write([]string{entry.Timestamp, entry.Priority, entry.Unit, entry.Message, string(details)}); err != nil {
			return nil, err
		}
		writer.Flush()
		if writer.Error() != nil || output.Len() > maxJournalExportBytes {
			return nil, errors.New("journal export exceeds bounded output")
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil || output.Len() > maxJournalExportBytes {
		if err == nil {
			err = errors.New("journal export exceeds bounded output")
		}
		return nil, err
	}
	return output.Bytes(), nil
}

type savedLogViewPayload struct {
	Name             string                     `json:"name"`
	Filter           preferences.SavedLogFilter `json:"filter"`
	ExpectedRevision int64                      `json:"expectedRevision,omitempty"`
}

func journalQueryFromSavedFilter(filter preferences.SavedLogFilter) (platform.JournalQuery, error) {
	query := platform.JournalQuery{
		Limit:      1,
		Boot:       filter.Boot,
		Priority:   filter.Priority,
		Unit:       filter.Unit,
		Executable: filter.Executable,
		Text:       filter.Text,
		Details:    filter.Details,
	}
	for key, destination := range map[string]*time.Time{"since": &query.Since, "until": &query.Until} {
		raw := map[string]string{"since": filter.Since, "until": filter.Until}[key]
		if raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return platform.JournalQuery{}, err
		}
		*destination = parsed
	}
	if err := query.Validate(); err != nil {
		return platform.JournalQuery{}, err
	}
	return query, nil
}

func decodeSavedLogViewPayload(writer http.ResponseWriter, request *http.Request) (savedLogViewPayload, error) {
	request.Body = http.MaxBytesReader(writer, request.Body, 32<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload savedLogViewPayload
	if err := decoder.Decode(&payload); err != nil {
		return savedLogViewPayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return savedLogViewPayload{}, errors.New("trailing request data")
		}
		return savedLogViewPayload{}, err
	}
	if _, err := journalQueryFromSavedFilter(payload.Filter); err != nil {
		return savedLogViewPayload{}, err
	}
	return payload, nil
}

func (server *Server) savedLogViews(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	limit := 100
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			problem(writer, http.StatusBadRequest, "invalid-log-view-limit", "Saved log view limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	items, err := server.preferences.SavedLogViews(request.Context(), current.Identity.Username, limit)
	if err != nil {
		problem(writer, http.StatusInternalServerError, "log-views-unavailable", "Saved log views are unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

func (server *Server) createSavedLogView(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	payload, err := decodeSavedLogViewPayload(writer, request)
	if err != nil || payload.Name == "" {
		problem(writer, http.StatusBadRequest, "invalid-log-view", "Saved log view name or filters are invalid")
		return
	}
	item, err := server.preferences.CreateSavedLogView(request.Context(), current.Identity.Username, payload.Name, payload.Filter)
	if err != nil {
		writeSavedLogViewError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, item)
}

func (server *Server) updateSavedLogView(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	payload, err := decodeSavedLogViewPayload(writer, request)
	if err != nil || payload.Name == "" || payload.ExpectedRevision < 1 {
		problem(writer, http.StatusBadRequest, "invalid-log-view", "Saved log view name, revision, or filters are invalid")
		return
	}
	item, err := server.preferences.UpdateSavedLogView(request.Context(), current.Identity.Username, chi.URLParam(request, "id"), payload.Name, payload.Filter, payload.ExpectedRevision)
	if err != nil {
		writeSavedLogViewError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

func (server *Server) deleteSavedLogView(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	revision, err := strconv.ParseInt(request.URL.Query().Get("expectedRevision"), 10, 64)
	if err != nil || revision < 1 {
		problem(writer, http.StatusBadRequest, "invalid-log-view-revision", "A positive expectedRevision is required")
		return
	}
	if err := server.preferences.DeleteSavedLogView(request.Context(), current.Identity.Username, chi.URLParam(request, "id"), revision); err != nil {
		writeSavedLogViewError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func writeSavedLogViewError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, preferences.ErrConflict):
		problem(writer, http.StatusConflict, "log-view-conflict", "The saved log view changed; refresh it before retrying")
	case errors.Is(err, preferences.ErrSavedLogViewNotFound):
		problem(writer, http.StatusNotFound, "log-view-not-found", "Saved log view was not found")
	default:
		problem(writer, http.StatusBadRequest, "invalid-log-view", "Saved log view name or filters are invalid")
	}
}

func (server *Server) logStream(writer http.ResponseWriter, request *http.Request) {
	query, err := parseJournalQuery(request)
	if err != nil || query.Cursor != "" {
		problem(writer, http.StatusBadRequest, "invalid-journal-query", "Unsupported live journal filter or cursor")
		return
	}
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
		err := server.followLogs(ctx, query, func(entry platform.LogEntry) error {
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
	if server.readUpdatesFn == nil {
		problem(writer, http.StatusServiceUnavailable, "updates-unavailable", "The update inventory service is unavailable")
		return
	}
	status := server.readUpdatesFn(request.Context())
	if status.Fingerprint == "" {
		status.Fingerprint = platform.UpdateFingerprint(status)
	}
	writeJSON(writer, http.StatusOK, status)
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
