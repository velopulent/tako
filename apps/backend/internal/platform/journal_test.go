package platform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalQueryValidationAndOpaqueCursor(t *testing.T) {
	query := JournalQuery{Limit: 50, Boot: strings.Repeat("a", 32), Priority: "3..5", Unit: "worker.service", Executable: "/usr/bin/worker", Text: "failed"}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (JournalQuery{Limit: 0, Since: time.Now(), Until: time.Now().Add(-time.Minute)}).Validate(); err == nil {
		t.Fatal("invalid time range accepted")
	}
	if err := (JournalQuery{Limit: 1, Unit: "../worker.service"}).Validate(); err == nil {
		t.Fatal("unsafe unit accepted")
	}
	raw := "s=cursor;seq=123"
	encoded := EncodeJournalCursor(raw)
	decoded, err := DecodeJournalCursor(encoded)
	if err != nil || decoded != raw {
		t.Fatalf("cursor round trip decoded=%q err=%v", decoded, err)
	}
	if _, err := DecodeJournalCursor("not-base64!!!"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestParseLogEntryKeepsCursorPrivateAndSupportsUserUnits(t *testing.T) {
	entry, ok := parseLogEntry([]byte(`{"__CURSOR":"s=cursor;seq=123","__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"3","_SYSTEMD_USER_UNIT":"worker.service","MESSAGE":"failed safely"}`))
	if !ok || entry.Unit != "worker.service" || entry.Cursor == "" {
		t.Fatalf("parsed entry=%#v ok=%v", entry, ok)
	}
	if payload := entry.Cursor; payload == "" {
		t.Fatal("missing cursor")
	}
}

func TestQueryLogsUsesBoundedOpaquePagination(t *testing.T) {
	bin := t.TempDir()
	journalctl := filepath.Join(bin, "journalctl")
	payload := strings.Join([]string{
		`{"__CURSOR":"c2","__REALTIME_TIMESTAMP":"1700000000000002","PRIORITY":"3","_SYSTEMD_UNIT":"worker.service","MESSAGE":"two"}`,
		`{"__CURSOR":"c1","__REALTIME_TIMESTAMP":"1700000000000001","PRIORITY":"3","_SYSTEMD_UNIT":"worker.service","MESSAGE":"one"}`,
		`{"__CURSOR":"c0","__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"3","_SYSTEMD_UNIT":"worker.service","MESSAGE":"zero"}`,
	}, "\n") + "\n"
	script := "#!/bin/sh\nprintf '%s' '" + strings.ReplaceAll(payload, "'", "'\\''") + "'\n"
	if err := os.WriteFile(journalctl, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	page, err := QueryLogs(context.Background(), JournalQuery{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if page.Items[0].Cursor != "" {
		t.Fatal("cursor leaked in public journal item")
	}
}
