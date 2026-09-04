//go:build linux && integration

package platform

import (
	"context"
	"testing"
)

// TestUpdatesAgainstHost exercises the real bounded read-only adapter in the
// disposable Linux VM. A host without its tagged native package manager is a supported
// degraded result, but the response must always remain bounded and explicit.
func TestUpdatesAgainstHost(t *testing.T) {
	status := Updates(context.Background())
	if len(status.Packages) > MaxUpdatePackages {
		t.Fatalf("update inventory exceeded bound: %d", len(status.Packages))
	}
	if status.Backend == "" || status.Contract == "" || status.Message == "" {
		t.Fatalf("update status omitted runtime contract: %#v", status)
	}
}
