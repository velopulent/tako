package bridge

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/fdpass"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

func TestTerminalRunsALoginShellOverTheHandedSocket(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skip("no pseudo-terminal support")
	}
	shells := &terminalSet{logger: zap.NewNop(), shell: func() string { return "/bin/sh" }}
	defer shells.closeAll()
	control, controlPeer, err := fdpass.Pair(unix.SOCK_SEQPACKET)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := fdpass.Conn(control)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := fdpass.Conn(controlPeer)
	if err != nil {
		t.Fatal(err)
	}
	go shells.accept(receiver)

	local, remote, err := fdpass.Pair(unix.SOCK_STREAM)
	if err != nil {
		t.Fatal(err)
	}
	header, _ := json.Marshal(auth.TerminalSize{Columns: 100, Rows: 40})
	if err := fdpass.Send(sender, header, remote); err != nil {
		t.Fatal(err)
	}
	remote.Close()
	terminal, err := fdpass.Conn(local)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	_ = terminal.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := terminal.Write([]byte("echo tako-$((20+22)); stty size\n")); err != nil {
		t.Fatal(err)
	}
	if !readUntil(t, terminal, "tako-42") {
		t.Fatal("the shell never ran the command")
	}
	if !readUntil(t, terminal, "40 100") {
		t.Fatal("the shell did not get the requested size")
	}

	if response := shells.resizeFrame(frame{ID: "1", Payload: json.RawMessage(`{"columns":132,"rows":50}`)}); response.Error != "" {
		t.Fatalf("resize failed: %s", response.Error)
	}
	if _, err := terminal.Write([]byte("stty size\n")); err != nil {
		t.Fatal(err)
	}
	if !readUntil(t, terminal, "50 132") {
		t.Fatal("resize did not reach the shell")
	}
	shells.closeAll()
	if _, err := io.ReadAll(terminal); err != nil && !strings.Contains(err.Error(), "reset") {
		t.Fatalf("terminal did not close cleanly: %v", err)
	}
}

func readUntil(t *testing.T, conn net.Conn, marker string) bool {
	t.Helper()
	reader := bufio.NewReader(conn)
	var seen strings.Builder
	for {
		chunk, err := reader.ReadString('\n')
		seen.WriteString(chunk)
		// The pty echoes input, so match the output line, not the command.
		if strings.Contains(chunk, marker) && !strings.Contains(chunk, "echo") && !strings.Contains(chunk, "stty") {
			return true
		}
		if err != nil {
			t.Logf("terminal output: %q", seen.String())
			return false
		}
	}
}

func TestTerminalResizeWithoutATerminalIsReported(t *testing.T) {
	shells := &terminalSet{logger: zap.NewNop()}
	if response := shells.resizeFrame(frame{ID: "1", Payload: json.RawMessage(`{"columns":80,"rows":24}`)}); response.Error != "terminal-not-ready" {
		t.Fatalf("resize without a terminal = %q", response.Error)
	}
	if response := shells.resizeFrame(frame{ID: "1", Payload: json.RawMessage(`{"columns":0,"rows":24}`)}); response.Error != "invalid-terminal-size" {
		t.Fatalf("zero-width resize = %q", response.Error)
	}
}

func TestTerminalEnvironmentDropsTakoPlumbing(t *testing.T) {
	environment := terminalEnvironment([]string{"HOME=/home/a", "TAKO_TERMINAL_FD=3", "TAKO_LOG_LEVEL=debug", "XDG_RUNTIME_DIR=/run/user/1000"})
	if strings.Join(environment, " ") != "HOME=/home/a XDG_RUNTIME_DIR=/run/user/1000" {
		t.Fatalf("shell environment = %v", environment)
	}
}

func TestPrivilegedBridgeServesOnlyAdministrativeOperations(t *testing.T) {
	if os.Geteuid() != 0 {
		if err := RunPrivileged(strings.NewReader(""), io.Discard, io.Discard); err == nil {
			t.Fatal("the administrative bridge started without root")
		}
	}
	for _, test := range []struct {
		method, payload, code string
	}{
		{"services.read", `{"scope":"system"}`, "unsupported-method"},
		{"file.apply", `{"action":"list","path":"/","unknown":true}`, "invalid-file-operation"},
		{"security.apply", `{"action":"inspect","framework":"SELinux"}`, "invalid-security-operation"},
	} {
		response := dispatchPrivileged(t.Context(), frame{ID: "1", Method: test.method, Payload: json.RawMessage(test.payload)})
		if response.Error != test.code {
			t.Fatalf("%s %s = %q, want %q", test.method, test.payload, response.Error, test.code)
		}
	}
}
