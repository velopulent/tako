//go:build linux && integration

package sessiond

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

// TestAdministrativeBridgeThroughSystemd starts the real
// `tako bridge --privileged` in its own transient service on the system
// manager, as sessiond does for an administrative grant, and has it inspect a
// file in the host's /tmp, which sessiond's private /tmp hides.
// tools/tako-integration runs it as root in disposable distribution VMs.
func TestAdministrativeBridgeThroughSystemd(t *testing.T) {
	binary := os.Getenv("TAKO_TEST_BINARY")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("run as root with TAKO_TEST_BINARY in a disposable VM")
	}
	marker, err := os.CreateTemp("/tmp", "tako-admin-bridge-")
	if err != nil {
		t.Fatal(err)
	}
	_ = marker.Close()
	defer os.Remove(marker.Name())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bridge, err := newPrivilegedBridgeStarter(startTransientService, binary)(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	if _, err := bridge.call(ctx, "file.apply", platform.FileOperation{Action: "stat", Path: marker.Name()}); err != nil {
		t.Fatalf("administrative bridge cannot see %s: %v", marker.Name(), err)
	}
}
