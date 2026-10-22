//go:build linux && integration

package sessiond

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

// TestSignInThroughSystemd signs the fixture user in the way sessiond does:
// the real `tako session-helper` runs PAM in its own transient service on the
// system manager and starts the user's bridge, and a shell runs through it.
// tools/tako-integration runs it as root in disposable distribution VMs.
func TestSignInThroughSystemd(t *testing.T) {
	username := os.Getenv("TAKO_TEST_PAM_USER")
	encodedResponses := takePAMResponses()
	binary := os.Getenv("TAKO_TEST_BINARY")
	if username == "" || encodedResponses == "" || binary == "" || os.Geteuid() != 0 {
		t.Skip("run as root with TAKO_TEST_PAM_USER, TAKO_TEST_PAM_RESPONSES, and TAKO_TEST_BINARY in a disposable VM")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opener := helperOpener{executable: binary, start: startTransientService}
	session, err := opener.OpenSessionWithConversation(ctx, username, "127.0.0.1", scriptedPAMConversation(t, encodedResponses))
	if err != nil {
		t.Fatalf("sign-in failed: %v", err)
	}
	defer session.Close()
	if session.Identity.Username != username || session.Identity.UID <= 0 {
		t.Fatalf("unexpected identity: %#v", session.Identity)
	}
	bridge, ok := session.Bridge.(*userBridgeProcess)
	if !ok {
		t.Fatalf("session has no user bridge: %T", session.Bridge)
	}
	terminal, err := bridge.openTerminal(auth.TerminalSize{Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	_ = terminal.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := terminal.Write([]byte("echo tako-$((6*7)) $(cat /proc/self/cgroup)\n")); err != nil {
		t.Fatal(err)
	}
	output, err := readUntil(terminal, "tako-42 ", "\n")
	if err != nil {
		t.Fatalf("shell never answered: %v\n%q", err, output)
	}
	// The shell belongs to the sign-in, not to sessiond (here, the test).
	shellCgroup := strings.TrimSpace(output[strings.Index(output, "tako-42 ")+len("tako-42 "):])
	ownCgroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	if shellCgroup == strings.TrimSpace(string(ownCgroup)) || !strings.Contains(shellCgroup, "/session-") && !strings.Contains(shellCgroup, "/tako-login-") {
		t.Fatalf("shell runs in %q, outside the user's sign-in", shellCgroup)
	}
	// Signing out stops the helper, which stops the bridge and hangs up the
	// shell.
	session.Close()
	_ = terminal.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.Copy(io.Discard, terminal); err != nil {
		t.Fatalf("the shell outlived sign-out: %v", err)
	}
}

// readUntil reads until the output contains start followed by end.
func readUntil(reader io.Reader, start, end string) (string, error) {
	var output []byte
	buffer := make([]byte, 4096)
	for {
		if index := strings.Index(string(output), start); index >= 0 && strings.Contains(string(output[index:]), end) {
			return string(output), nil
		}
		count, err := reader.Read(buffer)
		output = append(output, buffer[:count]...)
		if err != nil {
			return string(output), err
		}
	}
}
