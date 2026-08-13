// Package sessiond implements Tako's narrow, socket-activated PAM boundary.
// Production packaging runs it as root with systemd hardening; it never listens
// on a network socket and never retains credentials.
package sessiond

import (
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
	"go.uber.org/zap"
)

type bridgeGrant struct {
	identity auth.Identity
	expires  time.Time
	terminal *os.File
	active   bool
	closePAM func()
}

type grantStore struct {
	mu     sync.Mutex
	values map[string]bridgeGrant
}

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
		go handle(conn, service, grants, logger)
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

func handle(conn net.Conn, service auth.PAMAuthenticator, grants *grantStore, logger *zap.Logger) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	decoder := json.NewDecoder(io.LimitReader(conn, 16<<10))
	encoder := json.NewEncoder(conn)
	var request auth.Request
	if err := decoder.Decode(&request); err != nil {
		logger.Warn("session request rejected", zap.String("reason", "invalid-request"), zap.Error(err))
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return
	}
	if request.Operation == "terminal" {
		_ = conn.SetDeadline(time.Time{})
		identity, ok := grants.claim(request.Token)
		if !ok {
			logger.Warn("terminal request rejected", zap.String("reason", "invalid-bridge-token"))
			_ = encoder.Encode(auth.Response{Error: "invalid-bridge-token"})
			return
		}
		logger.Info("terminal starting", zap.String("username", identity.Username), zap.Int("uid", identity.UID))
		runTerminal(conn, identity, request.Columns, request.Rows, grants, request.Token, logger)
		return
	}
	if request.Operation == "resize-terminal" {
		responseError := grants.resize(request.Token, request.Columns, request.Rows)
		if responseError != "" {
			logger.Warn("terminal resize rejected", zap.String("reason", responseError))
		}
		_ = encoder.Encode(auth.Response{Error: responseError})
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

func (store *grantStore) resize(token string, columns, rows uint16) string {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		delete(store.values, token)
		store.mu.Unlock()
		if grant.closePAM != nil {
			grant.closePAM()
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

func (store *grantStore) setTerminal(token string, terminal *os.File) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if ok {
		grant.terminal = terminal
		store.values[token] = grant
	}
	store.mu.Unlock()
}

func (store *grantStore) add(identity auth.Identity, closePAM ...func()) (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	store.mu.Lock()
	var closeSession func()
	if len(closePAM) > 0 {
		closeSession = closePAM[0]
	}
	store.values[token] = bridgeGrant{identity: identity, expires: time.Now().Add(12 * time.Hour), closePAM: closeSession}
	store.mu.Unlock()
	return token, nil
}

func (store *grantStore) get(token string) (auth.Identity, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || time.Now().After(grant.expires) {
		delete(store.values, token)
		store.mu.Unlock()
		if grant.closePAM != nil {
			grant.closePAM()
		}
		return auth.Identity{}, false
	}
	store.mu.Unlock()
	return grant.identity, true
}

func (store *grantStore) claim(token string) (auth.Identity, bool) {
	store.mu.Lock()
	grant, ok := store.values[token]
	if !ok || grant.active || time.Now().After(grant.expires) {
		if ok && time.Now().After(grant.expires) {
			delete(store.values, token)
		}
		store.mu.Unlock()
		if ok && time.Now().After(grant.expires) && grant.closePAM != nil {
			grant.closePAM()
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
		if grant.terminal != nil {
			_ = grant.terminal.Close()
		}
		if grant.closePAM != nil {
			grant.closePAM()
		}
	}
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

func runTerminal(conn net.Conn, identity auth.Identity, columns, rows uint16, grants *grantStore, token string, logger *zap.Logger) {
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
	grants.setTerminal(token, terminal)
	defer terminal.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(terminal, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, terminal); done <- struct{}{} }()
	<-done
	_ = terminal.Close()
	_ = command.Process.Kill()
	_ = command.Wait()
	logger.Info("terminal stopped", zap.String("username", identity.Username))
}
