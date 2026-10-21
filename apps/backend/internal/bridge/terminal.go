package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/fdpass"
	"go.uber.org/zap"
)

// RunSession serves a signed-in user's bridge. Besides RPC on input and
// output, it starts a login shell for every socket sessiond hands over on
// terminals. The bridge already runs as the user inside their login session,
// so shells get the user's cgroup, SELinux context, and audit identity.
// It returns when input closes or ctx ends, hanging up every shell.
func RunSession(ctx context.Context, input io.Reader, output io.Writer, errorOutput io.Writer, terminals *net.UnixConn) error {
	logger, err := newLogger(errorOutput)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()
	shells := &terminalSet{logger: logger, shell: loginShell}
	defer shells.closeAll()
	if terminals != nil {
		defer terminals.Close()
		go shells.accept(terminals)
	}
	served := make(chan error, 1)
	go func() {
		served <- serve(logger, input, output, func(ctx context.Context, message frame) frame {
			if message.Method == "terminal.resize" {
				return shells.resizeFrame(message)
			}
			return dispatch(ctx, message)
		})
	}()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
		logger.Info("bridge stopping")
		return nil
	}
}

// terminalSet runs the session's login shells.
type terminalSet struct {
	mu     sync.Mutex
	active map[*terminal]struct{}
	latest *terminal
	logger *zap.Logger
	shell  func() string
}

type terminal struct {
	pty     *os.File
	command *exec.Cmd
	once    sync.Once
}

// accept starts a shell for each terminal socket received on control.
func (set *terminalSet) accept(control *net.UnixConn) {
	for {
		header, files, err := fdpass.Receive(control, 4096)
		if err != nil {
			return
		}
		if len(files) != 1 {
			fdpass.CloseAll(files)
			continue
		}
		var size auth.TerminalSize
		_ = json.Unmarshal(header, &size)
		conn, err := fdpass.Conn(files[0])
		if err != nil {
			continue
		}
		go set.run(conn, size)
	}
}

func (set *terminalSet) run(conn net.Conn, size auth.TerminalSize) {
	defer conn.Close()
	shell := set.shell()
	command := exec.Command(shell, "-l")
	if home, err := os.UserHomeDir(); err == nil {
		command.Dir = home
	}
	command.Env = append(terminalEnvironment(os.Environ()), "SHELL="+shell, "TERM=xterm-256color", "COLORTERM=truecolor")
	if size.Columns == 0 || size.Rows == 0 {
		size = auth.TerminalSize{Columns: 120, Rows: 30}
	}
	device, err := pty.StartWithSize(command, &pty.Winsize{Cols: size.Columns, Rows: size.Rows})
	if err != nil {
		set.logger.Error("terminal launch failed", zap.String("shell", shell), zap.Error(err))
		_, _ = fmt.Fprintf(conn, "\r\nTako could not start your shell (%s).\r\n", shell)
		return
	}
	current := &terminal{pty: device, command: command}
	set.add(current)
	defer set.remove(current)
	set.logger.Info("terminal started", zap.String("shell", shell))
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(device, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, device); done <- struct{}{} }()
	<-done
	current.stop()
	set.logger.Info("terminal stopped")
}

// stop hangs up the shell's process group, as closing a terminal would.
func (current *terminal) stop() {
	current.once.Do(func() {
		_ = current.pty.Close()
		if current.command.Process != nil {
			_ = syscall.Kill(-current.command.Process.Pid, syscall.SIGHUP)
			_ = current.command.Process.Kill()
		}
		_ = current.command.Wait()
	})
}

func (set *terminalSet) add(current *terminal) {
	set.mu.Lock()
	defer set.mu.Unlock()
	if set.active == nil {
		set.active = make(map[*terminal]struct{})
	}
	set.active[current] = struct{}{}
	set.latest = current
}

func (set *terminalSet) remove(current *terminal) {
	set.mu.Lock()
	defer set.mu.Unlock()
	delete(set.active, current)
	if set.latest == current {
		set.latest = nil
	}
}

func (set *terminalSet) closeAll() {
	set.mu.Lock()
	active := make([]*terminal, 0, len(set.active))
	for current := range set.active {
		active = append(active, current)
	}
	set.mu.Unlock()
	for _, current := range active {
		current.stop()
	}
}

func (set *terminalSet) resize(size auth.TerminalSize) error {
	if size.Columns == 0 || size.Rows == 0 {
		return errInvalidTerminalSize
	}
	set.mu.Lock()
	current := set.latest
	set.mu.Unlock()
	if current == nil {
		return errTerminalNotReady
	}
	return pty.Setsize(current.pty, &pty.Winsize{Cols: size.Columns, Rows: size.Rows})
}

var (
	errInvalidTerminalSize = errors.New("invalid terminal size")
	errTerminalNotReady    = errors.New("terminal not ready")
)

func (set *terminalSet) resizeFrame(message frame) frame {
	var size auth.TerminalSize
	if err := decodeHostPayload(message.Payload, &size); err != nil {
		return frame{ID: message.ID, Error: "invalid-terminal-size"}
	}
	switch err := set.resize(size); {
	case err == nil:
		return frame{ID: message.ID, Payload: json.RawMessage(`{"ok":true}`)}
	case errors.Is(err, errInvalidTerminalSize):
		return frame{ID: message.ID, Error: "invalid-terminal-size"}
	case errors.Is(err, errTerminalNotReady):
		return frame{ID: message.ID, Error: "terminal-not-ready"}
	default:
		return frame{ID: message.ID, Error: "terminal-resize-failed"}
	}
}

// terminalEnvironment drops Tako's own plumbing variables from what shells
// inherit.
func terminalEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, variable := range environment {
		if strings.HasPrefix(variable, "TAKO_") {
			continue
		}
		result = append(result, variable)
	}
	return result
}

// loginShell returns the account's shell from the passwd database, falling
// back to /bin/sh when the entry is missing or not an absolute path.
func loginShell() string {
	account, err := user.Current()
	if err != nil {
		return "/bin/sh"
	}
	output, err := exec.Command("getent", "passwd", account.Uid).Output()
	if err == nil {
		fields := strings.Split(strings.TrimSpace(string(output)), ":")
		if len(fields) == 7 && strings.HasPrefix(fields[6], "/") {
			return fields[6]
		}
	}
	return "/bin/sh"
}
