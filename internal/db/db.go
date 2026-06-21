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
	if err := db.dropLegacyTables(); err != nil {
		return fmt.Errorf("legacy cleanup: %w", err)
	}

	if err := db.migrateSchemaChanges(); err != nil {
		return fmt.Errorf("schema migration: %w", err)
	}

	queries := []string{
		`CREATE TABLE IF NOT EXISTS collections (
			collection_id    TEXT PRIMARY KEY,
			title            TEXT NOT NULL,
			description      TEXT,
			category         TEXT,
			curator          TEXT,
			url              TEXT,
			query            TEXT NOT NULL,
			expected_count   INTEGER NOT NULL DEFAULT 0,
			discovered_count INTEGER NOT NULL DEFAULT 0,
			status           TEXT NOT NULL DEFAULT 'pending'
				CHECK(status IN ('pending','discovering','discovered','failed')),
			last_cursor      TEXT,
			error_message    TEXT,
			last_synced_at   TEXT,
			created_at       TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
		)`,

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
			downloads     INTEGER NOT NULL DEFAULT 0,
			retry_count   INTEGER NOT NULL DEFAULT 0,
			prechecked    INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_albums_status ON albums(status)`,

		`CREATE TABLE IF NOT EXISTS collection_albums (
			collection_id TEXT NOT NULL REFERENCES collections(collection_id),
			album_id      TEXT NOT NULL REFERENCES albums(ia_identifier),
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (collection_id, album_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_albums_collection ON collection_albums(collection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_albums_album ON collection_albums(album_id)`,

		`CREATE TABLE IF NOT EXISTS tracks (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			album_id      TEXT NOT NULL REFERENCES albums(ia_identifier),
			filename      TEXT NOT NULL,
			title         TEXT,
			track_number  INTEGER,
			format        TEXT,
			bitrate       INTEGER,
			duration      REAL,
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
			track_id       INTEGER PRIMARY KEY REFERENCES tracks(id),
			clap           BLOB NOT NULL,
			mfcc           BLOB NOT NULL,
			chroma         BLOB NOT NULL,
			model_version  TEXT NOT NULL DEFAULT 'clap-htsat-fused:audio+text:512:l2:f16',
			dim            INTEGER NOT NULL DEFAULT 512,
			dtype          TEXT NOT NULL DEFAULT 'f16',
			quality_score  REAL,
			created_at     TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	}

	for _, q := range queries {
		if _, err := db.Conn.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q, err)
		}
	}

	return nil
}

func (db *DB) dropLegacyTables() error {
	for _, name := range []string{"cursor_state", "catalog_queue"} {
		if tableExists(db.Conn, name) {
			if _, err := db.Conn.Exec(fmt.Sprintf("DROP TABLE %s", name)); err != nil {
				return fmt.Errorf("drop %s: %w", name, err)
			}
		}
	}
	return nil
}

func (db *DB) migrateSchemaChanges() error {
	if tableExists(db.Conn, "track_embeddings") && columnExists(db.Conn, "track_embeddings", "embedding") {
		if _, err := db.Conn.Exec("DROP TABLE track_embeddings"); err != nil {
			return fmt.Errorf("drop old track_embeddings: %w", err)
		}
	}

	if tableExists(db.Conn, "tracks") && !columnExists(db.Conn, "tracks", "duration") {
		db.Conn.Exec("ALTER TABLE tracks ADD COLUMN duration REAL")
	}

	if tableExists(db.Conn, "albums") && !columnExists(db.Conn, "albums", "prechecked") {
		if _, err := db.Conn.Exec("ALTER TABLE albums ADD COLUMN prechecked INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("add prechecked column: %w", err)
		}
		if _, err := db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier IN (
			SELECT DISTINCT t.album_id FROM tracks t WHERE t.status = 'completed'
		)`); err != nil {
			return fmt.Errorf("backfill prechecked: %w", err)
		}
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

func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt *string
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}
