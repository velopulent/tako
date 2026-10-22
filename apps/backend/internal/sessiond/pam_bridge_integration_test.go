//go:build linux && integration

package sessiond

import (
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/fdpass"
)

// TestPAMBackedUserBridge is run as root in disposable distribution VMs after
// building TAKO_TEST_BINARY. It verifies NSS identity, PAM environment
// propagation, bridge readiness, real process credentials, and termination.
func TestPAMBackedUserBridge(t *testing.T) {
	username := os.Getenv("TAKO_TEST_PAM_USER")
	encodedResponses := takePAMResponses()
	binary := os.Getenv("TAKO_TEST_BINARY")
	if username == "" || encodedResponses == "" || binary == "" {
		t.Skip("set TAKO_TEST_PAM_USER, TAKO_TEST_PAM_RESPONSES, and TAKO_TEST_BINARY in a disposable VM")
	}
	service := os.Getenv("TAKO_TEST_PAM_SERVICE")
	if service == "" {
		service = "tako"
	}
	session, err := (auth.PAMAuthenticator{Service: service}).OpenSessionWithConversation(username, "", scriptedPAMConversation(t, encodedResponses))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.Identity.Username != username || session.Identity.UID <= 0 || session.Identity.GID <= 0 {
		t.Fatalf("unexpected NSS identity: %#v", session.Identity)
	}
	bridge, rpc, terminals, err := startSessionBridge(session, binary)
	if err != nil {
		t.Fatal(err)
	}
	defer terminals.Close()
	conn, err := fdpass.Conn(rpc)
	if err != nil {
		t.Fatal(err)
	}
	client := newUserBridgeProcess(conn, conn)
	defer client.Close()
	if err := client.ready(); err != nil {
		t.Fatalf("user bridge did not answer: %v", err)
	}
	status, err := os.ReadFile("/proc/" + strconv.Itoa(bridge.command.Process.Pid) + "/status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(status), "Uid:\t"+strconv.Itoa(session.Identity.UID)) || !strings.Contains(string(status), "Gid:\t"+strconv.Itoa(session.Identity.GID)) || !strings.Contains(string(status), "Groups:") {
		t.Fatalf("bridge has wrong credentials: %s", status)
	}
	account, err := user.Lookup(username)
	if err != nil {
		t.Fatal(err)
	}
	expectedGroups, err := account.GroupIds()
	if err != nil {
		t.Fatal(err)
	}
	groupLine := ""
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Groups:") {
			groupLine = line
		}
	}
	for _, groupID := range expectedGroups {
		if !strings.Contains(" "+groupLine+" ", " "+groupID+" ") {
			t.Fatalf("bridge lacks supplementary group %s: %s", groupID, groupLine)
		}
	}
	environment, err := os.ReadFile("/proc/" + strconv.Itoa(bridge.command.Process.Pid) + "/environ")
	if err != nil {
		t.Fatal(err)
	}
	environmentKey := os.Getenv("TAKO_TEST_PAM_ENV_KEY")
	environmentValue := os.Getenv("TAKO_TEST_PAM_ENV_VALUE")
	if environmentKey != "" && !strings.Contains(string(environment), environmentKey+"="+environmentValue+"\x00") {
		t.Fatalf("bridge lacks PAM environment marker %q", environmentKey)
	}
	pid := bridge.command.Process.Pid
	bridge.stop()
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("user bridge remained alive after close")
	}
}

// takePAMResponses returns TAKO_TEST_PAM_RESPONSES, removing it from the
// environment on first use so no child process inherits the passwords.
var takePAMResponses = sync.OnceValue(func() string {
	encoded := os.Getenv("TAKO_TEST_PAM_RESPONSES")
	os.Unsetenv("TAKO_TEST_PAM_RESPONSES")
	return encoded
})

// scriptedPAMConversation answers PAM's prompts, in order, with the JSON
// string array encoded.
func scriptedPAMConversation(t *testing.T, encoded string) auth.Conversation {
	t.Helper()
	var responses []string
	if err := json.Unmarshal([]byte(encoded), &responses); err != nil {
		t.Fatal("TAKO_TEST_PAM_RESPONSES must be a JSON string array")
	}
	index := 0
	return func(style auth.PromptStyle, _ string) (string, error) {
		if style == auth.PromptInfo || style == auth.PromptError {
			return "", nil
		}
		if index >= len(responses) {
			return "", errors.New("PAM requested more responses than supplied")
		}
		response := responses[index]
		responses[index] = ""
		index++
		return response, nil
	}
}
