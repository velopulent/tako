//go:build linux && integration

package platform

import (
	"context"
	"os"
	"testing"
)

// TestLoginHistoryAgainstSystemd is run in the disposable Linux VM with a
// known account. It verifies the real journal command and keeps an empty
// history valid because a freshly provisioned VM may have no login event yet.
func TestLoginHistoryAgainstSystemd(t *testing.T) {
	username := os.Getenv("TAKO_TEST_LOGIN_HISTORY_USER")
	if username == "" {
		t.Skip("set TAKO_TEST_LOGIN_HISTORY_USER in the Linux VM")
	}
	page, err := QueryLoginHistory(context.Background(), LoginHistoryQuery{Username: username, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Identity.Username != username {
		t.Fatalf("identity metadata username=%q", page.Identity.Username)
	}
	if len(page.Items) > MaxLoginHistoryPageSize {
		t.Fatalf("history page exceeded bound: %d", len(page.Items))
	}
}
