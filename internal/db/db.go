package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	Conn *sql.DB
	Path string
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	conn.SetMaxOpenConns(1)

	db := &DB{Conn: conn, Path: path}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.Conn.Close()
}

func (db *DB) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS catalog_queue (
			ia_identifier TEXT PRIMARY KEY,
			status        TEXT NOT NULL DEFAULT 'pending'
				CHECK(status IN ('pending','processing','completed','failed')),
			worker_id     TEXT,
			locked_at     TEXT,
			retry_count   INTEGER NOT NULL DEFAULT 0,
			error_message TEXT,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_catalog_queue_status ON catalog_queue(status)`,
		`CREATE INDEX IF NOT EXISTS idx_catalog_queue_locked ON catalog_queue(status, locked_at)`,

		`CREATE TABLE IF NOT EXISTS track_embeddings (
			ia_identifier TEXT PRIMARY KEY,
			embedding     BLOB NOT NULL,
			quality_score REAL,
			created_at    TEXT NOT NULL DEFAULT (datetime('now'))
		)`,

		`CREATE TABLE IF NOT EXISTS cursor_state (
			id             INTEGER PRIMARY KEY CHECK(id = 1),
			last_cursor    TEXT,
			items_indexed  INTEGER NOT NULL DEFAULT 0,
			last_run_at    TEXT
		)`,
		`INSERT OR IGNORE INTO cursor_state(id, last_cursor, items_indexed) VALUES(1, '', 0)`,
	}

	for _, q := range queries {
		if _, err := db.Conn.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q, err)
		}
	}

	return nil
}
