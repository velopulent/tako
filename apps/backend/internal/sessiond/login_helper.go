package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/fdpass"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

// Each sign-in runs in its own transient service, `tako session-helper`.
// The helper owns the PAM transaction: it authenticates, opens the session,
// and starts the user's bridge, which hosts their terminals. Running it
// outside sessiond matters for three reasons:
//
//   - pam_systemd registers a real logind session for the helper's process,
//     and the bridge and shells inherit its scope;
//   - pam_loginuid and pam_selinux change the calling process, so they must
//     not run inside the long-lived sessiond, and the user's processes get
//     their own audit identity and SELinux context;
//   - the user's processes see the host's filesystem instead of sessiond's
//     sandboxed mount namespace.
//
// Sessiond and the helper talk over a SOCK_SEQPACKET control socket, one
// JSON message per packet. Once the session is open the helper hands sessiond
// the bridge's sockets and waits; closing the control socket ends the session.

const (
	helperMessageLimit = 64 << 10
	// helperSignInLimit bounds an unfinished sign-in in case sessiond never
	// closes the control socket.
	helperSignInLimit = 3 * time.Minute
	// terminalFDVariable names the bridge's descriptor for terminal sockets.
	terminalFDVariable = "TAKO_TERMINAL_FD"
)

type helperMessage struct {
	Type       string           `json:"type"`
	Username   string           `json:"username,omitempty"`
	RemoteHost string           `json:"remoteHost,omitempty"`
	Style      auth.PromptStyle `json:"style,omitempty"`
	Message    string           `json:"message,omitempty"`
	Value      string           `json:"value,omitempty"`
	Identity   *auth.Identity   `json:"identity,omitempty"`
	Error      string           `json:"error,omitempty"`
}

func sendHelperMessage(conn *net.UnixConn, message helperMessage, files ...*os.File) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	defer clear(payload)
	return fdpass.Send(conn, payload, files...)
}

func receiveHelperMessage(conn *net.UnixConn) (helperMessage, []*os.File, error) {
	payload, files, err := fdpass.Receive(conn, helperMessageLimit)
	if err != nil {
		return helperMessage{}, nil, err
	}
	defer clear(payload)
	var message helperMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		fdpass.CloseAll(files)
		return helperMessage{}, nil, err
	}
	return message, files, nil
}

// RunLoginHelper serves one sign-in over the control socket on stdin. It is
// started by sessiond, never by hand.
func RunLoginHelper() error {
	// PAM modules change the calling thread (loginuid, SELinux exec context,
	// session keyring), and the bridge must be forked from that same thread
	// to inherit them. main.go already locked this goroutine to the main
	// thread in init, which pam_loginuid needs; locking again is a no-op that
	// keeps the requirement visible here.
	runtime.LockOSThread()
	if os.Geteuid() != 0 {
		return errors.New("the sign-in helper must run as root")
	}
	control, err := fdpass.Conn(os.NewFile(0, "sign-in control"))
	if err != nil {
		return err
	}
	defer control.Close()
	// systemd passed the control socket as stdout too. Nothing may write the
	// protocol by accident, so stdout becomes /dev/null.
	if null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		_ = unix.Dup2(int(null.Fd()), 1)
		_ = null.Close()
	}
	logger := zap.L().Named("sign-in")
	start, files, err := receiveHelperMessage(control)
	fdpass.CloseAll(files)
	if err != nil || start.Type != "start" || start.Username == "" || len(start.Username) > 256 {
		return errors.New("invalid sign-in request")
	}
	abandon := time.AfterFunc(helperSignInLimit, func() { _ = control.Close() })
	session, err := auth.PAMAuthenticator{Service: "tako"}.OpenSessionWithConversation(start.Username, start.RemoteHost, func(style auth.PromptStyle, prompt string) (string, error) {
		if err := sendHelperMessage(control, helperMessage{Type: "prompt", Style: style, Message: prompt}); err != nil {
			return "", err
		}
		reply, files, err := receiveHelperMessage(control)
		fdpass.CloseAll(files)
		if err != nil {
			return "", err
		}
		if reply.Type != "answer" {
			return "", errors.New("unexpected sign-in message")
		}
		return reply.Value, nil
	})
	abandon.Stop()
	if err != nil {
		logger.Warn("sign-in failed", zap.String("username", start.Username), zap.String("remote_host", start.RemoteHost), zap.Error(err))
		_ = sendHelperMessage(control, helperMessage{Type: "failed", Error: "authentication-failed"})
		return nil
	}
	defer session.Close()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	bridge, rpc, terminals, err := startSessionBridge(session, executable)
	if err != nil {
		logger.Error("user bridge failed to start", zap.String("username", session.Identity.Username), zap.Error(err))
		_ = sendHelperMessage(control, helperMessage{Type: "failed", Error: "session-failed"})
		return nil
	}
	identity := session.Identity
	err = sendHelperMessage(control, helperMessage{Type: "opened", Identity: &identity}, rpc, terminals)
	_ = rpc.Close()
	_ = terminals.Close()
	if err != nil {
		bridge.stop()
		return err
	}
	logger.Info("session opened", zap.String("username", identity.Username), zap.Int("uid", identity.UID), zap.String("remote_host", start.RemoteHost))

	ended := make(chan string, 3)
	go func() {
		for {
			_, files, err := receiveHelperMessage(control)
			fdpass.CloseAll(files)
			if err != nil {
				ended <- "signed out"
				return
			}
		}
	}()
	go func() {
		<-bridge.exited
		ended <- "user bridge exited"
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		<-signals
		ended <- "stopped"
	}()
	reason := <-ended
	bridge.stop()
	logger.Info("session closed", zap.String("username", identity.Username), zap.String("reason", reason))
	return nil
}

// sessionBridge is the user's bridge process as the helper sees it.
type sessionBridge struct {
	command *exec.Cmd
	exited  chan struct{}
	once    sync.Once
}

// stop asks the bridge to close its terminals and exit, then kills it.
func (bridge *sessionBridge) stop() {
	bridge.once.Do(func() {
		if bridge.command.Process == nil {
			return
		}
		_ = bridge.command.Process.Signal(syscall.SIGTERM)
		select {
		case <-bridge.exited:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-bridge.command.Process.Pid, syscall.SIGKILL)
			<-bridge.exited
		}
	})
}

// startSessionBridge starts `executable bridge` as the session's user. It
// returns sessiond's ends of the RPC socket and the terminal handoff socket.
func startSessionBridge(session auth.UserSession, executable string) (*sessionBridge, *os.File, *os.File, error) {
	account, err := user.LookupId(strconv.Itoa(session.Identity.UID))
	if err != nil {
		return nil, nil, nil, err
	}
	credential, err := userCredential(account, session.Identity)
	if err != nil {
		return nil, nil, nil, err
	}
	rpc, rpcChild, err := fdpass.Pair(unix.SOCK_STREAM)
	if err != nil {
		return nil, nil, nil, err
	}
	terminals, terminalsChild, err := fdpass.Pair(unix.SOCK_SEQPACKET)
	if err != nil {
		rpc.Close()
		rpcChild.Close()
		return nil, nil, nil, err
	}
	defer rpcChild.Close()
	defer terminalsChild.Close()
	command := exec.Command(executable, "bridge")
	command.Stdin, command.Stdout, command.Stderr = rpcChild, rpcChild, os.Stderr
	command.ExtraFiles = []*os.File{terminalsChild}
	command.Dir = account.HomeDir
	command.Env = append(bridgeEnvironment(session.Environment, account), terminalFDVariable+"=3")
	if level := os.Getenv("TAKO_LOG_LEVEL"); level != "" {
		command.Env = append(command.Env, "TAKO_LOG_LEVEL="+level)
	}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: credential, Setsid: true}
	if err := command.Start(); err != nil {
		rpc.Close()
		terminals.Close()
		return nil, nil, nil, fmt.Errorf("start user bridge %s uid=%d gid=%d: %w", executable, session.Identity.UID, session.Identity.GID, err)
	}
	bridge := &sessionBridge{command: command, exited: make(chan struct{})}
	go func() {
		_ = command.Wait()
		close(bridge.exited)
	}()
	return bridge, rpc, terminals, nil
}

// helperOpener signs a user in through a per-login helper service.
type helperOpener struct {
	executable string
	start      serviceStarter
}

func (opener helperOpener) OpenSessionWithConversation(ctx context.Context, username, remoteHost string, conversation auth.Conversation) (auth.UserSession, error) {
	local, peer, err := fdpass.Pair(unix.SOCK_SEQPACKET)
	if err != nil {
		return auth.UserSession{}, err
	}
	control, err := fdpass.Conn(local)
	if err != nil {
		peer.Close()
		return auth.UserSession{}, err
	}
	name, err := randomToken(12)
	if err != nil {
		peer.Close()
		control.Close()
		return auth.UserSession{}, err
	}
	startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = opener.start(startCtx, "tako-login-"+name+".service", "Tako sign-in", []string{opener.executable, "session-helper"}, peer)
	cancel()
	peer.Close()
	if err != nil {
		control.Close()
		return auth.UserSession{}, fmt.Errorf("start sign-in helper: %w", err)
	}
	abandon := context.AfterFunc(ctx, func() { _ = control.Close() })
	fail := func(err error) (auth.UserSession, error) {
		abandon()
		_ = control.Close()
		if ctx.Err() != nil {
			return auth.UserSession{}, ctx.Err()
		}
		return auth.UserSession{}, err
	}
	if err := sendHelperMessage(control, helperMessage{Type: "start", Username: username, RemoteHost: remoteHost}); err != nil {
		return fail(err)
	}
	for {
		message, files, err := receiveHelperMessage(control)
		if err != nil {
			return fail(fmt.Errorf("sign-in helper stopped: %w", err))
		}
		switch message.Type {
		case "prompt":
			fdpass.CloseAll(files)
			answer, err := conversation(message.Style, message.Message)
			if err != nil {
				return fail(err)
			}
			err = sendHelperMessage(control, helperMessage{Type: "answer", Value: answer})
			answer = ""
			if err != nil {
				return fail(err)
			}
		case "failed":
			fdpass.CloseAll(files)
			if message.Error == "session-failed" {
				return fail(fmt.Errorf("%w: the user bridge did not start", errSessionFailed))
			}
			return fail(auth.ErrAuthenticationFailed)
		case "opened":
			if message.Identity == nil || len(files) != 2 {
				fdpass.CloseAll(files)
				return fail(errors.New("invalid sign-in helper result"))
			}
			if !abandon() {
				fdpass.CloseAll(files)
				return fail(context.Canceled)
			}
			session, err := newHelperSession(control, *message.Identity, files[0], files[1])
			if err != nil {
				_ = control.Close()
				return auth.UserSession{}, fmt.Errorf("%w: %v", errSessionFailed, err)
			}
			return session, nil
		default:
			fdpass.CloseAll(files)
			return fail(errors.New("unexpected sign-in helper message"))
		}
	}
}

// newHelperSession connects to the bridge the helper started. The session
// ends when sessiond closes it or when the helper exits.
func newHelperSession(control *net.UnixConn, identity auth.Identity, rpcFile, terminalsFile *os.File) (auth.UserSession, error) {
	rpc, err := fdpass.Conn(rpcFile)
	if err != nil {
		terminalsFile.Close()
		return auth.UserSession{}, err
	}
	terminals, err := fdpass.Conn(terminalsFile)
	if err != nil {
		rpc.Close()
		return auth.UserSession{}, err
	}
	process := newUserBridgeProcess(rpc, rpc)
	process.terminals = terminals
	if err := process.ready(); err != nil {
		_ = process.Close()
		return auth.UserSession{}, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, files, err := receiveHelperMessage(control)
			fdpass.CloseAll(files)
			if err != nil {
				return
			}
		}
	}()
	var once sync.Once
	closeSession := func() {
		once.Do(func() {
			_ = process.Close()
			_ = control.Close()
		})
	}
	return auth.UserSession{Identity: identity, Close: closeSession, Bridge: process, Done: done}, nil
}
