package preferences

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenMigratesAndSecuresDatabase(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	store, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, path := range []string{dataDir, filepath.Join(dataDir, "tako.db")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if path == dataDir {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s permissions are %o, want %o", path, info.Mode().Perm(), want)
		}
	}

	var version int
	if err := store.database.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("database version is %d, want 3", version)
	}
}

func TestMonitoringPreferenceUsesOptimisticRevision(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	created, err := store.SetMonitoringInterval(ctx, "30s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 {
		t.Fatalf("created revision is %d, want 1", created.Revision)
	}
	if _, err := store.SetMonitoringInterval(ctx, "5s", 0); err != ErrConflict {
		t.Fatalf("stale update error is %v, want %v", err, ErrConflict)
	}
	updated, err := store.SetMonitoringInterval(ctx, "5s", created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.DefaultInterval != "5s" {
		t.Fatalf("unexpected update: %#v", updated)
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var count int
	if err := second.database.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration recorded %d times, want 1", count)
	}
	var version int
	if err := second.database.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("database version is %d, want 3", version)
	}
}

func TestDiagnosticJobsAreDurableAndRecoverWithoutRetry(t *testing.T) {
	dataDir := t.TempDir()
	store, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateJob(context.Background(), "host-inventory", "operator", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartJob(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateJobProgress(context.Background(), job.ID, 50, "Collecting capabilities"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RecoverJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != JobInterrupted || recovered.Message != "Interrupted by service restart" {
		t.Fatalf("job was not marked interrupted: %#v", recovered)
	}
	if err := store.CompleteJob(context.Background(), job.ID, json.RawMessage(`{"unsafe":true}`)); err != ErrJobTerminal {
		t.Fatalf("interrupted job was allowed to complete: %v", err)
	}
}

func TestDiagnosticJobCancellationIsExplicit(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.CreateJob(context.Background(), "host-inventory", "operator", false)
	if err != nil {
		t.Fatal(err)
	}
	canceled, err := store.CancelJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.State != JobCanceled || !canceled.CancelRequested || canceled.CompletedAt.IsZero() {
		t.Fatalf("unexpected canceled job: %#v", canceled)
	}
	if _, err := store.CancelJob(context.Background(), job.ID); err != ErrJobTerminal {
		t.Fatalf("second cancellation error is %v, want %v", err, ErrJobTerminal)
	}
}

func TestOperationReceiptsAreDurableAndBounded(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	recorded, err := store.RecordOperation(context.Background(), OperationReceipt{
		Actor:          "operator",
		Target:         "system/sshd.service/restart",
		StartedAt:      started,
		CompletedAt:    started.Add(time.Second),
		Result:         "succeeded",
		Administrative: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if recorded.ID == "" {
		t.Fatal("receipt has no opaque id")
	}
	items, err := store.OperationReceipts(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Target != recorded.Target || !items[0].Administrative {
		t.Fatalf("unexpected receipts: %#v", items)
	}
	if _, err := store.RecordOperation(context.Background(), OperationReceipt{Actor: "", Target: "secret", Result: "failed"}); err == nil {
		t.Fatal("invalid receipt was accepted")
	}
}
