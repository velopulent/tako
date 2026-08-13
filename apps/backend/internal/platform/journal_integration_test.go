//go:build linux && integration

package platform

import (
	"context"
	"os"
	"testing"
)

// TestJournalQueryAgainstSystemd is enabled in a disposable VM with journald
// by setting TAKO_TEST_JOURNAL_VM=1.
func TestJournalQueryAgainstSystemd(t *testing.T) {
	if os.Getenv("TAKO_TEST_JOURNAL_VM") != "1" {
		t.Skip("set TAKO_TEST_JOURNAL_VM=1 in a disposable VM with journald")
	}
	page, err := QueryLogs(context.Background(), JournalQuery{Limit: 10, Boot: "current", Priority: "0..7"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Items {
		if entry.Cursor != "" {
			t.Fatal("journal cursor escaped the page boundary")
		}
	}
}
