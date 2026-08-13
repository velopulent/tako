//go:build linux && integration

package sessiond

import (
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/velopulent/tako/internal/auth"
)

// TestPAMBackedUserBridge is run as root in disposable distribution VMs after
// building TAKO_TEST_BINARY. It verifies NSS identity, PAM environment
// propagation, bridge readiness, real process credentials, and termination.
func TestPAMBackedUserBridge(t *testing.T) {
	username := os.Getenv("TAKO_TEST_PAM_USER")
	encodedResponses := os.Getenv("TAKO_TEST_PAM_RESPONSES")
	binary := os.Getenv("TAKO_TEST_BINARY")
	if username == "" || encodedResponses == "" || binary == "" {
		t.Skip("set TAKO_TEST_PAM_USER, TAKO_TEST_PAM_RESPONSES, and TAKO_TEST_BINARY in a disposable VM")
	}
	var responses []string
	if err := json.Unmarshal([]byte(encodedResponses), &responses); err != nil {
		t.Fatal("TAKO_TEST_PAM_RESPONSES must be a JSON string array")
	}
	os.Unsetenv("TAKO_TEST_PAM_RESPONSES")
	service := os.Getenv("TAKO_TEST_PAM_SERVICE")
	if service == "" {
		service = "tako"
	}
	index := 0
	session, err := (auth.PAMAuthenticator{Service: service}).OpenSessionWithConversation(username, func(style auth.PromptStyle, _ string) (string, error) {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.Identity.Username != username || session.Identity.UID <= 0 || session.Identity.GID <= 0 {
		t.Fatalf("unexpected NSS identity: %#v", session.Identity)
	}
	bridge, err := startUserBridgeExecutable(session, binary)
	if err != nil {
		t.Fatal(err)
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
	if err := bridge.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("user bridge remained alive after close")
	}
}
