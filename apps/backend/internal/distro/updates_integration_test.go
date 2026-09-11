//go:build linux && integration

package distro

import (
	"context"
	"testing"

	"github.com/velopulent/tako/internal/platform"
)

// TestUpdatesAgainstHost exercises the real distro-specific update adapter in
// the disposable Linux VM. A host without its tagged native package manager is
// a supported degraded result, but the response must always remain bounded and
// explicit.
func TestUpdatesAgainstHost(t *testing.T) {
	status := NewUpdateService().Status(context.Background())
	if len(status.Packages) > platform.MaxUpdatePackages {
		t.Fatalf("update inventory exceeded bound: %d", len(status.Packages))
	}
	if status.Backend == "" || status.Contract == "" || status.Message == "" {
		t.Fatalf("update status omitted runtime contract: %#v", status)
	}
}
