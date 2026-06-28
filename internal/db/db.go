package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	Conn *sql.DB
	Path string

	stopCheckpoint chan struct{}
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=10000&_wal_autocheckpoint=1000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite allows only one writer at a time. With a multi-connection pool,
	// concurrent writers from different worker goroutines collide and surface as
	// "database is locked" (SQLITE_BUSY) once the busy timeout expires. Pinning
	// the pool to a single connection serializes all access in Go so writes never
	// contend at the SQLite level. DB calls here are tiny; the expensive work
	// (network, decode, CLAP) happens outside the DB, so this is not a throughput
	// bottleneck.
	conn.SetMaxOpenConns(1)

	db := &DB{Conn: conn, Path: path, stopCheckpoint: make(chan struct{})}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	db.checkpointWAL()
	db.startCheckpointer()

	return db, nil
}

func (db *DB) Close() error {
	if db.stopCheckpoint != nil {
		close(db.stopCheckpoint)
		db.stopCheckpoint = nil
	}
	return db.Conn.Close()
}

// checkpointWAL forces a truncating WAL checkpoint. SQLite's passive
// auto-checkpoint silently no-ops whenever a reader is active, which under
// continuous read/write load lets the WAL grow without bound. An oversized WAL
// makes every page read do a linear walFindFrame scan, which pegs CPU and
// stalls writers. Forcing TRUNCATE periodically keeps the WAL bounded.
func (db *DB) checkpointWAL() {
	if _, err := db.Conn.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		log.Printf("[db] wal_checkpoint failed: %v", err)
	}
}

func (db *DB) startCheckpointer() {
	stop := db.stopCheckpoint
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				db.checkpointWAL()
			}
		}
	}()
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
			source_type      TEXT NOT NULL DEFAULT 'collection',
			list_name        TEXT,
			parent_id        TEXT,
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
				CHECK(status IN ('pending','resolving','resolved','failed','unavailable')),
			error_message TEXT,
			downloads     INTEGER NOT NULL DEFAULT 0,
			retry_count   INTEGER NOT NULL DEFAULT 0,
			prechecked    INTEGER NOT NULL DEFAULT 0,
			subjects      TEXT,
			genres        TEXT,
			license       TEXT,
			listenability_score     REAL,
			listenability_tier      TEXT,
			listenability_decision  TEXT,
			listenability_stream    TEXT,
			listenability_reasons   TEXT,
			listenability_components TEXT,
			listenability_version   TEXT,
			listenability_checked_at TEXT,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_albums_status ON albums(status)`,
		`CREATE INDEX IF NOT EXISTS idx_albums_listenability_score ON albums(listenability_score)`,

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
				CHECK(status IN ('pending','processing','completed','failed','unavailable')),
			worker_id     TEXT,
			locked_at     TEXT,
			retry_count   INTEGER NOT NULL DEFAULT 0,
			error_message TEXT,
			tags          TEXT,
			listenability_score     REAL,
			listenability_tier      TEXT,
			listenability_decision  TEXT,
			listenability_stream    TEXT,
			listenability_reasons   TEXT,
			listenability_components TEXT,
			listenability_version   TEXT,
			listenability_checked_at TEXT,
			listenability_worker_id TEXT,
			listenability_locked_at TEXT,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(album_id, filename)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_status ON tracks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks(album_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_locked ON tracks(status, locked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_listenability_work ON tracks(status, listenability_version, listenability_locked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_listenability_score ON tracks(listenability_score)`,

		`CREATE TABLE IF NOT EXISTS track_embeddings (
			track_id        INTEGER PRIMARY KEY REFERENCES tracks(id),
			clap            BLOB NOT NULL,
			mfcc            BLOB NOT NULL,
			chroma          BLOB NOT NULL,
			model_version   TEXT NOT NULL DEFAULT 'clap-htsat-fused:audio+text:512:l2:f16',
			dim             INTEGER NOT NULL DEFAULT 512,
			dtype           TEXT NOT NULL DEFAULT 'f16',
			sample_strategy TEXT NOT NULL DEFAULT 'head',
			quality_score   REAL,
			created_at      TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	}

	for _, q := range queries {
		if _, err := db.Conn.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q, err)
		}
	}

	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_tracks_listenability_work ON tracks(status, listenability_version, listenability_locked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_listenability_score ON tracks(listenability_score)`,
		`CREATE INDEX IF NOT EXISTS idx_albums_listenability_score ON albums(listenability_score)`,
	}
	for _, q := range indexQueries {
		if _, err := db.Conn.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", q, err)
		}
	}

	if err := db.migrateUnavailableCheckConstraint(); err != nil {
		return fmt.Errorf("unavailable check migration: %w", err)
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

	if tableExists(db.Conn, "albums") && !columnExists(db.Conn, "albums", "subjects") {
		if _, err := db.Conn.Exec("ALTER TABLE albums ADD COLUMN subjects TEXT"); err != nil {
			return fmt.Errorf("add subjects column: %w", err)
		}
	}

	if tableExists(db.Conn, "albums") && !columnExists(db.Conn, "albums", "genres") {
		if _, err := db.Conn.Exec("ALTER TABLE albums ADD COLUMN genres TEXT"); err != nil {
			return fmt.Errorf("add genres column: %w", err)
		}
	}

	if tableExists(db.Conn, "albums") && !columnExists(db.Conn, "albums", "license") {
		if _, err := db.Conn.Exec("ALTER TABLE albums ADD COLUMN license TEXT"); err != nil {
			return fmt.Errorf("add license column: %w", err)
		}
	}

	if tableExists(db.Conn, "tracks") && !columnExists(db.Conn, "tracks", "tags") {
		if _, err := db.Conn.Exec("ALTER TABLE tracks ADD COLUMN tags TEXT"); err != nil {
			return fmt.Errorf("add tags column: %w", err)
		}
	}

	if tableExists(db.Conn, "collections") && !columnExists(db.Conn, "collections", "source_type") {
		if _, err := db.Conn.Exec("ALTER TABLE collections ADD COLUMN source_type TEXT NOT NULL DEFAULT 'collection'"); err != nil {
			return fmt.Errorf("add source_type column: %w", err)
		}
	}

	if tableExists(db.Conn, "collections") && !columnExists(db.Conn, "collections", "list_name") {
		if _, err := db.Conn.Exec("ALTER TABLE collections ADD COLUMN list_name TEXT"); err != nil {
			return fmt.Errorf("add list_name column: %w", err)
		}
	}

	if tableExists(db.Conn, "collections") && !columnExists(db.Conn, "collections", "parent_id") {
		if _, err := db.Conn.Exec("ALTER TABLE collections ADD COLUMN parent_id TEXT"); err != nil {
			return fmt.Errorf("add parent_id column: %w", err)
		}
	}

	if tableExists(db.Conn, "track_embeddings") && !columnExists(db.Conn, "track_embeddings", "sample_strategy") {
		if _, err := db.Conn.Exec("ALTER TABLE track_embeddings ADD COLUMN sample_strategy TEXT NOT NULL DEFAULT 'head'"); err != nil {
			return fmt.Errorf("add sample_strategy column: %w", err)
		}
	}

	if tableExists(db.Conn, "tracks") && !columnExists(db.Conn, "tracks", "listenability_score") {
		cols := []struct{ name, typ string }{
			{"listenability_score", "REAL"},
			{"listenability_tier", "TEXT"},
			{"listenability_decision", "TEXT"},
			{"listenability_stream", "TEXT"},
			{"listenability_reasons", "TEXT"},
			{"listenability_components", "TEXT"},
			{"listenability_version", "TEXT"},
			{"listenability_checked_at", "TEXT"},
			{"listenability_worker_id", "TEXT"},
			{"listenability_locked_at", "TEXT"},
		}
		for _, c := range cols {
			if _, err := db.Conn.Exec(fmt.Sprintf("ALTER TABLE tracks ADD COLUMN %s %s", c.name, c.typ)); err != nil {
				return fmt.Errorf("add tracks.%s: %w", c.name, err)
			}
		}
	}

	if tableExists(db.Conn, "albums") && !columnExists(db.Conn, "albums", "listenability_score") {
		cols := []struct{ name, typ string }{
			{"listenability_score", "REAL"},
			{"listenability_tier", "TEXT"},
			{"listenability_decision", "TEXT"},
			{"listenability_stream", "TEXT"},
			{"listenability_reasons", "TEXT"},
			{"listenability_components", "TEXT"},
			{"listenability_version", "TEXT"},
			{"listenability_checked_at", "TEXT"},
		}
		for _, c := range cols {
			if _, err := db.Conn.Exec(fmt.Sprintf("ALTER TABLE albums ADD COLUMN %s %s", c.name, c.typ)); err != nil {
				return fmt.Errorf("add albums.%s: %w", c.name, err)
			}
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

func checkConstraintHasStatus(sqlDB *sql.DB, table, status string) (bool, error) {
	var createSQL string
	err := sqlDB.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&createSQL)
	if err != nil {
		return false, fmt.Errorf("check constraint %s: %w", table, err)
	}
	return strings.Contains(createSQL, fmt.Sprintf("'%s'", status)), nil
}

func (db *DB) migrateUnavailableCheckConstraint() error {
	hasAlbums, err := checkConstraintHasStatus(db.Conn, "albums", "unavailable")
	if err != nil {
		return err
	}
	hasTracks, err := checkConstraintHasStatus(db.Conn, "tracks", "unavailable")
	if err != nil {
		return err
	}
	if hasAlbums && hasTracks {
		return nil
	}

	db.Conn.Exec("PRAGMA foreign_keys = OFF")
	defer db.Conn.Exec("PRAGMA foreign_keys = ON")

	if !hasAlbums && tableExists(db.Conn, "albums") {
		if err := recreateTableWithUnavailable(db.Conn, "albums"); err != nil {
			return fmt.Errorf("migrate albums: %w", err)
		}
	}
	if !hasTracks && tableExists(db.Conn, "tracks") {
		if err := recreateTableWithUnavailable(db.Conn, "tracks"); err != nil {
			return fmt.Errorf("migrate tracks: %w", err)
		}
	}
	return nil
}

func recreateTableWithUnavailable(sqlDB *sql.DB, table string) error {
	switch table {
	case "albums":
		return recreateAlbumsWithUnavailable(sqlDB)
	case "tracks":
		return recreateTracksWithUnavailable(sqlDB)
	}
	return fmt.Errorf("unknown table: %s", table)
}

func recreateAlbumsWithUnavailable(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
		CREATE TABLE albums_new (
			ia_identifier TEXT PRIMARY KEY,
			title         TEXT,
			creator       TEXT,
			collection    TEXT,
			art_url       TEXT,
			track_count   INTEGER NOT NULL DEFAULT 0,
			status        TEXT NOT NULL DEFAULT 'pending'
				CHECK(status IN ('pending','resolving','resolved','failed','unavailable')),
			error_message TEXT,
			downloads     INTEGER NOT NULL DEFAULT 0,
			retry_count   INTEGER NOT NULL DEFAULT 0,
			prechecked    INTEGER NOT NULL DEFAULT 0,
			subjects      TEXT,
			genres        TEXT,
			license       TEXT,
			listenability_score     REAL,
			listenability_tier      TEXT,
			listenability_decision  TEXT,
			listenability_stream    TEXT,
			listenability_reasons   TEXT,
			listenability_components TEXT,
			listenability_version   TEXT,
			listenability_checked_at TEXT,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
		)`)
	if err != nil {
		return fmt.Errorf("create albums_new: %w", err)
	}
	_, err = sqlDB.Exec(`INSERT INTO albums_new(ia_identifier, title, creator, collection, art_url, track_count, status, error_message, downloads, retry_count, prechecked, subjects, genres, license, listenability_score, listenability_tier, listenability_decision, listenability_stream, listenability_reasons, listenability_components, listenability_version, listenability_checked_at, created_at, updated_at)
		SELECT ia_identifier, title, creator, collection, art_url, track_count, status, error_message, downloads, retry_count, prechecked, subjects, genres, license, listenability_score, listenability_tier, listenability_decision, listenability_stream, listenability_reasons, listenability_components, listenability_version, listenability_checked_at, created_at, updated_at FROM albums`)
	if err != nil {
		return fmt.Errorf("copy albums: %w", err)
	}
	_, err = sqlDB.Exec(`DROP TABLE albums`)
	if err != nil {
		return fmt.Errorf("drop albums: %w", err)
	}
	_, err = sqlDB.Exec(`ALTER TABLE albums_new RENAME TO albums`)
	if err != nil {
		return fmt.Errorf("rename albums_new: %w", err)
	}
	_, err = sqlDB.Exec(`CREATE INDEX IF NOT EXISTS idx_albums_status ON albums(status)`)
	if err != nil {
		return fmt.Errorf("reindex albums: %w", err)
	}
	return nil
}

func recreateTracksWithUnavailable(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
		CREATE TABLE tracks_new (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			album_id      TEXT NOT NULL,
			filename      TEXT NOT NULL,
			title         TEXT,
			track_number  INTEGER,
			format        TEXT,
			bitrate       INTEGER,
			duration      REAL,
			download_url  TEXT NOT NULL,
			status        TEXT NOT NULL DEFAULT 'pending'
				CHECK(status IN ('pending','processing','completed','failed','unavailable')),
			worker_id     TEXT,
			locked_at     TEXT,
			retry_count   INTEGER NOT NULL DEFAULT 0,
			error_message TEXT,
			tags          TEXT,
			listenability_score     REAL,
			listenability_tier      TEXT,
			listenability_decision  TEXT,
			listenability_stream    TEXT,
			listenability_reasons   TEXT,
			listenability_components TEXT,
			listenability_version   TEXT,
			listenability_checked_at TEXT,
			listenability_worker_id TEXT,
			listenability_locked_at TEXT,
			created_at    TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(album_id, filename)
		)`)
	if err != nil {
		return fmt.Errorf("create tracks_new: %w", err)
	}
	_, err = sqlDB.Exec(`INSERT INTO tracks_new(id, album_id, filename, title, track_number, format, bitrate, duration, download_url, status, worker_id, locked_at, retry_count, error_message, tags, listenability_score, listenability_tier, listenability_decision, listenability_stream, listenability_reasons, listenability_components, listenability_version, listenability_checked_at, listenability_worker_id, listenability_locked_at, created_at, updated_at)
		SELECT id, album_id, filename, title, track_number, format, bitrate, duration, download_url, status, worker_id, locked_at, retry_count, error_message, tags, listenability_score, listenability_tier, listenability_decision, listenability_stream, listenability_reasons, listenability_components, listenability_version, listenability_checked_at, listenability_worker_id, listenability_locked_at, created_at, updated_at FROM tracks`)
	if err != nil {
		return fmt.Errorf("copy tracks: %w", err)
	}
	_, err = sqlDB.Exec(`DROP TABLE tracks`)
	if err != nil {
		return fmt.Errorf("drop tracks: %w", err)
	}
	_, err = sqlDB.Exec(`ALTER TABLE tracks_new RENAME TO tracks`)
	if err != nil {
		return fmt.Errorf("rename tracks_new: %w", err)
	}
	_, err = sqlDB.Exec(`CREATE INDEX IF NOT EXISTS idx_tracks_status ON tracks(status)`)
	if err != nil {
		return fmt.Errorf("reindex tracks status: %w", err)
	}
	_, err = sqlDB.Exec(`CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks(album_id)`)
	if err != nil {
		return fmt.Errorf("reindex tracks album: %w", err)
	}
	_, err = sqlDB.Exec(`CREATE INDEX IF NOT EXISTS idx_tracks_locked ON tracks(status, locked_at)`)
	if err != nil {
		return fmt.Errorf("reindex tracks locked: %w", err)
	}
	return nil
}
