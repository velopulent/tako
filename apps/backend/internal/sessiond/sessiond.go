// Package sessiond implements Tako's narrow, socket-activated PAM boundary.
// Production packaging runs it as root with systemd hardening; it never listens
// on a network socket and never retains credentials.
package sessiond

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/config"
	"github.com/velopulent/tako/internal/idle"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

type bridgeGrant struct {
	identity     auth.Identity
	environment  map[string]string
	expires      time.Time
	adminToken   string
	adminExpires time.Time
	terminal     *os.File
	bridge       io.Closer
	active       bool
	closePAM     func()
	timer        *time.Timer
}

type grantStore struct {
	mu       sync.Mutex
	notifyMu sync.Mutex
	values   map[string]bridgeGrant
	activity *idle.Tracker
}

type administrativePolicy func(context.Context, auth.Identity, string) error

type serviceBackend interface {
	Run(context.Context, platform.ServiceOperation) error
}

type systemServiceBackend struct{}

func (systemServiceBackend) Run(ctx context.Context, operation platform.ServiceOperation) error {
	if operation.Scope == "user" {
		return errors.New("user service actions require the authenticated user bridge")
	}
	arguments := []string{operation.Action, "--", operation.Unit}
	return exec.CommandContext(ctx, "systemctl", arguments...).Run()
}

var errAdministrativeUnavailable = errors.New("administrative policy unavailable")

// Run starts the privileged local session service. The caller selects this
// process mode explicitly; it never shares a process with the web gateway.
func Run(args []string, updates *platform.UpdateService) error {
	flags := flag.NewFlagSet("sessiond", flag.ContinueOnError)
	socket := flags.String("socket", "/run/tako/session.sock", "Unix socket path")
	configPath := flags.String("config", "/etc/tako/config.toml", "gateway configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtimeConfig, err := config.Load(*configPath, false)
	if err != nil {
		return err
	}

	listener, activated, err := activatedListener()
	if err != nil {
		return err
	}
	if !activated {
		if err := ensureSocketParent(*socket); err != nil {
			return err
		}
		_ = os.Remove(*socket)
		listener, err = net.Listen("unix", *socket)
		if err != nil {
			return err
		}
		if err := os.Chmod(*socket, 0o660); err != nil {
			_ = listener.Close()
			return err
		}
	}
	defer listener.Close()
	logger := zap.L().Named("sessiond")
	logger.Info("session service listening",
		zap.String("network", "unix"),
		zap.String("address", listener.Addr().String()),
		zap.Bool("socket_activated", activated),
	)

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()
	activity := idle.New(runtimeConfig.ServiceIdleTimeout)
	defer activity.Stop()
	if activated {
		activity.Start()
		go func() {
			select {
			case <-activity.Done():
				logger.Info("session service idle; shutting down")
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	service := auth.PAMAuthenticator{Service: "tako"}
	grants := &grantStore{values: make(map[string]bridgeGrant), activity: activity}
	hostRuntime := newHostRuntime(runtimeConfig.MonitoringInterval, runtimeConfig.HistoryRetention)
	hostRuntime.updates = updates
	go hostRuntime.run(ctx)
	defer hostRuntime.close()
	policy := newAdministrativePolicy()
	conversations := newConversationStore(service, func(session auth.UserSession) (string, error) {
		return grants.addUserSession(session)
	})
	conversations.activity = activity
	defer conversations.closeAll()
	requests := newHandler(service, conversations, grants, policy, logger, hostRuntime)
	requests.certificate, requests.certificateKey = runtimeConfig.Certificate, runtimeConfig.CertificateKey
	defer grants.closeAll()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Warn("connection accept failed", zap.Error(err))
			continue
		}
		release := activity.Block()
		go func() {
			defer release()
			requests.serve(conn)
		}()
	}
}

func ensureSocketParent(socket string) error {
	dir := filepath.Dir(socket)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o750)
}

func activatedListener() (net.Listener, bool, error) {
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
		return nil, false, nil
	}
	file := os.NewFile(3, "systemd-session-socket")
	if file == nil {
		return nil, true, errors.New("invalid systemd session socket")
	}
	defer file.Close()
	listener, err := net.FileListener(file)
	return listener, true, err
}

func (store *grantStore) resize(token string, columns, rows uint16) string {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		if ok {
			delete(store.values, token)
		}
		store.mu.Unlock()
		if ok {
			cleanupGrant(grant)
		}
		return "invalid-bridge-token"
	}
	if grant.terminal == nil {
		store.mu.Unlock()
		return "terminal-not-ready"
	}
	if columns == 0 || rows == 0 {
		store.mu.Unlock()
		return "invalid-terminal-size"
	}
	err := pty.Setsize(grant.terminal, &pty.Winsize{Cols: columns, Rows: rows})
	store.mu.Unlock()
	if err != nil {
		return "terminal-resize-failed"
	}
	return ""
}

func (store *grantStore) setTerminal(token string, terminal *os.File) bool {
	store.mu.Lock()
	grant, ok := store.values[token]
	if ok {
		grant.terminal = terminal
		store.values[token] = grant
	}
	store.mu.Unlock()
	return ok
}

func (store *grantStore) addGrant(identity auth.Identity, environment map[string]string, bridge io.Closer, closeSession func()) (string, error) {
	var once sync.Once
	closeResources := func() {
		once.Do(func() {
			if bridge != nil {
				_ = bridge.Close()
			}
			if closeSession != nil {
				closeSession()
			}
		})
	}
	token, err := randomToken(32)
	if err != nil {
		closeResources()
		return "", err
	}
	store.mu.Lock()
	grant := bridgeGrant{identity: identity, environment: environment, expires: time.Now().Add(30 * time.Second), bridge: bridge, closePAM: closeResources}
	grant.timer = time.AfterFunc(30*time.Second, func() { store.expire(token) })
	store.values[token] = grant
	store.mu.Unlock()
	store.notifyActivity()
	return token, nil
}

func (store *grantStore) expire(token string) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok {
		store.mu.Unlock()
		return
	}
	remaining := time.Until(grant.expires)
	if remaining > 0 {
		grant.timer = time.AfterFunc(remaining, func() { store.expire(token) })
		store.values[token] = grant
		store.mu.Unlock()
		return
	}
	delete(store.values, token)
	store.mu.Unlock()
	store.notifyActivity()
	cleanupGrant(grant)
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (store *grantStore) get(token string) (auth.Identity, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		delete(store.values, token)
		store.mu.Unlock()
		if ok {
			store.notifyActivity()
			cleanupGrant(grant)
		}
		return auth.Identity{}, false
	}
	store.mu.Unlock()
	return grant.identity, true
}

func (store *grantStore) bridgeFor(token string) (io.Closer, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	expired := ok && time.Now().After(grant.expires)
	if !ok || expired || grant.bridge == nil {
		store.mu.Unlock()
		if expired {
			store.close(token)
		}
		return nil, false
	}
	bridge := grant.bridge
	store.mu.Unlock()
	return bridge, true
}

func (store *grantStore) authorize(ctx context.Context, token, password string, ttl uint32, policy administrativePolicy) (string, time.Time, error) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		store.mu.Unlock()
		if ok {
			store.close(token)
		}
		return "", time.Time{}, errors.New("invalid-bridge-token")
	}
	identity := grant.identity
	store.mu.Unlock()
	if err := policy(ctx, identity, password); err != nil {
		return "", time.Time{}, err
	}
	adminToken, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, errAdministrativeUnavailable
	}
	duration := time.Duration(ttl) * time.Second
	if duration < time.Minute {
		duration = 5 * time.Minute
	}
	if duration > time.Hour {
		duration = time.Hour
	}
	until := time.Now().Add(duration)
	store.mu.Lock()
	grant, ok = store.values[token]
	if !ok || time.Now().After(grant.expires) {
		store.mu.Unlock()
		return "", time.Time{}, errors.New("invalid-bridge-token")
	}
	grant.adminToken = adminToken
	grant.adminExpires = until
	store.values[token] = grant
	store.mu.Unlock()
	return adminToken, until, nil
}

func (store *grantStore) adminIdentity(token string) (auth.Identity, bool) {
	if token == "" {
		return auth.Identity{}, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := time.Now()
	for _, grant := range store.values {
		if grant.adminToken == token && !grant.adminExpires.IsZero() && now.Before(grant.adminExpires) && now.Before(grant.expires) {
			return grant.identity, true
		}
	}
	return auth.Identity{}, false
}

func (store *grantStore) revokeAdmin(token string) bool {
	if token == "" {
		return false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for key, grant := range store.values {
		if grant.adminToken == token {
			grant.adminToken = ""
			grant.adminExpires = time.Time{}
			store.values[key] = grant
			return true
		}
	}
	return false
}

// claim reserves the session's single terminal.
func (store *grantStore) claim(token string) (auth.Identity, map[string]string, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || grant.active || time.Now().After(grant.expires) {
		store.mu.Unlock()
		return auth.Identity{}, nil, false
	}
	grant.active = true
	store.values[token] = grant
	store.mu.Unlock()
	return grant.identity, grant.environment, true
}

func (store *grantStore) closeAll() {
	store.mu.Lock()
	values := store.values
	store.values = make(map[string]bridgeGrant)
	store.mu.Unlock()
	store.notifyActivity()
	for _, grant := range values {
		cleanupGrant(grant)
	}
}

func (store *grantStore) close(token string) bool {
	store.mu.Lock()
	grant, ok := store.values[token]
	if ok {
		delete(store.values, token)
	}
	store.mu.Unlock()
	if !ok {
		return false
	}
	store.notifyActivity()
	cleanupGrant(grant)
	return true
}

func (store *grantStore) notifyActivity() {
	if store.activity == nil {
		return
	}
	store.notifyMu.Lock()
	defer store.notifyMu.Unlock()
	store.mu.Lock()
	busy := len(store.values) > 0
	store.mu.Unlock()
	store.activity.SetBusy("sessiond-grants", busy)
}

func cleanupGrant(grant bridgeGrant) {
	if grant.timer != nil {
		grant.timer.Stop()
	}
	if grant.terminal != nil {
		_ = grant.terminal.Close()
	}
	if grant.closePAM != nil {
		grant.closePAM()
	}
}

func (store *grantStore) confirm(token string) bool {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		store.mu.Unlock()
		if ok {
			store.close(token)
		}
		return false
	}
	grant.expires = time.Now().Add(12 * time.Hour)
	store.values[token] = grant
	store.mu.Unlock()
	return true
}

func (store *grantStore) addUserSession(session auth.UserSession) (string, error) {
	userBridge, err := startUserBridge(session)
	if err != nil {
		return "", err
	}
	token, err := store.addGrant(session.Identity, session.Environment, userBridge, session.Close)
	if err != nil {
		return "", err
	}
	go func() {
		<-userBridge.exited
		store.close(token)
	}()
	return token, nil
}

func (store *grantStore) release(token string) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if ok {
		grant.active = false
		grant.terminal = nil
		store.values[token] = grant
	}
	store.mu.Unlock()
}

func runTerminal(conn net.Conn, reader io.Reader, identity auth.Identity, environment map[string]string, size auth.TerminalSize, grants *grantStore, token string, logger *zap.Logger) {
	defer grants.release(token)
	account, err := user.LookupId(strconv.Itoa(identity.UID))
	if err != nil {
		logger.Error("terminal user lookup failed", zap.Int("uid", identity.UID), zap.Error(err))
		return
	}
	shell := loginShell(account)
	credential, err := userCredential(account, identity)
	if err != nil {
		logger.Error("terminal credential lookup failed", zap.Int("uid", identity.UID), zap.Error(err))
		return
	}
	command := exec.Command(shell, "-l")
	command.Dir = account.HomeDir
	// The shell inherits the PAM session environment (XDG_RUNTIME_DIR, locale,
	// D-Bus address) so user tools such as `systemctl --user` work.
	command.Env = append(bridgeEnvironment(environment, account), "SHELL="+shell, "TERM=xterm-256color", "COLORTERM=truecolor")
	command.SysProcAttr = &syscall.SysProcAttr{Credential: credential, Setsid: true, Setctty: true}
	if size.Columns == 0 || size.Rows == 0 {
		size = auth.TerminalSize{Columns: 120, Rows: 30}
	}
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: size.Columns, Rows: size.Rows})
	if err != nil {
		logger.Error("terminal launch failed", zap.String("username", identity.Username), zap.Error(err))
		return
	}
	if !grants.setTerminal(token, terminal) {
		_ = terminal.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
		return
	}
	defer terminal.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(terminal, reader); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, terminal); done <- struct{}{} }()
	<-done
	_ = terminal.Close()
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGHUP)
	_ = command.Process.Kill()
	_ = command.Wait()
	logger.Info("terminal stopped", zap.String("username", identity.Username))
}

// loginShell returns the account's shell from the passwd database, falling
// back to /bin/sh when the entry is missing or not an absolute path.
func loginShell(account *user.User) string {
	output, err := exec.Command("getent", "passwd", account.Uid).Output()
	if err == nil {
		fields := strings.Split(strings.TrimSpace(string(output)), ":")
		if len(fields) == 7 && strings.HasPrefix(fields[6], "/") {
			return fields[6]
		}
	}
	return "/bin/sh"
}
