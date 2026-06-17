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

	conn.SetMaxOpenConns(8)

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
	if err := db.migrateFromOldSchema(); err != nil {
		return fmt.Errorf("legacy migration: %w", err)
	}

	queries := []string{
		`CREATE TABLE IF NOT EXISTS albums (
			ia_identifier TEXT PRIMARY KEY,
			title         TEXT,
			creator       TEXT,
			collection    TEXT,
			art_url       TEXT,
			track_count   INTEGER NOT NULL DEFAULT 0,
			status        TEXT NOT NULL DEFAULT 'pending'
				CHECK(status IN ('pending','resolving','resolved','failed')),
			error_message TEXT,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_albums_status ON albums(status)`,

		`CREATE TABLE IF NOT EXISTS tracks (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			album_id      TEXT NOT NULL REFERENCES albums(ia_identifier),
			filename      TEXT NOT NULL,
			title         TEXT,
			track_number  INTEGER,
			format        TEXT,
			bitrate       INTEGER,
			download_url  TEXT NOT NULL,
			status        TEXT NOT NULL DEFAULT 'pending'
				CHECK(status IN ('pending','processing','completed','failed')),
			worker_id     TEXT,
			locked_at     TEXT,
			retry_count   INTEGER NOT NULL DEFAULT 0,
			error_message TEXT,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(album_id, filename)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_status ON tracks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks(album_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_locked ON tracks(status, locked_at)`,

		`CREATE TABLE IF NOT EXISTS track_embeddings (
			track_id      INTEGER PRIMARY KEY REFERENCES tracks(id),
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

	db.Conn.Exec(`ALTER TABLE albums ADD COLUMN downloads INTEGER NOT NULL DEFAULT 0`)

	return nil
}

func (db *DB) migrateFromOldSchema() error {
	var tableName string
	err := db.Conn.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='catalog_queue'`,
	).Scan(&tableName)

	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check catalog_queue: %w", err)
	}

	tx, err := db.Conn.Begin()
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS albums (
		ia_identifier TEXT PRIMARY KEY,
		title         TEXT,
		creator       TEXT,
		collection    TEXT,
		art_url       TEXT,
		track_count   INTEGER NOT NULL DEFAULT 0,
		status        TEXT NOT NULL DEFAULT 'pending',
		error_message TEXT,
		created_at    TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return fmt.Errorf("create albums: %w", err)
	}

	_, err = tx.Exec(`INSERT OR IGNORE INTO albums(ia_identifier, status, created_at, updated_at)
		SELECT ia_identifier, 'pending', created_at, updated_at FROM catalog_queue`)
	if err != nil {
		return fmt.Errorf("migrate catalog_queue to albums: %w", err)
	}

	_, err = tx.Exec(`DROP TABLE IF EXISTS track_embeddings`)
	if err != nil {
		return fmt.Errorf("drop old track_embeddings: %w", err)
	}

	_, err = tx.Exec(`DROP TABLE IF EXISTS catalog_queue`)
	if err != nil {
		return fmt.Errorf("drop catalog_queue: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}

	return nil
}

func tableExists(db *sql.DB, name string) bool {
	var n string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	return err == nil
}
