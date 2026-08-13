package platform

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validTimerOperation(action string) TimerOperation {
	return TimerOperation{
		Action:      action,
		Scope:       "user",
		Name:        "backup",
		Description: "Nightly backup",
		OnCalendar:  "*-*-* 02:00:00",
		Command:     "/usr/bin/backup --safe",
	}
}

func TestValidateTimerOperationRejectsInjectionAndAmbiguousSchedules(t *testing.T) {
	valid := validTimerOperation("create")
	if err := ValidateTimerOperation(valid); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []TimerOperation{
		{Action: "create", Scope: "user", Name: "../backup", OnCalendar: "hourly", Command: "/usr/bin/backup"},
		{Action: "create", Scope: "user", Name: "backup", OnCalendar: "hourly\nExecStart=/bin/sh", Command: "/usr/bin/backup"},
		{Action: "create", Scope: "user", Name: "backup", OnCalendar: "hourly", OnBootSec: "1h", Command: "/usr/bin/backup"},
		{Action: "create", Scope: "user", Name: "backup", OnCalendar: "hourly", Command: "/usr/bin/backup;id"},
		{Action: "update", Scope: "user", Name: "backup", OnCalendar: "hourly", Command: "/usr/bin/backup"},
		{Action: "update", Scope: "user", Name: "backup", ExpectedFingerprint: strings.Repeat("a", 64), OnCalendar: "hourly"},
	} {
		if err := ValidateTimerOperation(operation); !errors.Is(err, ErrInvalidTimerOperation) {
			t.Fatalf("operation %#v returned %v", operation, err)
		}
	}
}

func TestTimerOperationRejectsUnknownAndTrailingJSON(t *testing.T) {
	var operation TimerOperation
	if err := json.Unmarshal([]byte(`{"action":"preview","scope":"user","name":"backup","extra":true}`), &operation); err == nil {
		t.Fatal("unknown timer field accepted")
	}
	if err := json.Unmarshal([]byte(`{"action":"preview","scope":"user","name":"backup"} {}`), &operation); err == nil {
		t.Fatal("trailing timer JSON accepted")
	}
}

func TestApplyTimerFilesCreatesUpdatesConflictsAndDeletesPair(t *testing.T) {
	root := t.TempDir()
	create := validTimerOperation("create")
	state, err := ApplyTimerFiles(root, create)
	if err != nil || !state.Exists || state.Fingerprint == "" {
		t.Fatalf("create state=%#v err=%v", state, err)
	}
	timerPath := filepath.Join(root, "backup.timer")
	servicePath := filepath.Join(root, "backup.service")
	if _, err := os.Stat(timerPath); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(servicePath); err != nil || !strings.Contains(string(content), "ExecStart=/usr/bin/backup --safe") {
		t.Fatalf("service content=%q err=%v", content, err)
	}
	if _, err := ApplyTimerFiles(root, create); !errors.Is(err, ErrTimerConflict) {
		t.Fatalf("duplicate create returned %v", err)
	}

	update := create
	update.Action = "update"
	update.ExpectedFingerprint = state.Fingerprint
	update.Description = "Updated backup"
	updated, err := ApplyTimerFiles(root, update)
	if err != nil || updated.Fingerprint == state.Fingerprint {
		t.Fatalf("update state=%#v err=%v", updated, err)
	}
	stale := update
	stale.ExpectedFingerprint = state.Fingerprint
	if _, err := ApplyTimerFiles(root, stale); !errors.Is(err, ErrTimerConflict) {
		t.Fatalf("stale update returned %v", err)
	}

	remove := TimerOperation{Action: "delete", Scope: "user", Name: "backup", ExpectedFingerprint: updated.Fingerprint}
	removed, err := ApplyTimerFiles(root, remove)
	if err != nil || removed.Exists {
		t.Fatalf("delete state=%#v err=%v", removed, err)
	}
}

func TestApplyTimerFilesEnableDisableUsesSafeWantsLink(t *testing.T) {
	root := t.TempDir()
	state, err := ApplyTimerFiles(root, validTimerOperation("create"))
	if err != nil {
		t.Fatal(err)
	}
	enable := TimerOperation{Action: "enable", Scope: "user", Name: "backup", ExpectedFingerprint: state.Fingerprint}
	state, err = ApplyTimerFiles(root, enable)
	if err != nil || !state.Enabled {
		t.Fatalf("enable state=%#v err=%v", state, err)
	}
	disable := enable
	disable.Action = "disable"
	disable.ExpectedFingerprint = state.Fingerprint
	state, err = ApplyTimerFiles(root, disable)
	if err != nil || state.Enabled {
		t.Fatalf("disable state=%#v err=%v", state, err)
	}
}
