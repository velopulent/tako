package preferences

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
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
	if version != 1 {
		t.Fatalf("database version is %d, want 1", version)
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
}
