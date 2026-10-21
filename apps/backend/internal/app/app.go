package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/branding"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/dashboard"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/idle"
	"github.com/velopulent/tako/internal/session"
	"github.com/velopulent/tako/internal/version"
	"go.uber.org/zap"
)

// backgroundHeader marks a request as automatic polling. Such requests are
// authorized like any other but never count as user activity, so an
// unattended tab still reaches the session idle timeout. EventSource cannot
// set headers and uses the backgroundQuery parameter instead.
const (
	backgroundHeader = "X-Tako-Background"
	backgroundQuery  = "background"
)

type Server struct {
	config        config.Config
	branding      *branding.Service
	http          *http.Server
	sessions      *session.Store
	authenticator auth.Authenticator
	broker        HostBroker
	jobs          *jobManager
	loginAttempts *loginLimiter
	signIns       *signInConversations
	logger        *zap.Logger
	certificates  *certificateStore
	lifetime      context.Context
	cancel        context.CancelFunc
	cleanupQueue  chan auth.Identity
	pruneDone     chan struct{}
	workersWG     sync.WaitGroup
	cleanupMu     sync.Mutex
	cleanupClosed bool
	incidents     *incidentAcks
	idle          *idle.Tracker
	previews      *previewGrants

	hostMu         sync.Mutex
	hostSnapshot   host.Info
	hostSnapshotAt time.Time
}

func New(cfg config.Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())
	var authenticator auth.Authenticator = auth.SocketAuthenticator{Path: cfg.SessionSocket}
	var broker HostBroker = socketHostBroker{path: cfg.SessionSocket}
	if cfg.Development {
		password, err := developmentPassword()
		if err != nil {
			cancel()
			return nil, err
		}
		authenticator = auth.DevelopmentAuthenticator{Password: password}
		broker = newLocalHostBroker(ctx)
	}
	sessions := session.NewStore(cfg.SessionIdleTimeout, cfg.SessionLifetime)
	server := &Server{
		config:        cfg,
		branding:      branding.New(strings.TrimSpace(os.Getenv("TAKO_BRANDING_DIR")), version.Value, cfg.Development),
		sessions:      sessions,
		authenticator: authenticator,
		broker:        broker,
		loginAttempts: newLoginLimiter(5, time.Minute),
		signIns:       newSignInConversations(),
		logger:        zap.L().Named("gateway"),
		lifetime:      ctx,
		cancel:        cancel,
		cleanupQueue:  make(chan auth.Identity, 64),
		pruneDone:     make(chan struct{}),
		incidents:     newIncidentAcks(filepath.Join(cfg.DataDir, "incident-acknowledgements.json")),
		idle:          idle.New(cfg.ServiceIdleTimeout),
		previews:      newPreviewGrants(),
	}
	server.jobs = newJobManager(ctx)
	server.certificates = &certificateStore{load: server.loadCertificate}
	sessions.SetDeleteHook(server.enqueueUserSessionClose)
	sessions.SetCountHook(func(count int) { server.idle.SetBusy("sessions", count > 0) })
	server.jobs.SetBusyHook(func(busy bool) { server.idle.SetBusy("jobs", busy) })
	for range 4 {
		server.workersWG.Add(1)
		go server.cleanupWorker()
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
		Handler:           server.activityMiddleware(server.routes()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return server, nil
}

// developmentPassword is the shared secret for `serve --dev`. Without it any
// local account could sign in to the gateway as the developer.
func developmentPassword() (string, error) {
	if value := os.Getenv("TAKO_DEV_PASSWORD"); value != "" {
		return value, nil
	}
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	password := strconv.FormatUint(new(big.Int).SetBytes(buffer).Uint64(), 36)
	zap.L().Warn("development login: sign in as your own user with this password, or set TAKO_DEV_PASSWORD", zap.String("password", password))
	return password, nil
}

func (server *Server) closeUserSession(identity auth.Identity) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if controller, ok := server.authenticator.(auth.AdministrativeController); ok && identity.AdminToken != "" {
		if err := controller.RevokeAdministrative(ctx, identity.AdminToken); err != nil {
			server.logger.Debug("administrative grant cleanup failed", zap.String("username", identity.Username), zap.Error(err))
		}
	}
	if controller, ok := server.authenticator.(auth.SessionController); ok && identity.BridgeToken != "" {
		if err := controller.CloseUserSession(ctx, identity.BridgeToken); err != nil {
			server.logger.Warn("user session cleanup failed", zap.String("username", identity.Username), zap.Error(err))
		}
	}
}

func (server *Server) enqueueUserSessionClose(identity auth.Identity) {
	if identity.BridgeToken == "" && identity.AdminToken == "" {
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
	listener, err := activatedListener()
	if err != nil {
		return err
	}
	if listener == nil {
		listener, err = net.Listen("tcp", server.config.Address)
		if err != nil {
			return err
		}
		server.logger.Info("listening", zap.String("address", listener.Addr().String()))
	} else {
		server.logger.Info("using systemd listener", zap.String("address", listener.Addr().String()))
		server.idle.Start()
	}
	return server.Serve(listener)
}

// Idle is closed when a socket-activated gateway has been idle long enough to
// shut down. Direct listeners never start the idle tracker.
func (server *Server) Idle() <-chan struct{} {
	return server.idle.Done()
}

func (server *Server) activityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		release := server.idle.Block()
		defer release()
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) Serve(listener net.Listener) error {
	if server.config.Development {
		return server.http.Serve(listener)
	}
	ctx, cancel := context.WithTimeout(server.lifetime, 30*time.Second)
	certificate, err := server.certificates.Reload(ctx)
	cancel()
	if err != nil {
		return err
	}
	server.logCertificate("certificate loaded", certificate)
	go server.renewCertificates()
	server.http.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: server.certificates.get}
	return server.http.ServeTLS(listener, "", "")
}

func (server *Server) Shutdown(ctx context.Context) error {
	server.idle.Stop()
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
	return errors.Join(shutdownErr, server.jobs.Close(ctx))
}

func (server *Server) routes() http.Handler {
	router := chi.NewRouter()
	// Forwarding headers are trusted only from configured proxies; see
	// clientAddress. The client address feeds sign-in limits and audit logs.
	router.Use(middleware.RequestID, middleware.Recoverer, server.logRequest, server.securityHeaders)

	router.Route("/api/v1", func(router chi.Router) {
		router.Post("/auth/login", server.login)
		router.Get("/branding", server.brandingMetadata)
		router.Group(func(router chi.Router) {
			router.Use(server.requireSession, server.requireCSRF)
			router.Get("/auth/session", server.currentSession)
			router.Post("/auth/logout", server.logout)
			router.Post("/auth/activity", server.activity)
			router.Get("/auth/sessions", server.userSessions)
			router.Post("/auth/sessions/{id}/revoke", server.revokeUserSession)
			router.Post("/admin/elevate", server.adminElevate)
			router.Post("/admin/drop", server.adminDrop)

			router.Get("/dashboard", server.dashboard)
			router.Get("/capabilities", server.capabilities)
			router.Get("/metrics", server.metricHistory)
			router.Get("/metrics/stream", server.metricStream)
			router.Get("/host/config", server.hostConfiguration)
			router.Put("/host/config", server.updateHostConfiguration)
			router.Get("/host/power", server.powerStatus)
			router.Post("/host/power", server.requestPower)
			router.Get("/terminal", server.terminalStatus)
			router.Get("/terminal/ws", server.terminalWebSocket)

			router.Get("/logs", server.logs)
			router.Get("/logs/stream", server.logStream)
			router.Get("/logs/export", server.logExport)
			router.Get("/incidents", server.incidentTimeline)
			router.Post("/incidents/{id}/{state}", server.incidentTransition)

			router.Get("/jobs", server.jobsList)
			router.Get("/jobs/{id}", server.jobDetail)
			router.Post("/jobs/{id}/cancel", server.cancelJob)

			router.Get("/accounts/users", server.users)
			router.Get("/accounts/users/{username}/login-history", server.loginHistory)
			router.Post("/accounts/users/account/preview", server.previewLocalAccount)
			router.Post("/accounts/users/account", server.applyLocalAccount)
			router.Post("/accounts/users/password", server.changePassword)
			router.Get("/accounts/users/ssh-keys", server.sshKeys)
			router.Post("/accounts/users/ssh-keys/preview", server.previewSSHKeys)
			router.Post("/accounts/users/ssh-keys", server.applySSHKeys)
			router.Get("/accounts/groups", server.groups)
			router.Post("/accounts/groups/preview", server.previewLocalGroup)
			router.Post("/accounts/groups", server.applyLocalGroup)
			router.Post("/accounts/groups/membership/preview", server.previewGroupMembership)
			router.Post("/accounts/groups/membership", server.applyGroupMembership)
			router.Post("/accounts/groups/admin-role/preview", server.previewAdministrativeRole)
			router.Post("/accounts/groups/admin-role", server.applyAdministrativeRole)

			router.Get("/updates", server.updates)
			router.Post("/updates/preview", server.previewUpdates)
			router.Post("/updates/refresh", server.refreshUpdates)
			router.Post("/updates", server.startUpdateJob)
			router.Get("/updates/history", server.updateHistory)
			router.Get("/updates/live", server.updateLiveStatus)
			router.Get("/updates/kpatch", server.kpatchStatus)
			router.Put("/updates/kpatch", server.applyKpatchSettings)

			router.Get("/services", server.services)
			router.Get("/services/{scope}/{unit}", server.serviceDetail)
			router.Get("/services/{scope}/{unit}/configuration", server.serviceConfiguration)
			router.Post("/services/{scope}/{unit}/actions/preview", server.serviceActionPreview)
			router.Post("/services/{scope}/{unit}/actions", server.serviceAction)
			router.Post("/services/{scope}/{unit}/overrides/preview", server.serviceOverridePreview)
			router.Post("/services/{scope}/{unit}/overrides", server.serviceOverride)
			router.Post("/timers/preview", server.timerPreview)
			router.Post("/timers", server.timerAction)

			router.Get("/storage", server.storage)
			router.Post("/storage/preview", server.storagePreview)
			router.Post("/storage", server.storageApply)
			router.Get("/network", server.network)
			router.Post("/network/preview", server.networkPreview)
			router.Post("/network", server.networkApply)
			router.Get("/firewall", server.firewallStatus)
			router.Post("/firewall/preview", server.firewallPreview)
			router.Post("/firewall", server.firewallApply)
			router.Get("/security", server.securityStatus)
			router.Post("/security/preview", server.securityPreview)
			router.Post("/security", server.securityApply)

			router.Get("/files", server.filesList)
			router.Get("/files/search", server.filesSearch)
			router.Get("/files/content", server.fileContent)
			router.Post("/files", server.fileOperation)
			router.Post("/files/upload", server.fileUpload)

			router.Get("/processes", server.processes)
			router.Get("/processes/{pid}", server.processDetail)
			router.Post("/processes/{pid}/signal/preview", server.processSignalPreview)
			router.Post("/processes/{pid}/signal", server.processSignal)
		})
	})
	router.Get("/branding/{asset}", server.brandingAsset)
	router.Handle("/*", spaHandler())
	return router
}

func (server *Server) login(writer http.ResponseWriter, request *http.Request) {
	if !server.originAllowed(request) {
		server.logger.Warn("login rejected", zap.String("reason", "origin-not-allowed"), zap.String("origin", request.Header.Get("Origin")))
		problem(writer, http.StatusForbidden, "origin-not-allowed", "Request origin is not allowed")
		return
	}
	credentials, ok := decodeBody[auth.ConversationRequest](writer, request, 16<<10, "invalid-request")
	if !ok {
		return
	}
	defer func() {
		credentials.Password = ""
		for index := range credentials.Responses {
			credentials.Responses[index].Value = ""
		}
	}()
	if !credentials.Valid() {
		reject(writer, "invalid-request")
		return
	}
	client := server.clientAddress(request)
	limitKey := clientLimitKey(client)
	if credentials.Cancel {
		server.signIns.Done(credentials.ConversationID)
		if err := server.authenticator.CancelConversation(request.Context(), credentials.ConversationID); err != nil {
			problem(writer, http.StatusServiceUnavailable, "authentication-unavailable", "Authentication service is unavailable")
			return
		}
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if credentials.ConversationID == "" {
		if !server.loginAttempts.Allow(limitKey) {
			server.logger.Warn("login rejected", zap.String("reason", "rate-limited"), zap.String("remote_ip", client))
			problem(writer, http.StatusTooManyRequests, "rate-limited", "Too many sign-in attempts; wait a minute and try again")
			return
		}
		if !server.signIns.Allow(limitKey) {
			server.logger.Warn("login rejected", zap.String("reason", "too-many-open-sign-ins"), zap.String("remote_ip", client))
			problem(writer, http.StatusTooManyRequests, "rate-limited", "Too many sign-in attempts in progress; finish or cancel one first")
			return
		}
	}
	username := credentials.Username
	continuing := credentials.ConversationID
	result, err := server.authenticator.AdvanceConversation(request.Context(), &credentials)
	if err != nil || len(result.Prompts) == 0 {
		server.signIns.Done(continuing)
	}
	if err != nil {
		switch code := auth.ErrorCode(err, ""); {
		case code == "invalid-conversation":
			problem(writer, http.StatusBadRequest, code, "The sign-in attempt expired; start again")
		case code == "authentication-busy":
			problem(writer, http.StatusServiceUnavailable, code, "Authentication service is busy; try again shortly")
		case code == "session-failed":
			server.logger.Error("user session setup failed", zap.String("username", username))
			problem(writer, http.StatusInternalServerError, code, "Could not create your session")
		case errors.Is(err, auth.ErrServiceUnavailable):
			server.logger.Error("authentication service unavailable", zap.Error(err))
			problem(writer, http.StatusServiceUnavailable, "authentication-unavailable", "Authentication service is unavailable")
		default:
			// A uniform delay keeps failed and unknown accounts indistinguishable.
			time.Sleep(300 * time.Millisecond)
			server.logger.Warn("authentication failed", zap.String("username", username), zap.String("remote_ip", client))
			problem(writer, http.StatusUnauthorized, "authentication-failed", "Incorrect username or password")
		}
		return
	}
	if len(result.Prompts) > 0 {
		server.signIns.Track(result.ConversationID, limitKey)
		writeJSON(writer, http.StatusAccepted, map[string]any{"conversationId": result.ConversationID, "prompts": result.Prompts})
		return
	}
	if result.Identity == nil {
		problem(writer, http.StatusUnauthorized, "authentication-failed", "Incorrect username or password")
		return
	}
	identity := *result.Identity
	identity.BridgeToken = result.BridgeToken
	server.loginAttempts.Reset(limitKey)
	created, err := server.sessions.Create(identity)
	if err != nil {
		server.closeUserSession(identity)
		server.logger.Error("session creation failed", zap.String("username", identity.Username), zap.Error(err))
		problem(writer, http.StatusInternalServerError, "session-failed", "Could not create your session")
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
	server.sessions.SetClient(created.ID, client, request.UserAgent())
	http.SetCookie(writer, server.sessionCookie(created.ID, int(server.config.SessionLifetime.Seconds())))
	server.logger.Info("login succeeded", zap.String("username", identity.Username), zap.Int("uid", identity.UID), zap.String("remote_ip", client))
	writeJSON(writer, http.StatusOK, server.sessionPayload(created))
}

func (server *Server) sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: session.CookieName, Value: value, Path: "/", HttpOnly: true, Secure: !server.config.Development, SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
}

func (server *Server) sessionPayload(current session.Session) map[string]any {
	payload := map[string]any{
		"user":                    current.Identity,
		"csrfToken":               current.CSRF,
		"administrative":          hasAdministrativeAccess(current),
		"adminIdleTimeoutSeconds": int(server.config.AdminIdleTimeout.Seconds()),
	}
	if hasAdministrativeAccess(current) {
		payload["adminUntil"] = current.AdminUntil
	}
	return payload
}

func (server *Server) currentSession(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, server.sessionPayload(currentSession(request)))
}

func (server *Server) adminElevate(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	controller, ok := server.authenticator.(auth.AdministrativeController)
	if !ok {
		problem(writer, http.StatusServiceUnavailable, "administrative-access-unavailable", "Administrative policy service is unavailable")
		return
	}
	body, ok := decodeBody[struct {
		Password  string   `json:"password"`
		Responses []string `json:"responses,omitempty"`
	}](writer, request, 16<<10, "invalid-request")
	if !ok {
		return
	}
	// Additional PAM answers (an MFA code) follow the password on separate
	// lines of the policy check's standard input.
	valid := body.Password != "" && len(body.Password) <= 4096 && len(body.Responses) <= 4
	secret := body.Password
	for _, response := range append([]string{body.Password}, body.Responses...) {
		valid = valid && len(response) <= 4096 && !strings.ContainsAny(response, "\x00\r\n")
	}
	for _, response := range body.Responses {
		secret += "\n" + response
	}
	defer func() { body.Password, secret = "", "" }()
	if !valid {
		problem(writer, http.StatusBadRequest, "invalid-request", "Enter your password")
		return
	}
	access, err := controller.AuthorizeAdministrative(request.Context(), current.Identity.BridgeToken, secret, server.config.AdminIdleTimeout)
	if err != nil {
		switch code := auth.ErrorCode(err, ""); {
		case code == "administrative-policy-unavailable" || errors.Is(err, auth.ErrServiceUnavailable):
			problem(writer, http.StatusServiceUnavailable, "administrative-access-unavailable", "No sudo or Polkit policy can grant administrative access")
		case code == "invalid-bridge-token":
			reject(writer, "session-expired")
		default:
			server.logger.Warn("administrative access denied", zap.String("username", current.Identity.Username))
			problem(writer, http.StatusForbidden, "administrative-access-denied", "The password was wrong, or your account may not administer this host")
		}
		return
	}
	if previous := current.Identity.AdminToken; previous != "" {
		_ = controller.RevokeAdministrative(request.Context(), previous)
	}
	if !server.sessions.SetAdministrative(current.ID, access.Token, access.Until) {
		_ = controller.RevokeAdministrative(request.Context(), access.Token)
		reject(writer, "session-expired")
		return
	}
	server.logger.Info("administrative access gained", zap.String("username", current.Identity.Username))
	writeJSON(writer, http.StatusOK, map[string]any{"administrative": true, "until": access.Until})
}

func (server *Server) adminDrop(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	token := server.sessions.DropAdministrative(current.ID)
	if controller, ok := server.authenticator.(auth.AdministrativeController); ok && token != "" {
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		if err := controller.RevokeAdministrative(ctx, token); err != nil {
			server.logger.Debug("administrative access revoke failed", zap.String("username", current.Identity.Username), zap.Error(err))
		}
		cancel()
	}
	server.logger.Info("administrative access dropped", zap.String("username", current.Identity.Username))
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) logout(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if identity, deleted := server.sessions.DeleteWithoutNotify(current.ID); deleted {
		server.closeUserSession(identity)
	}
	server.logger.Info("logout succeeded", zap.String("username", current.Identity.Username))
	http.SetCookie(writer, server.sessionCookie("", -1))
	writer.WriteHeader(http.StatusNoContent)
}

// userSessions lists the signed-in account's own sessions.
func (server *Server) userSessions(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	items := make([]map[string]any, 0)
	for _, item := range server.sessions.ForUser(current.Identity.UID) {
		items = append(items, map[string]any{
			"id":             item.PublicID(),
			"createdAt":      item.CreatedAt,
			"seenAt":         item.SeenAt,
			"current":        item.ID == current.ID,
			"administrative": hasAdministrativeAccess(item),
			"remoteAddress":  item.RemoteAddress,
			"userAgent":      item.UserAgent,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"sessions": items})
}

// revokeUserSession signs out one of the account's own sessions, such as a
// browser left signed in elsewhere.
func (server *Server) revokeUserSession(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	id := chi.URLParam(request, "id")
	if !server.sessions.RevokeForUser(current.Identity.UID, id) {
		problem(writer, http.StatusNotFound, "session-not-found", "That session already ended")
		return
	}
	server.logger.Info("session revoked", zap.String("username", current.Identity.Username), zap.Bool("current", id == current.PublicID()), zap.String("remote_ip", server.clientAddress(request)))
	if id == current.PublicID() {
		http.SetCookie(writer, server.sessionCookie("", -1))
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		wrapped := middleware.NewWrapResponseWriter(writer, request.ProtoMajor)
		next.ServeHTTP(wrapped, request)
		level := zap.DebugLevel
		switch {
		case wrapped.Status() >= 500:
			level = zap.WarnLevel
		case strings.HasPrefix(request.URL.Path, "/api/"):
			level = zap.InfoLevel
		}
		server.logger.Log(level, "HTTP request",
			zap.String("request_id", middleware.GetReqID(request.Context())),
			zap.String("method", request.Method),
			zap.String("path", request.URL.Path),
			zap.Int("status", wrapped.Status()),
			zap.Int("bytes", wrapped.BytesWritten()),
			zap.String("remote_ip", server.clientAddress(request)),
			zap.Duration("duration", time.Since(started)),
		)
	})
}

type sessionKey struct{}

func (server *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(session.CookieName)
		if err != nil {
			problem(writer, http.StatusUnauthorized, "no-session", "Sign in to continue")
			return
		}
		lookup := server.sessions.Get
		if isBackground(request) {
			lookup = server.sessions.Peek
		}
		current, ok := lookup(cookie.Value)
		if !ok {
			reject(writer, "session-expired")
			return
		}
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), sessionKey{}, current)))
	})
}

func isBackground(request *http.Request) bool {
	return request.Header.Get(backgroundHeader) == "1" || request.URL.Query().Get(backgroundQuery) == "1"
}

// activity records user interaction that made no other request, such as
// reading a page or scrolling a log, so the session stays signed in.
func (server *Server) activity(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusNoContent)
}

// requireCSRF verifies every state-changing request. Safe methods pass
// through; the terminal WebSocket upgrade checks its origin separately.
func (server *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet || request.Method == http.MethodHead {
			next.ServeHTTP(writer, request)
			return
		}
		token := request.Header.Get("X-CSRF-Token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(currentSession(request).CSRF)) != 1 || !server.originAllowed(request) {
			problem(writer, http.StatusForbidden, "csrf-failed", "Request verification failed; reload the page")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) originAllowed(request *http.Request) bool {
	origin := strings.TrimSuffix(request.Header.Get("Origin"), "/")
	if origin == "" && server.config.Development {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if server.config.AllowedOrigins == nil {
		scheme := "http"
		if request.TLS != nil {
			scheme = "https"
		}
		return strings.EqualFold(scheme+"://"+request.Host, origin)
	}
	for _, allowed := range server.config.AllowedOrigins {
		if strings.EqualFold(strings.TrimSuffix(allowed, "/"), origin) {
			return true
		}
	}
	return false
}

func (server *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := writer.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy(request.URL.Path))
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		if strings.HasPrefix(request.URL.Path, "/api/") {
			header.Set("Cache-Control", "no-store")
		}
		if !server.config.Development {
			header.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(writer, request)
	})
}

const (
	prodContentSecurityPolicy = "default-src 'self'; connect-src 'self'; img-src 'self' data: blob:; media-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; object-src 'none'; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	// Vite dev injects an inline module preamble and serves HMR over the
	// gateway origin, so the dev proxy needs inline and eval scripts. It is
	// used only when TAKO_VITE_URL points at a loopback Vite server.
	devContentSecurityPolicy = "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; connect-src 'self' ws: wss:; img-src 'self' data: blob:; media-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; object-src 'none'; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
)

// contentSecurityPolicy relaxes script rules only for proxied Vite dev pages.
func contentSecurityPolicy(path string) string {
	if viteDevTarget() != nil && !strings.HasPrefix(path, "/api/") {
		return devContentSecurityPolicy
	}
	return prodContentSecurityPolicy
}

// ensureCertificate returns the generated self-signed pair in the data
// directory, creating it, or replacing it when it is unreadable or close to
// expiry.
func (server *Server) ensureCertificate() (string, string, error) {
	if err := os.MkdirAll(server.config.DataDir, 0o700); err != nil {
		return "", "", err
	}
	certificate := filepath.Join(server.config.DataDir, "tako.crt")
	key := filepath.Join(server.config.DataDir, "tako.key")
	if generatedCertificateUsable(certificate, key, time.Now()) {
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
			if ip, _, parseErr := net.ParseCIDR(address.String()); parseErr == nil && !ip.IsUnspecified() && !ip.IsLoopback() {
				ipAddresses = append(ipAddresses, ip)
			}
		}
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: dnsNames[len(dnsNames)-1], Organization: []string{"Tako"}},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", err
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(key, "PRIVATE KEY", keyBytes, 0o600); err != nil {
		return "", "", err
	}
	if err := writePEM(certificate, "CERTIFICATE", der, 0o644); err != nil {
		return "", "", err
	}
	server.logger.Info("generated self-signed certificate", zap.String("path", certificate), zap.Strings("names", dnsNames))
	return certificate, key, nil
}

// writePEM writes atomically so a crash never leaves a truncated key behind.
func writePEM(path, kind string, der []byte, mode os.FileMode) error {
	return writeFileAtomic(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), mode)
}

// writeFileAtomic replaces path with content in one rename, so readers and
// crashes only ever see the old or the new file.
func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tako-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func spaHandler() http.Handler {
	if target := viteDevTarget(); target != nil {
		proxy := httputil.NewSingleHostReverseProxy(target)
		fallback := spaFileHandler()
		proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, err error) {
			zap.L().Warn("vite dev server unavailable; serving built assets", zap.Error(err))
			fallback.ServeHTTP(writer, request)
		}
		return proxy
	}
	return spaFileHandler()
}

// viteDevTarget returns the Vite dev server URL when TAKO_VITE_URL names a
// loopback address. Production never sets it, so the gateway serves the
// embedded dashboard; host development proxies non-API traffic to Vite.
func viteDevTarget() *url.URL {
	raw := strings.TrimSpace(os.Getenv("TAKO_VITE_URL"))
	if raw == "" {
		return nil
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Port() == "" {
		return nil
	}
	switch target.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return target
	}
	return nil
}

func spaFileHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		files := dashboard.Files()
		fileServer := http.FileServer(http.FS(files))
		path := strings.TrimPrefix(request.URL.Path, "/")
		if path != "" {
			if file, err := files.Open(path); err == nil {
				_ = file.Close()
				if strings.HasPrefix(path, "assets/") {
					writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(writer, request)
				return
			}
			if strings.HasPrefix(path, "assets/") || strings.HasPrefix(path, "api/") {
				http.NotFound(writer, request)
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
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
}

type loginAttempt struct {
	count int
	until time.Time
}

// loginLimiter bounds new sign-in attempts per client address.
type loginLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	attempts  map[string]loginAttempt
	nextSweep time.Time
}

func newLoginLimiter(limit int, window time.Duration) *loginLimiter {
	return &loginLimiter{limit: limit, window: window, attempts: make(map[string]loginAttempt)}
}

// maxTrackedClients bounds limiter memory. When it is reached the entry
// closest to expiry is dropped; refusing every new client instead would let
// one attacker with many addresses lock everyone out.
const maxTrackedClients = 8192

// Allow counts a new sign-in attempt for key, a clientLimitKey.
func (limiter *loginLimiter) Allow(key string) bool {
	now := time.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if now.After(limiter.nextSweep) {
		for tracked, value := range limiter.attempts {
			if now.After(value.until) {
				delete(limiter.attempts, tracked)
			}
		}
		limiter.nextSweep = now.Add(time.Minute)
	}
	if _, exists := limiter.attempts[key]; !exists && len(limiter.attempts) >= maxTrackedClients {
		oldest, oldestUntil := "", time.Time{}
		for tracked, value := range limiter.attempts {
			if oldest == "" || value.until.Before(oldestUntil) {
				oldest, oldestUntil = tracked, value.until
			}
		}
		delete(limiter.attempts, oldest)
	}
	attempt := limiter.attempts[key]
	if now.After(attempt.until) {
		attempt = loginAttempt{until: now.Add(limiter.window)}
	}
	if attempt.count >= limiter.limit {
		return false
	}
	attempt.count++
	limiter.attempts[key] = attempt
	return true
}

func (limiter *loginLimiter) Reset(key string) {
	limiter.mu.Lock()
	delete(limiter.attempts, key)
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
