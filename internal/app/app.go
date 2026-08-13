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
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
	"github.com/velopulent/tako/internal/webui"
	"go.uber.org/zap"
)

type Server struct {
	config        config.Config
	http          *http.Server
	sessions      *session.Store
	authenticator auth.Authenticator
	metrics       *metrics.Sampler
	loginAttempts *loginLimiter
	logger        *zap.Logger
	cancel        context.CancelFunc
}

func New(cfg config.Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())
	authenticator := auth.Authenticator(auth.SocketAuthenticator{Path: cfg.SessionSocket})
	if cfg.Development {
		authenticator = auth.DevelopmentAuthenticator{}
	}
	sampler := metrics.NewSampler(450)
	go sampler.Run(ctx, 2*time.Second)
	server := &Server{
		config:        cfg,
		sessions:      session.NewStore(15*time.Minute, 12*time.Hour),
		authenticator: authenticator,
		metrics:       sampler,
		loginAttempts: newLoginLimiter(5, time.Minute),
		logger:        zap.L().Named("gateway"),
		cancel:        cancel,
	}
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
	return server.http.Shutdown(ctx)
}

func (server *Server) routes() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, server.logRequest)
	router.Use(server.securityHeaders)

	router.Route("/api/v1", func(router chi.Router) {
		router.Post("/auth/login", server.login)
		router.Group(func(router chi.Router) {
			router.Use(server.requireSession)
			router.Get("/auth/session", server.currentSession)
			router.With(server.requireCSRF).Post("/auth/logout", server.logout)
			router.Get("/capabilities", server.capabilities)
			router.Get("/dashboard", server.dashboard)
			router.Get("/metrics", server.metricHistory)
			router.Get("/metrics/stream", server.metricStream)
			router.Get("/terminal/ws", server.terminalWebSocket)
			router.Get("/logs", server.logs)
			router.Get("/logs/stream", server.logStream)
			router.Get("/users", server.users)
			router.Get("/updates", server.updates)
			router.Get("/services", server.services)
			router.Get("/storage", server.storage)
			router.Get("/network", server.network)
			router.Get("/processes", server.processes)
			router.Get("/terminal", server.terminalStatus)
		})
	})
	router.Handle("/*", spaHandler(webui.Files()))
	return router
}

func (server *Server) login(writer http.ResponseWriter, request *http.Request) {
	if !server.originAllowed(request) {
		server.logger.Warn("login rejected", zap.String("reason", "origin-not-allowed"), zap.String("origin", request.Header.Get("Origin")))
		problem(writer, http.StatusForbidden, "origin-not-allowed", "Request origin is not allowed")
		return
	}
	if !server.loginAttempts.Allow(request.RemoteAddr) {
		server.logger.Warn("login rejected", zap.String("reason", "rate-limited"), zap.String("remote_ip", request.RemoteAddr))
		problem(writer, http.StatusTooManyRequests, "rate-limited", "Too many sign-in attempts")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	var credentials auth.Request
	if err := json.NewDecoder(request.Body).Decode(&credentials); err != nil {
		server.logger.Warn("login rejected", zap.String("reason", "invalid-request"), zap.Error(err))
		problem(writer, http.StatusBadRequest, "invalid-request", "Invalid login request")
		return
	}
	identity, err := server.authenticator.Authenticate(request.Context(), credentials.Username, credentials.Password)
	credentials.Password = ""
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
		server.logger.Error("session creation failed", zap.String("username", identity.Username), zap.Error(err))
		problem(writer, http.StatusInternalServerError, "session-failed", "Could not create session")
		return
	}
	http.SetCookie(writer, &http.Cookie{Name: session.CookieName, Value: created.ID, Path: "/", HttpOnly: true, Secure: !server.config.Development, SameSite: http.SameSiteStrictMode, MaxAge: int((12 * time.Hour).Seconds())})
	server.logger.Info("login succeeded", zap.String("username", identity.Username), zap.Int("uid", identity.UID))
	writeJSON(writer, http.StatusOK, map[string]any{"user": identity, "csrfToken": created.CSRF})
}

func (server *Server) currentSession(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	writeJSON(writer, http.StatusOK, map[string]any{"user": current.Identity, "csrfToken": current.CSRF})
}

func (server *Server) logout(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	server.sessions.Delete(current.ID)
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
	capabilities := platform.Detect(request.Context())
	current := request.Context().Value(sessionKey{}).(session.Session)
	for index := range capabilities {
		if capabilities[index].ID == "terminal" && current.Identity.BridgeToken != "" {
			capabilities[index].Available = true
			capabilities[index].Reason = ""
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"capabilities": capabilities})
}

func (server *Server) dashboard(writer http.ResponseWriter, _ *http.Request) {
	current, _ := server.metrics.Current()
	writeJSON(writer, http.StatusOK, map[string]any{"host": host.Read(), "metrics": current})
}

func (server *Server) metricHistory(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"samples": server.metrics.History()})
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
	stream, unsubscribe := server.metrics.Subscribe()
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
	items, err := platform.Units(request.Context())
	server.writeModule(writer, "services", items, err)
}

func (server *Server) logs(writer http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	items, err := platform.Logs(request.Context(), limit)
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
