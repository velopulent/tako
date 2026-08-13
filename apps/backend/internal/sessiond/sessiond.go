// Package sessiond implements Tako's narrow, socket-activated PAM boundary.
// Production packaging runs it as root with systemd hardening; it never listens
// on a network socket and never retains credentials.
package sessiond

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

type bridgeGrant struct {
	identity     auth.Identity
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
	mu     sync.Mutex
	values map[string]bridgeGrant
}

type administrativePolicy func(context.Context, auth.Identity, string) error

var errAdministrativeUnavailable = errors.New("administrative policy unavailable")

// Run starts the privileged local session service. The caller selects this
// process mode explicitly; it never shares a process with the web gateway.
func Run(args []string) error {
	flags := flag.NewFlagSet("sessiond", flag.ContinueOnError)
	socket := flags.String("socket", "/run/tako/session.sock", "Unix socket path")
	if err := flags.Parse(args); err != nil {
		return err
	}

	listener, activated, err := activatedListener()
	if err != nil {
		return err
	}
	if !activated {
		if err := os.MkdirAll("/run/tako", 0o750); err != nil {
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	service := auth.PAMAuthenticator{Service: "tako"}
	grants := &grantStore{values: make(map[string]bridgeGrant)}
	policy := newAdministrativePolicy()
	conversations := newConversationStore(service, func(session auth.UserSession) (string, error) {
		return grants.addUserSession(session)
	})
	defer conversations.closeAll()
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
		go handle(conn, service, conversations, grants, policy, logger)
	}
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

func handle(conn net.Conn, service auth.PAMAuthenticator, conversations *conversationStore, grants *grantStore, policy administrativePolicy, logger *zap.Logger, hostBackends ...hostConfigBackend) {
	defer conn.Close()
	backend := hostConfigBackend(systemHostConfigBackend{})
	if len(hostBackends) > 0 && hostBackends[0] != nil {
		backend = hostBackends[0]
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReaderSize(conn, 16<<10)
	encoder := json.NewEncoder(conn)
	var request auth.Request
	if err := decodeRequestLine(reader, &request); err != nil {
		logger.Warn("session request rejected", zap.String("reason", "invalid-request"), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation != "host-config" && hasHostConfigurationFields(request) {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation == "conversation" {
		if request.Token != "" || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-conversation"})
			return
		}
		conversationRequest := auth.ConversationRequest{
			ConversationID: request.ConversationID,
			Username:       request.Username,
			Password:       request.Password,
			Responses:      request.Responses,
		}
		if !conversationRequest.Valid() || conversationRequest.Cancel {
			_ = encoder.Encode(auth.Response{Error: "invalid-conversation"})
			return
		}
		_ = conn.SetDeadline(time.Now().Add(time.Minute))
		conversationCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		response, err := conversations.advance(conversationCtx, conversationRequest)
		request.Password = ""
		conversationRequest.Password = ""
		for index := range request.Responses {
			request.Responses[index].Value = ""
		}
		for index := range conversationRequest.Responses {
			conversationRequest.Responses[index].Value = ""
		}
		if err != nil {
			code := "authentication-failed"
			if errors.Is(err, errConversationBusy) {
				code = "authentication-busy"
			}
			if errors.Is(err, errConversationInvalid) || errors.Is(err, errConversationBounds) {
				code = "invalid-conversation"
			}
			_ = encoder.Encode(auth.Response{Error: code})
			return
		}
		if err := encoder.Encode(auth.Response{
			Identity:       response.Identity,
			BridgeToken:    response.BridgeToken,
			ConversationID: response.ConversationID,
			Prompts:        response.Prompts,
		}); err != nil && response.ConversationID != "" {
			conversations.cancel(response.ConversationID)
		}
		return
	}
	if request.Operation == "cancel-conversation" {
		if request.Username != "" || request.Password != "" || len(request.Responses) != 0 || request.Token != "" || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-conversation"})
			return
		}
		if !(auth.ConversationRequest{ConversationID: request.ConversationID, Cancel: true}).Valid() {
			_ = encoder.Encode(auth.Response{Error: "invalid-conversation"})
			return
		}
		conversations.cancel(request.ConversationID)
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "authorize-admin" {
		if request.Token == "" || request.AdminToken != "" || request.Username != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.AdminTTL > 3600 || len(request.Password) > 4096 {
			_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
			return
		}
		if policy == nil {
			request.Password = ""
			_ = encoder.Encode(auth.Response{Error: "administrative-policy-unavailable"})
			return
		}
		adminCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		adminToken, until, err := grants.authorize(adminCtx, request.Token, request.Password, request.AdminTTL, policy)
		cancel()
		request.Password = ""
		if err != nil {
			code := "administrative-access-denied"
			if errors.Is(err, errAdministrativeUnavailable) {
				code = "administrative-policy-unavailable"
			}
			_ = encoder.Encode(auth.Response{Error: code})
			return
		}
		_ = encoder.Encode(auth.Response{AdminToken: adminToken, AdminUntil: until})
		return
	}
	if request.Operation == "revoke-admin" {
		if request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.AdminTTL != 0 {
			_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
			return
		}
		if !grants.revokeAdmin(request.AdminToken) {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "close-session" {
		if request.Token == "" || request.AdminToken != "" || !grants.close(request.Token) {
			_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
			return
		}
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "confirm-session" {
		if request.Token == "" || request.AdminToken != "" || !grants.confirm(request.Token) {
			_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
			return
		}
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation == "terminal" {
		if request.Token == "" || request.AdminToken != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-terminal-request"})
			return
		}
		_ = conn.SetDeadline(time.Time{})
		identity, ok := grants.claim(request.Token)
		if !ok {
			logger.Warn("terminal request rejected", zap.String("reason", "invalid-bridge-token"))
			_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
			return
		}
		logger.Info("terminal starting", zap.String("username", identity.Username), zap.Int("uid", identity.UID))
		runTerminal(conn, reader, identity, request.Columns, request.Rows, grants, request.Token, logger)
		return
	}
	if request.Operation == "resize-terminal" {
		if request.Token == "" || request.AdminToken != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-terminal-request"})
			return
		}
		responseError := grants.resize(request.Token, request.Columns, request.Rows)
		if responseError != "" {
			logger.Warn("terminal resize rejected", zap.String("reason", responseError))
		}
		_ = encoder.Encode(auth.Response{Error: responseError})
		return
	}
	if request.Operation == "service-action" {
		if request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 {
			_ = encoder.Encode(auth.Response{Error: "invalid-service-request"})
			return
		}
		var identity auth.Identity
		var ok bool
		if request.Scope == "system" {
			if request.Token != "" || request.AdminToken == "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
				return
			}
			identity, ok = grants.adminIdentity(request.AdminToken)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
				return
			}
		} else {
			if request.Token == "" || request.AdminToken != "" {
				_ = encoder.Encode(auth.Response{Error: "invalid-service-request"})
				return
			}
			identity, ok = grants.get(request.Token)
			if !ok {
				_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
				return
			}
		}
		operation, operationErr := parseServiceOperation(request.Scope, request.Unit, request.Action)
		if operationErr != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-service-operation"})
			return
		}
		errorCode := runServiceAction(operation)
		logger.Info("service action", zap.String("username", identity.Username), zap.String("scope", request.Scope), zap.String("unit", request.Unit), zap.String("action", request.Action), zap.String("result", errorCode))
		_ = encoder.Encode(auth.Response{Error: errorCode})
		return
	}
	if request.Operation == "host-config" {
		if request.AdminToken == "" || request.Token != "" || request.Username != "" || request.Password != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" {
			_ = encoder.Encode(auth.Response{Error: "invalid-administrative-request"})
			return
		}
		operation, operationErr := parseHostConfigurationOperation(request.Hostname, request.Timezone, request.NTPEnabled, request.ExpectedFingerprint)
		if operationErr != nil {
			_ = encoder.Encode(auth.Response{Error: "invalid-host-configuration"})
			return
		}
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			_ = encoder.Encode(auth.Response{Error: "invalid-admin-token"})
			return
		}
		configurationCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		current, readErr := backend.Read(configurationCtx)
		if readErr != nil {
			cancel()
			_ = encoder.Encode(auth.Response{Error: "host-config-unavailable"})
			return
		}
		if current.Fingerprint != operation.ExpectedFingerprint {
			cancel()
			_ = encoder.Encode(auth.Response{Error: "host-config-conflict"})
			return
		}
		desired := platform.NewHostConfiguration(operation.Hostname, operation.Timezone, operation.NTPEnabled)
		applyErr := backend.Apply(configurationCtx, current, desired)
		cancel()
		if applyErr != nil {
			_ = encoder.Encode(auth.Response{Error: "host-config-failed"})
			return
		}
		logger.Info("host configuration changed", zap.String("username", identity.Username))
		_ = encoder.Encode(auth.Response{})
		return
	}
	if request.Operation != "authenticate" || request.Token != "" || request.AdminToken != "" || request.ConversationID != "" || len(request.Responses) != 0 || request.Columns != 0 || request.Rows != 0 || request.Action != "" || request.Unit != "" || request.Scope != "" || request.Username == "" || request.Password == "" {
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	identity, closePAM, err := service.OpenSession(request.Username, request.Password)
	request.Password = ""
	if err != nil {
		logger.Warn("PAM authentication failed", zap.String("username", request.Username), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: "authentication-failed"})
		return
	}
	token, err := grants.add(identity, closePAM)
	if err != nil {
		closePAM()
		logger.Error("bridge grant creation failed", zap.String("username", identity.Username), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: "session-failed"})
		return
	}
	logger.Info("PAM authentication succeeded", zap.String("username", identity.Username), zap.Int("uid", identity.UID))
	_ = encoder.Encode(auth.Response{Identity: &identity, BridgeToken: token})
}

func hasHostConfigurationFields(request auth.Request) bool {
	return request.Hostname != "" || request.Timezone != "" || request.NTPEnabled || request.ExpectedFingerprint != ""
}

func runServiceAction(operation serviceOperation) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	arguments := []string{operation.Action, "--", operation.Unit}
	if operation.Scope == "user" {
		arguments = append([]string{"--user"}, arguments...)
	}
	if err := exec.CommandContext(ctx, "systemctl", arguments...).Run(); err != nil {
		if ctx.Err() != nil {
			return "service-job-timeout"
		}
		return "service-action-failed"
	}
	return ""
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

func (store *grantStore) add(identity auth.Identity, closePAM ...func()) (string, error) {
	var closeSession func()
	if len(closePAM) > 0 {
		closeSession = closePAM[0]
	}
	return store.addGrant(identity, nil, closeSession)
}

func (store *grantStore) addGrant(identity auth.Identity, bridge io.Closer, closeSession func()) (string, error) {
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
	grant := bridgeGrant{identity: identity, expires: time.Now().Add(30 * time.Second), bridge: bridge, closePAM: closeResources}
	grant.timer = time.AfterFunc(30*time.Second, func() { store.expire(token) })
	store.values[token] = grant
	store.mu.Unlock()
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
			cleanupGrant(grant)
		}
		return auth.Identity{}, false
	}
	store.mu.Unlock()
	return grant.identity, true
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

func (store *grantStore) claim(token string) (auth.Identity, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || grant.active || time.Now().After(grant.expires) {
		expired := ok && time.Now().After(grant.expires)
		if expired {
			delete(store.values, token)
		}
		store.mu.Unlock()
		if expired {
			cleanupGrant(grant)
		}
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
	cleanupGrant(grant)
	return true
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
	token, err := store.addGrant(session.Identity, userBridge, session.Close)
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

func decodeRequestLine(reader *bufio.Reader, request *auth.Request) error {
	const maxRequestBytes = 16 << 10
	var payload []byte
	for {
		part, err := reader.ReadSlice('\n')
		payload = append(payload, part...)
		if len(payload) > maxRequestBytes {
			return errors.New("request exceeds limit")
		}
		if err == nil {
			break
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(request); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing request data")
	}
	return nil
}

func runTerminal(conn net.Conn, reader io.Reader, identity auth.Identity, columns, rows uint16, grants *grantStore, token string, logger *zap.Logger) {
	defer grants.release(token)
	account, err := user.LookupId(strconv.Itoa(identity.UID))
	if err != nil {
		logger.Error("terminal user lookup failed", zap.Int("uid", identity.UID), zap.Error(err))
		return
	}
	shell := "/bin/sh"
	if payload, readErr := os.ReadFile("/etc/passwd"); readErr == nil {
		for _, line := range strings.Split(string(payload), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) == 7 && fields[2] == strconv.Itoa(identity.UID) {
				shell = fields[6]
				break
			}
		}
	}
	command := exec.Command(shell, "-l")
	command.Dir = account.HomeDir
	command.Env = []string{"HOME=" + account.HomeDir, "USER=" + account.Username, "LOGNAME=" + account.Username, "SHELL=" + shell, "TERM=xterm-256color", "PATH=/usr/local/sbin:/usr/local/bin:/usr/bin:/bin"}
	credential := &syscall.Credential{Uid: uint32(identity.UID), Gid: uint32(identity.GID)}
	if groupIDs, groupErr := account.GroupIds(); groupErr == nil {
		for _, groupID := range groupIDs {
			parsed, parseErr := strconv.ParseUint(groupID, 10, 32)
			if parseErr == nil {
				credential.Groups = append(credential.Groups, uint32(parsed))
			}
		}
	}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: credential, Setsid: true}
	if columns == 0 {
		columns = 120
	}
	if rows == 0 {
		rows = 30
	}
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: columns, Rows: rows})
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
	_ = command.Process.Kill()
	_ = command.Wait()
	logger.Info("terminal stopped", zap.String("username", identity.Username))
}
