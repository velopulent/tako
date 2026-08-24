package packagekit

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestParseHistoryTimestampAcceptsShortOffsets(t *testing.T) {
	cases := []string{
		"2026-01-29T12:57:49.112827-08",
		"2026-01-29T19:27:49.112827-01:30",
		"2026-01-29T20:57:49.112827Z",
	}
	for _, timeSpec := range cases {
		if _, ok := parseHistoryTimestamp(timeSpec); !ok {
			t.Fatalf("parseHistoryTimestamp(%q) failed", timeSpec)
		}
	}
	if _, ok := parseHistoryTimestamp(""); ok {
		t.Fatal("empty timestamp accepted")
	}
	if _, ok := parseHistoryTimestamp("not-a-time"); ok {
		t.Fatal("garbage timestamp accepted")
	}
}

func TestStatusMessageCoversUpdateFlow(t *testing.T) {
	if got := StatusMessage(StatusWaitingForLock); got != "Waiting for another package operation" {
		t.Fatalf("waiting-for-lock message = %q", got)
	}
	if got := StatusMessage(8); got != "Downloading" {
		t.Fatalf("download message = %q", got)
	}
	if got := StatusMessage(10); got != "Updating" {
		t.Fatalf("update message = %q", got)
	}
	if got := StatusMessage(StatusFinished); got != "Finished" {
		t.Fatalf("finished message = %q", got)
	}
}

func TestTransactionWatcherRecordBoundsAndLatestLog(t *testing.T) {
	watcher := &TransactionWatcher{logs: make(map[dbus.ObjectPath][]ActionLogEntry)}
	for i := 0; i < maxActionLogEntries+50; i++ {
		watcher.record("/org/freedesktop/PackageKit/transactions/1", ActionLogEntry{Status: 10, PackageID: "pkg"})
	}
	if got := len(watcher.ActionLog("/org/freedesktop/PackageKit/transactions/1")); got != maxActionLogEntries {
		t.Fatalf("log length = %d, want %d", got, maxActionLogEntries)
	}
	if got := watcher.LatestLog(""); len(got) != maxActionLogEntries {
		t.Fatalf("latest log = %d entries", len(got))
	}
	if got := watcher.ActionLog("/org/freedesktop/PackageKit/transactions/missing"); len(got) != 0 {
		t.Fatalf("missing path returned %d entries", len(got))
	}
}
