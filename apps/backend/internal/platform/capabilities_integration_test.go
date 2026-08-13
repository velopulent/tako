//go:build linux && integration

package platform

import (
	"context"
	"testing"
	"time"
)

// TestHostCapabilitiesContract is the disposable-VM seam used on each
// supported distribution image. It intentionally asserts the contract and
// fail-closed invariants, not a particular distribution's installed services.
func TestHostCapabilitiesContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	capabilities := Detect(ctx)
	if len(capabilities) < 10 {
		t.Fatalf("runtime capability inventory is incomplete: %#v", capabilities)
	}
	for _, capability := range capabilities {
		if capability.ID == "" || capability.State == "" || capability.Contract == "" || capability.ReadAuthority == "" || capability.MutationAuthority == "" {
			t.Fatalf("capability lacks contract fields: %#v", capability)
		}
		if capability.State == StateConflicted && capability.Mutable {
			t.Fatalf("conflicted backend permits mutation: %#v", capability)
		}
		if capability.State != StateReady && capability.SetupGuidance == "" {
			t.Fatalf("unready backend lacks setup guidance: %#v", capability)
		}
	}
}
