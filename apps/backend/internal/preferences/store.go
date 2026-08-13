package preferences

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
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

const (
	JobPending     = "pending"
	JobRunning     = "running"
	JobSucceeded   = "succeeded"
	JobFailed      = "failed"
	JobCanceled    = "canceled"
	JobInterrupted = "interrupted"
)

type Job struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	Actor           string          `json:"actor"`
	State           string          `json:"state"`
	Progress        int             `json:"progress"`
	Message         string          `json:"message"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           string          `json:"error,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	StartedAt       time.Time       `json:"startedAt,omitempty"`
	CompletedAt     time.Time       `json:"completedAt,omitempty"`
	CancelRequested bool            `json:"cancelRequested"`
	Dangerous       bool            `json:"dangerous"`
}

var ErrJobNotFound = errors.New("job not found")
var ErrJobTerminal = errors.New("job is already complete")

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

func (store *Store) CreateJob(ctx context.Context, kind, actor string, dangerous bool) (Job, error) {
	if kind == "" || len(kind) > 64 || actor == "" || len(actor) > 256 {
		return Job{}, errors.New("invalid job")
	}
	id, err := receiptID()
	if err != nil {
		return Job{}, fmt.Errorf("create job id: %w", err)
	}
	now := time.Now().UTC()
	_, err = store.database.ExecContext(ctx, `
		INSERT INTO diagnostic_jobs
			(id, kind, actor, state, progress, message, result, error, created_at, started_at, completed_at, cancel_requested, dangerous)
		VALUES (?, ?, ?, ?, 0, ?, '', '', ?, '', '', 0, ?)
	`, id, kind, actor, JobPending, "Queued", now.Format(time.RFC3339Nano), dangerous)
	if err != nil {
		return Job{}, fmt.Errorf("create diagnostic job: %w", err)
	}
	return Job{ID: id, Kind: kind, Actor: actor, State: JobPending, Message: "Queued", CreatedAt: now, Dangerous: dangerous}, nil
}

func (store *Store) GetJob(ctx context.Context, id string) (Job, error) {
	var job Job
	var result, startedAt, completedAt, createdAt string
	var cancelRequested, dangerous int
	err := store.database.QueryRowContext(ctx, `
		SELECT id, kind, actor, state, progress, message, result, error, created_at, started_at, completed_at, cancel_requested, dangerous
		FROM diagnostic_jobs WHERE id = ?
	`, id).Scan(&job.ID, &job.Kind, &job.Actor, &job.State, &job.Progress, &job.Message, &result, &job.Error, &createdAt, &startedAt, &completedAt, &cancelRequested, &dangerous)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("read diagnostic job: %w", err)
	}
	if err := decodeJobTimes(&job, createdAt, startedAt, completedAt); err != nil {
		return Job{}, err
	}
	if result != "" {
		job.Result = json.RawMessage(result)
	}
	job.CancelRequested = cancelRequested != 0
	job.Dangerous = dangerous != 0
	return job, nil
}

func (store *Store) Jobs(ctx context.Context, limit int) ([]Job, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := store.database.QueryContext(ctx, `
		SELECT id, kind, actor, state, progress, message, result, error, created_at, started_at, completed_at, cancel_requested, dangerous
		FROM diagnostic_jobs ORDER BY created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list diagnostic jobs: %w", err)
	}
	defer rows.Close()
	items := make([]Job, 0, limit)
	for rows.Next() {
		var job Job
		var result, startedAt, completedAt, createdAt string
		var cancelRequested, dangerous int
		if err := rows.Scan(&job.ID, &job.Kind, &job.Actor, &job.State, &job.Progress, &job.Message, &result, &job.Error, &createdAt, &startedAt, &completedAt, &cancelRequested, &dangerous); err != nil {
			return nil, fmt.Errorf("read diagnostic job: %w", err)
		}
		if err := decodeJobTimes(&job, createdAt, startedAt, completedAt); err != nil {
			return nil, err
		}
		if result != "" {
			job.Result = json.RawMessage(result)
		}
		job.CancelRequested = cancelRequested != 0
		job.Dangerous = dangerous != 0
		items = append(items, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read diagnostic jobs: %w", err)
	}
	return items, nil
}

func (store *Store) StartJob(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := store.database.ExecContext(ctx, `
		UPDATE diagnostic_jobs SET state = ?, started_at = ?, message = ?, progress = 1
		WHERE id = ? AND state = ? AND cancel_requested = 0
	`, JobRunning, now, "Running", id, JobPending)
	if err != nil {
		return fmt.Errorf("start diagnostic job: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify diagnostic job start: %w", err)
	}
	if changed == 0 {
		job, getErr := store.GetJob(ctx, id)
		if errors.Is(getErr, ErrJobNotFound) {
			return ErrJobNotFound
		}
		if getErr != nil {
			return getErr
		}
		if job.State != JobPending || job.CancelRequested {
			return ErrJobTerminal
		}
		return errors.New("job could not be started")
	}
	return nil
}

func (store *Store) UpdateJobProgress(ctx context.Context, id string, progress int, message string) error {
	if progress < 0 || progress > 100 || len(message) > 256 {
		return errors.New("invalid job progress")
	}
	result, err := store.database.ExecContext(ctx, `
		UPDATE diagnostic_jobs SET progress = ?, message = ? WHERE id = ? AND state = ?
	`, progress, message, id, JobRunning)
	if err != nil {
		return fmt.Errorf("update diagnostic job progress: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify diagnostic job progress: %w", err)
	}
	if changed == 0 {
		return ErrJobTerminal
	}
	return nil
}

func (store *Store) CompleteJob(ctx context.Context, id string, result json.RawMessage) error {
	if len(result) > 1<<20 || (len(result) > 0 && !json.Valid(result)) {
		return errors.New("invalid job result")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	updated, err := store.database.ExecContext(ctx, `
		UPDATE diagnostic_jobs SET state = ?, progress = 100, message = ?, result = ?, completed_at = ?
		WHERE id = ? AND state = ? AND cancel_requested = 0
	`, JobSucceeded, "Completed", string(result), now, id, JobRunning)
	if err != nil {
		return fmt.Errorf("complete diagnostic job: %w", err)
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify diagnostic job completion: %w", err)
	}
	if changed == 0 {
		return ErrJobTerminal
	}
	return nil
}

func (store *Store) FailJob(ctx context.Context, id, message string, jobErr error) error {
	if message == "" || len(message) > 256 {
		return errors.New("invalid job failure")
	}
	if jobErr == nil {
		jobErr = errors.New("job failed")
	}
	publicError := jobErr.Error()
	if len(publicError) > 1024 {
		publicError = publicError[:1024]
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	updated, err := store.database.ExecContext(ctx, `
		UPDATE diagnostic_jobs SET state = ?, message = ?, error = ?, completed_at = ?
		WHERE id = ? AND state IN (?, ?)
	`, JobFailed, message, publicError, now, id, JobPending, JobRunning)
	if err != nil {
		return fmt.Errorf("fail diagnostic job: %w", err)
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify diagnostic job failure: %w", err)
	}
	if changed == 0 {
		return ErrJobTerminal
	}
	return nil
}

func (store *Store) CancelJob(ctx context.Context, id string) (Job, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	updated, err := store.database.ExecContext(ctx, `
		UPDATE diagnostic_jobs
		SET state = ?, cancel_requested = 1, message = ?, completed_at = ?
		WHERE id = ? AND state IN (?, ?)
	`, JobCanceled, "Canceled by operator", now, id, JobPending, JobRunning)
	if err != nil {
		return Job{}, fmt.Errorf("cancel diagnostic job: %w", err)
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return Job{}, fmt.Errorf("verify diagnostic job cancellation: %w", err)
	}
	if changed == 0 {
		job, getErr := store.GetJob(ctx, id)
		if errors.Is(getErr, ErrJobNotFound) {
			return Job{}, ErrJobNotFound
		}
		if getErr != nil {
			return Job{}, getErr
		}
		return job, ErrJobTerminal
	}
	return store.GetJob(ctx, id)
}

func (store *Store) RecoverJobs(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := store.database.ExecContext(ctx, `
		UPDATE diagnostic_jobs
		SET state = ?, message = ?, error = ?, completed_at = ?
		WHERE state IN (?, ?)
	`, JobInterrupted, "Interrupted by service restart", "Job was not retried after service restart", now, JobPending, JobRunning)
	if err != nil {
		return fmt.Errorf("recover diagnostic jobs: %w", err)
	}
	return nil
}

func decodeJobTimes(job *Job, createdAt, startedAt, completedAt string) error {
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return fmt.Errorf("parse job creation time: %w", err)
	}
	job.CreatedAt = parsed
	if startedAt != "" {
		job.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt)
		if err != nil {
			return fmt.Errorf("parse job start time: %w", err)
		}
	}
	if completedAt != "" {
		job.CompletedAt, err = time.Parse(time.RFC3339Nano, completedAt)
		if err != nil {
			return fmt.Errorf("parse job completion time: %w", err)
		}
	}
	return nil
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
		`CREATE TABLE diagnostic_jobs (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			actor TEXT NOT NULL,
			state TEXT NOT NULL CHECK (state IN ('pending', 'running', 'succeeded', 'failed', 'canceled', 'interrupted')),
			progress INTEGER NOT NULL CHECK (progress BETWEEN 0 AND 100),
			message TEXT NOT NULL,
			result TEXT NOT NULL,
			error TEXT NOT NULL,
			created_at TEXT NOT NULL,
			started_at TEXT NOT NULL,
			completed_at TEXT NOT NULL,
			cancel_requested INTEGER NOT NULL CHECK (cancel_requested IN (0, 1)),
			dangerous INTEGER NOT NULL CHECK (dangerous IN (0, 1))
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
