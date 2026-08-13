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

// TestServiceOverrideLoadsInDisposableVM is enabled in a VM with a user
// systemd manager by setting TAKO_TEST_OVERRIDE_VM=1.
func TestServiceOverrideLoadsInDisposableVM(t *testing.T) {
	if os.Getenv("TAKO_TEST_OVERRIDE_VM") != "1" {
		t.Skip("set TAKO_TEST_OVERRIDE_VM=1 in a disposable VM with a user systemd manager")
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(configRoot, "systemd", "user")
	unit := "tako-override-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".service"
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(root, unit)
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/bin/sleep infinity\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(unitPath)
		_, _ = ApplyServiceOverride(root, OverrideOperation{Action: "delete", Scope: "user", Unit: unit, ExpectedFingerprint: currentOverrideFingerprint(root, unit)})
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	})
	operation := OverrideOperation{Action: "apply", Scope: "user", Unit: unit, Restart: "on-failure", RestartSec: "5s"}
	state, err := ApplyServiceOverride(root, operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("systemctl", "--user", "show", unit, "--property=Restart", "--value").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != "on-failure" || state.Fingerprint == "" {
		t.Fatalf("systemd did not load managed restart policy: output=%q state=%#v", output, state)
	}
}

func currentOverrideFingerprint(root, unit string) string {
	state, err := ReadServiceOverride(root, OverrideOperation{Action: "preview", Scope: "user", Unit: unit})
	if err != nil {
		return ""
	}
	return state.Fingerprint
}
