// Package backup provides the backup runner and history database.
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

type Record struct {
	ID          int64
	RunID       string
	CreatedAt   time.Time
	Target      string
	Destination string
	Status      string // "success" | "failure"
	SizeBytes   int64
	DurationMs  int64
	ErrorMsg    string
	LogOutput   string
	Filename    string
}

type HistoryDB struct {
	db *sql.DB
}

func DefaultHistoryPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "backuper", "history.db")
}

func OpenHistoryDB(path string) (*HistoryDB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("creating history dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening history db: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating history db: %w", err)
	}
	return &HistoryDB{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS history (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id      TEXT NOT NULL DEFAULT '',
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		target      TEXT NOT NULL,
		destination TEXT NOT NULL,
		status      TEXT NOT NULL,
		size_bytes  INTEGER NOT NULL DEFAULT 0,
		duration_ms INTEGER NOT NULL DEFAULT 0,
		error_msg   TEXT NOT NULL DEFAULT '',
		log_output  TEXT NOT NULL DEFAULT '',
		filename    TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	// Migrate existing databases: add run_id column if missing.
	_, _ = db.Exec(`ALTER TABLE history ADD COLUMN run_id TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_history_run_id ON history(run_id)`)
	return nil
}

func (h *HistoryDB) Insert(ctx context.Context, r *Record) (int64, error) {
	res, err := h.db.ExecContext(ctx,
		`INSERT INTO history (run_id, created_at, target, destination, status, size_bytes, duration_ms, error_msg, log_output, filename)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RunID,
		r.CreatedAt.UTC().Format(time.RFC3339),
		r.Target, r.Destination, r.Status,
		r.SizeBytes, r.DurationMs,
		r.ErrorMsg, r.LogOutput, r.Filename,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting history record: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (h *HistoryDB) Query(ctx context.Context, targetFilter string, limit int) ([]*Record, error) {
	query := `SELECT id, run_id, created_at, target, destination, status, size_bytes, duration_ms, error_msg, log_output, filename
	          FROM history`
	var args []any
	if targetFilter != "" {
		query += " WHERE target = ?"
		args = append(args, targetFilter)
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close()

	return scanRecords(rows)
}

func (h *HistoryDB) GetByRunID(ctx context.Context, runID string) (*Record, error) {
	row := h.db.QueryRowContext(ctx,
		`SELECT id, run_id, created_at, target, destination, status, size_bytes, duration_ms, error_msg, log_output, filename
		 FROM history WHERE run_id = ?`, runID)
	var r Record
	var createdAt string
	if err := row.Scan(
		&r.ID, &r.RunID, &createdAt, &r.Target, &r.Destination,
		&r.Status, &r.SizeBytes, &r.DurationMs,
		&r.ErrorMsg, &r.LogOutput, &r.Filename,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying run %q: %w", runID, err)
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &r, nil
}

func scanRecords(rows *sql.Rows) ([]*Record, error) {
	var records []*Record
	for rows.Next() {
		var r Record
		var createdAt string
		if err := rows.Scan(
			&r.ID, &r.RunID, &createdAt, &r.Target, &r.Destination,
			&r.Status, &r.SizeBytes, &r.DurationMs,
			&r.ErrorMsg, &r.LogOutput, &r.Filename,
		); err != nil {
			return nil, fmt.Errorf("scanning history row: %w", err)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		records = append(records, &r)
	}
	return records, rows.Err()
}

func (h *HistoryDB) Close() error { return h.db.Close() }
