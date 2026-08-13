package preferences

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const monitoringIntervalKey = "monitoring.default_interval"

var ErrConflict = errors.New("preference changed")

type MonitoringPreference struct {
	DefaultInterval string
	Revision        int64
}

type Store struct {
	database *sql.DB
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure data directory: %w", err)
	}
	databasePath := filepath.Join(dataDir, "tako.db")
	file, err := os.OpenFile(databasePath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create preferences database: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close preferences database file: %w", err)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		return nil, fmt.Errorf("secure preferences database: %w", err)
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open preferences database: %w", err)
	}
	database.SetMaxOpenConns(1)
	store := &Store{database: database}
	if err := store.migrate(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := databasePath + suffix
		if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = database.Close()
			return nil, fmt.Errorf("secure preferences database file: %w", err)
		}
	}
	return store, nil
}

func (store *Store) Close() error {
	return store.database.Close()
}

func (store *Store) MonitoringInterval(ctx context.Context, fallback string) (MonitoringPreference, error) {
	var preference MonitoringPreference
	err := store.database.QueryRowContext(ctx, "SELECT value, revision FROM preferences WHERE key = ?", monitoringIntervalKey).Scan(&preference.DefaultInterval, &preference.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return MonitoringPreference{DefaultInterval: fallback}, nil
	}
	if err != nil {
		return MonitoringPreference{}, fmt.Errorf("read monitoring preference: %w", err)
	}
	return preference, nil
}

func (store *Store) SetMonitoringInterval(ctx context.Context, value string, expectedRevision int64) (MonitoringPreference, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var result sql.Result
	var err error
	if expectedRevision == 0 {
		result, err = store.database.ExecContext(ctx, `
			INSERT INTO preferences (key, value, revision, updated_at)
			VALUES (?, ?, 1, ?)
			ON CONFLICT(key) DO NOTHING
		`, monitoringIntervalKey, value, now)
	} else {
		result, err = store.database.ExecContext(ctx, `
			UPDATE preferences
			SET value = ?, revision = revision + 1, updated_at = ?
			WHERE key = ? AND revision = ?
		`, value, now, monitoringIntervalKey, expectedRevision)
	}
	if err != nil {
		return MonitoringPreference{}, fmt.Errorf("save monitoring preference: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return MonitoringPreference{}, fmt.Errorf("verify monitoring preference: %w", err)
	}
	if changed != 1 {
		return MonitoringPreference{}, ErrConflict
	}
	return store.MonitoringInterval(ctx, value)
}

func (store *Store) migrate(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := store.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure preferences database: %w", err)
		}
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin database migration: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
		`CREATE TABLE preferences (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			revision INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	if _, err := transaction.ExecContext(ctx, migrations[0]); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	var current int
	if err := transaction.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return fmt.Errorf("read database version: %w", err)
	}
	for version := current + 1; version < len(migrations); version++ {
		if _, err := transaction.ExecContext(ctx, migrations[version]); err != nil {
			return fmt.Errorf("apply database migration %d: %w", version, err)
		}
		if _, err := transaction.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)", version); err != nil {
			return fmt.Errorf("record database migration %d: %w", version, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit database migration: %w", err)
	}
	return nil
}
