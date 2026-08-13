package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validOverrideOperation(action string) OverrideOperation {
	return OverrideOperation{
		Action:      action,
		Scope:       "user",
		Unit:        "worker.service",
		Environment: map[string]string{"APP_MODE": "safe", "WORKERS": "2"},
		Restart:     "on-failure",
		RestartSec:  "5s",
		MemoryMax:   "512M",
	}
}

func TestValidateOverrideOperationRejectsUnsupportedDirectivesAndInjection(t *testing.T) {
	if err := ValidateOverrideOperation(validOverrideOperation("apply")); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []OverrideOperation{
		{Action: "apply", Scope: "user", Unit: "worker.service", Environment: map[string]string{"BAD-KEY": "value"}, Restart: "always"},
		{Action: "apply", Scope: "user", Unit: "worker.service", Environment: map[string]string{"APP": "value\nExecStart=/bin/sh"}, Restart: "always"},
		{Action: "apply", Scope: "user", Unit: "worker.service", Restart: "on-failure", RestartSec: "$(id)"},
		{Action: "apply", Scope: "user", Unit: "worker.service", Restart: "on-failure", CPUQuota: "10100%"},
		{Action: "apply", Scope: "user", Unit: "worker.service", Environment: nil},
	} {
		if err := ValidateOverrideOperation(operation); !errors.Is(err, ErrInvalidOverrideOperation) {
			t.Fatalf("operation %#v returned %v", operation, err)
		}
	}
	if err := ValidateOverrideOperation(OverrideOperation{Action: "preview", Scope: "user", Unit: "worker.service", Environment: map[string]string{"BAD-KEY": "value"}}); !errors.Is(err, ErrInvalidOverrideOperation) {
		t.Fatalf("malformed preview returned %v", err)
	}
}

func TestApplyServiceOverrideIsAtomicAndRejectsStaleWrites(t *testing.T) {
	root := t.TempDir()
	operation := validOverrideOperation("apply")
	state, err := ApplyServiceOverride(root, operation)
	if err != nil || !state.Exists || state.Fingerprint == "" {
		t.Fatalf("apply state=%#v err=%v", state, err)
	}
	path := filepath.Join(root, "worker.service.d", "50-tako.conf")
	content, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(content), `Environment="APP_MODE=safe"`) || strings.Contains(string(content), "ExecStart") {
		t.Fatalf("unexpected drop-in content=%q err=%v", content, err)
	}
	preview, err := ReadServiceOverride(root, OverrideOperation{Action: "preview", Scope: "user", Unit: "worker.service"})
	if err != nil || preview.Fingerprint != state.Fingerprint || preview.Definition == nil {
		t.Fatalf("preview state=%#v err=%v", preview, err)
	}
	stale := operation
	stale.Environment = map[string]string{"APP_MODE": "changed"}
	stale.ExpectedFingerprint = strings.Repeat("a", 64)
	if _, err := ApplyServiceOverride(root, stale); !errors.Is(err, ErrOverrideConflict) {
		t.Fatalf("stale apply returned %v", err)
	}
	updated := operation
	updated.Environment = map[string]string{"APP_MODE": "changed"}
	updated.ExpectedFingerprint = state.Fingerprint
	state, err = ApplyServiceOverride(root, updated)
	if err != nil || state.Fingerprint == preview.Fingerprint {
		t.Fatalf("updated state=%#v err=%v", state, err)
	}
	deleted, err := ApplyServiceOverride(root, OverrideOperation{Action: "delete", Scope: "user", Unit: "worker.service", ExpectedFingerprint: state.Fingerprint})
	if err != nil || deleted.Exists {
		t.Fatalf("delete state=%#v err=%v", deleted, err)
	}
}

func TestReadServiceOverrideRejectsUnmanagedDirective(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "worker.service.d")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "50-tako.conf"), []byte("[Service]\nExecStart=/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadServiceOverride(root, OverrideOperation{Action: "preview", Scope: "user", Unit: "worker.service"})
	if !errors.Is(err, ErrOverrideUnmanaged) {
		t.Fatalf("unmanaged directive returned %v", err)
	}
}
