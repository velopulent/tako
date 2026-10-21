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
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

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
	bridge       io.Closer
	// adminBridge serves this grant's administrative work that needs the
	// real host; it starts on first use and stops with the grant.
	adminBridge privilegedBridge
	adminTimer  *time.Timer
	active      bool
	closePAM    func()
	timer       *time.Timer
}

type grantStore struct {
	mu       sync.Mutex
	notifyMu sync.Mutex
	// adminStartMu serializes administrative bridge starts so concurrent
	// requests share one bridge.
	adminStartMu sync.Mutex
	values       map[string]bridgeGrant
	activity     *idle.Tracker
	// lifetime bounds a confirmed login; it matches the gateway's
	// session.lifetime. Zero uses defaultGrantLifetime.
	lifetime time.Duration
}

const defaultGrantLifetime = 12 * time.Hour

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

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	service := auth.PAMAuthenticator{Service: "tako"}
	grants := &grantStore{values: make(map[string]bridgeGrant), activity: activity, lifetime: runtimeConfig.SessionLifetime}
	hostRuntime := newHostRuntime(runtimeConfig.MonitoringInterval, runtimeConfig.HistoryRetention)
	hostRuntime.updates = updates
	go hostRuntime.run(ctx)
	defer hostRuntime.close()
	policy := newAdministrativePolicy()
	opener := helperOpener{executable: executable, start: startTransientService}
	conversations := newConversationStore(opener, func(session auth.UserSession) (string, error) {
		return grants.addUserSession(session)
	})
	conversations.activity = activity
	defer conversations.closeAll()
	requests := newHandler(service, conversations, grants, policy, logger, hostRuntime)
	requests.certificate, requests.certificateKey = runtimeConfig.Certificate, runtimeConfig.CertificateKey
	requests.privileged = newPrivilegedBridgeStarter(startTransientService, executable)
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
	previous := grant.adminBridge
	if grant.adminTimer != nil {
		grant.adminTimer.Stop()
	}
	grant.adminToken = adminToken
	grant.adminExpires = until
	grant.adminBridge = nil
	grant.adminTimer = time.AfterFunc(duration, func() { store.endAdministrative(adminToken) })
	store.values[token] = grant
	store.mu.Unlock()
	closePrivilegedBridge(previous)
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
	return store.endAdministrative(token)
}

// endAdministrative drops an administrative grant, on revocation or expiry,
// and stops its administrative bridge.
func (store *grantStore) endAdministrative(token string) bool {
	if token == "" {
		return false
	}
	store.mu.Lock()
	for key, grant := range store.values {
		if grant.adminToken == token {
			bridge := grant.adminBridge
			if grant.adminTimer != nil {
				grant.adminTimer.Stop()
			}
			grant.adminToken, grant.adminExpires, grant.adminBridge, grant.adminTimer = "", time.Time{}, nil, nil
			store.values[key] = grant
			store.mu.Unlock()
			closePrivilegedBridge(bridge)
			return true
		}
	}
	store.mu.Unlock()
	return false
}

// administrativeBridge returns the administrative bridge for adminToken,
// starting it on first use.
func (store *grantStore) administrativeBridge(ctx context.Context, adminToken string, start privilegedBridgeStarter) (bridgeCaller, error) {
	if start == nil {
		return nil, auth.Code("system-backend-unavailable")
	}
	store.adminStartMu.Lock()
	defer store.adminStartMu.Unlock()
	lookup := func() (string, bridgeGrant, bool) {
		now := time.Now()
		for key, grant := range store.values {
			if adminToken != "" && grant.adminToken == adminToken && now.Before(grant.adminExpires) && now.Before(grant.expires) {
				return key, grant, true
			}
		}
		return "", bridgeGrant{}, false
	}
	store.mu.Lock()
	_, grant, ok := lookup()
	if !ok {
		store.mu.Unlock()
		return nil, auth.Code("invalid-admin-token")
	}
	if grant.adminBridge != nil && grant.adminBridge.alive() {
		store.mu.Unlock()
		return grant.adminBridge, nil
	}
	stale := grant.adminBridge
	store.mu.Unlock()
	closePrivilegedBridge(stale)
	bridge, err := start(ctx)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	key, grant, ok := lookup()
	if !ok {
		store.mu.Unlock()
		closePrivilegedBridge(bridge)
		return nil, auth.Code("invalid-admin-token")
	}
	grant.adminBridge = bridge
	store.values[key] = grant
	store.mu.Unlock()
	return bridge, nil
}

// claim reserves the session's single terminal.
func (store *grantStore) claim(token string) (auth.Identity, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || grant.active || time.Now().After(grant.expires) {
		store.mu.Unlock()
		return auth.Identity{}, false
	}
	grant.active = true
	store.values[token] = grant
	store.mu.Unlock()
	return grant.identity, true
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
	if grant.adminTimer != nil {
		grant.adminTimer.Stop()
	}
	closePrivilegedBridge(grant.adminBridge)
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
	lifetime := store.lifetime
	if lifetime <= 0 {
		lifetime = defaultGrantLifetime
	}
	grant.expires = time.Now().Add(lifetime)
	store.values[token] = grant
	store.mu.Unlock()
	return true
}

// addUserSession registers a session the sign-in helper opened. The grant
// closes when the helper ends the session on its own.
func (store *grantStore) addUserSession(session auth.UserSession) (string, error) {
	if session.Bridge == nil {
		if session.Close != nil {
			session.Close()
		}
		return "", errors.New("the session has no user bridge")
	}
	token, err := store.addGrant(session.Identity, session.Environment, session.Bridge, session.Close)
	if err != nil {
		return "", err
	}
	if session.Done != nil {
		go func() {
			<-session.Done
			store.close(token)
		}()
	}
	return token, nil
}

func (store *grantStore) release(token string) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if ok {
		grant.active = false
		store.values[token] = grant
	}
	store.mu.Unlock()
}
