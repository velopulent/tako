package preferences

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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

type OperationReceipt struct {
	ID             string    `json:"id"`
	Actor          string    `json:"actor"`
	Target         string    `json:"target"`
	StartedAt      time.Time `json:"startedAt"`
	CompletedAt    time.Time `json:"completedAt"`
	Result         string    `json:"result"`
	Error          string    `json:"error,omitempty"`
	Administrative bool      `json:"administrative"`
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

func (store *Store) RecordOperation(ctx context.Context, receipt OperationReceipt) (OperationReceipt, error) {
	if receipt.Actor == "" || len(receipt.Actor) > 256 || receipt.Target == "" || len(receipt.Target) > 512 || len(receipt.Result) == 0 || len(receipt.Result) > 64 || len(receipt.Error) > 1024 {
		return OperationReceipt{}, errors.New("invalid operation receipt")
	}
	if receipt.StartedAt.IsZero() {
		receipt.StartedAt = time.Now().UTC()
	}
	if receipt.CompletedAt.IsZero() {
		receipt.CompletedAt = time.Now().UTC()
	}
	if receipt.CompletedAt.Before(receipt.StartedAt) {
		return OperationReceipt{}, errors.New("invalid operation receipt timing")
	}
	id, err := receiptID()
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("create operation receipt id: %w", err)
	}
	receipt.ID = id
	_, err = store.database.ExecContext(ctx, `
		INSERT INTO operation_receipts
			(id, actor, target, started_at, completed_at, result, error, administrative)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, receipt.ID, receipt.Actor, receipt.Target, receipt.StartedAt.UTC().Format(time.RFC3339Nano), receipt.CompletedAt.UTC().Format(time.RFC3339Nano), receipt.Result, receipt.Error, receipt.Administrative)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("save operation receipt: %w", err)
	}
	return receipt, nil
}

func (store *Store) OperationReceipts(ctx context.Context, limit int) ([]OperationReceipt, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := store.database.QueryContext(ctx, `
		SELECT id, actor, target, started_at, completed_at, result, error, administrative
		FROM operation_receipts
		ORDER BY completed_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list operation receipts: %w", err)
	}
	defer rows.Close()
	items := make([]OperationReceipt, 0, limit)
	for rows.Next() {
		var item OperationReceipt
		var startedAt, completedAt string
		if err := rows.Scan(&item.ID, &item.Actor, &item.Target, &startedAt, &completedAt, &item.Result, &item.Error, &item.Administrative); err != nil {
			return nil, fmt.Errorf("read operation receipt: %w", err)
		}
		item.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt)
		if err != nil {
			return nil, fmt.Errorf("parse operation start time: %w", err)
		}
		item.CompletedAt, err = time.Parse(time.RFC3339Nano, completedAt)
		if err != nil {
			return nil, fmt.Errorf("parse operation completion time: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read operation receipts: %w", err)
	}
	return items, nil
}

func receiptID() (string, error) {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
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
		`CREATE TABLE operation_receipts (
			id TEXT PRIMARY KEY,
			actor TEXT NOT NULL,
			target TEXT NOT NULL,
			started_at TEXT NOT NULL,
			completed_at TEXT NOT NULL,
			result TEXT NOT NULL,
			error TEXT NOT NULL,
			administrative INTEGER NOT NULL CHECK (administrative IN (0, 1))
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
