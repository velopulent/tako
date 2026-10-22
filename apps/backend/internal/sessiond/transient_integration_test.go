//go:build linux && integration

package sessiond

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/velopulent/tako/internal/fdpass"
	"golang.org/x/sys/unix"
)

// These tests start real transient units on the caller's user manager, which
// accepts the same StartTransientUnit call as the system manager. A unit's
// stdio socket must carry nothing but its protocol: systemd once copied
// stdout onto stderr, so every log line landed in the sign-in protocol and
// no one could sign in.

// TestTransientServiceReceivesTheSocketAsStdio checks that the service talks
// over the passed socket while its stderr goes to the journal.
func TestTransientServiceReceivesTheSocketAsStdio(t *testing.T) {
	requireUserManager(t)
	local, peer, err := fdpass.Pair(unix.SOCK_STREAM)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := fdpass.Conn(local)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	marker := "tako-stderr-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	unit := startUserService(t, []string{"/bin/sh", "-c", "echo " + marker + " >&2; read line; echo \"echo:$line\""}, peer)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || reply != "echo:hello\n" {
		t.Fatalf("service replied %q: %v", reply, err)
	}
	waitForUserJournal(t, unit, marker)
}

// TestBridgeServiceAnswersOverItsSocket starts the real `tako bridge` the way
// sessiond starts the administrative bridge. The bridge logs as it starts,
// so it answers only if those logs stay out of its socket.
func TestBridgeServiceAnswersOverItsSocket(t *testing.T) {
	requireUserManager(t)
	binary := takoBinary(t)
	local, peer, err := fdpass.Pair(unix.SOCK_STREAM)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := fdpass.Conn(local)
	if err != nil {
		t.Fatal(err)
	}
	unit := startUserService(t, []string{binary, "bridge"}, peer)
	bridge := newUserBridgeProcess(conn, conn)
	defer bridge.Close()
	if err := bridge.ready(); err != nil {
		t.Fatal(err)
	}
	waitForUserJournal(t, unit, "process starting")
}

// TestSignInHelperServiceKeepsLogsOffItsControlSocket starts the real
// `tako session-helper` the way sessiond does. Under a user manager it is not
// root, so it logs why and exits; sessiond must still read nothing but
// sign-in messages, then the end of the socket.
func TestSignInHelperServiceKeepsLogsOffItsControlSocket(t *testing.T) {
	requireUserManager(t)
	binary := takoBinary(t)
	local, peer, err := fdpass.Pair(unix.SOCK_SEQPACKET)
	if err != nil {
		t.Fatal(err)
	}
	control, err := fdpass.Conn(local)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	unit := startUserService(t, []string{binary, "session-helper"}, peer)
	_ = control.SetDeadline(time.Now().Add(5 * time.Second))
	for {
		_, files, err := receiveHelperMessage(control)
		fdpass.CloseAll(files)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("control socket carried something other than a sign-in message: %v", err)
		}
	}
	waitForUserJournal(t, unit, "the sign-in helper must run as root")
}

// requireUserManager skips the test where no user manager can be reached.
func requireUserManager(t *testing.T) {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skipf("no user service manager: %v", err)
	}
	_ = conn.Close()
}

// startUserService starts argv as a transient user service with stdio as its
// stdin and stdout, closes stdio, and returns the unit's name.
func startUserService(t *testing.T, argv []string, stdio *os.File) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unit := "tako-transient-test-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".service"
	err := startTransientServiceOn(ctx, dbus.ConnectSessionBus, unit, "Tako transient service test", argv, stdio)
	stdio.Close()
	if err != nil {
		t.Skipf("user service manager refused a transient unit: %v", err)
	}
	return unit
}

// waitForUserJournal waits for the user unit's journal to contain text.
func waitForUserJournal(t *testing.T, unit, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		output, err := exec.Command("journalctl", "--no-pager", "--output=cat", "_SYSTEMD_USER_UNIT="+unit).CombinedOutput()
		if err == nil && strings.Contains(string(output), text) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never logged %q to the journal: %v\n%s", unit, text, err, output)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// takoBinary returns TAKO_TEST_BINARY, or builds cmd/tako with the build
// tags this test binary was built with.
func takoBinary(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("TAKO_TEST_BINARY"); binary != "" {
		return binary
	}
	var tags string
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "-tags" {
				tags = setting.Value
			}
		}
	}
	binary := filepath.Join(t.TempDir(), "tako")
	output, err := exec.Command("go", "build", "-tags", tags, "-o", binary, "github.com/velopulent/tako/cmd/tako").CombinedOutput()
	if err != nil {
		t.Fatalf("build tako: %v\n%s", err, output)
	}
	return binary
}
