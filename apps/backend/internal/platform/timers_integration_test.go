//go:build linux && integration

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestStructuredTimerPairLoadsInDisposableVM is enabled in a VM with a user
// systemd manager by setting TAKO_TEST_TIMER_VM=1. It verifies the generated
// pair through the same systemctl user boundary used by the user bridge.
func TestStructuredTimerPairLoadsInDisposableVM(t *testing.T) {
	if os.Getenv("TAKO_TEST_TIMER_VM") != "1" {
		t.Skip("set TAKO_TEST_TIMER_VM=1 in a disposable VM with a user systemd manager")
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(configRoot, "systemd", "user")
	name := "tako-integration-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	operation := TimerOperation{
		Action:     "create",
		Scope:      "user",
		Name:       name,
		OnBootSec:  "10m",
		Command:    "/bin/true",
	}
	state, err := ApplyTimerFiles(root, operation)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ApplyTimerFiles(root, TimerOperation{Action: "delete", Scope: "user", Name: name, ExpectedFingerprint: state.Fingerprint})
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	})
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("systemctl", "--user", "show", name+".timer", "--property=Unit", "--value").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != name+".service" {
		t.Fatalf("systemd resolved timer pair to %q", strings.TrimSpace(string(output)))
	}
}
