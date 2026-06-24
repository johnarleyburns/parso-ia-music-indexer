package db

import (
	"database/sql"
	"math"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.Remove(path)
	})
	return db
}

func testAlbumInserts(ids ...string) []AlbumInsert {
	result := make([]AlbumInsert, len(ids))
	for i, id := range ids {
		result[i] = AlbumInsert{Identifier: id}
	}
	return result
}

func TestOpenMigrate(t *testing.T) {
	db := testDB(t)

	var count int
	if err := db.Conn.QueryRow(`SELECT count(*) FROM albums`).Scan(&count); err != nil {
		t.Fatalf("query albums: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 albums, got %d", count)
	}

	if err := db.Conn.QueryRow(`SELECT count(*) FROM tracks`).Scan(&count); err != nil {
		t.Fatalf("query tracks: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tracks, got %d", count)
	}

	if err := db.Conn.QueryRow(`SELECT count(*) FROM collections`).Scan(&count); err != nil {
		t.Fatalf("query collections: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 collections, got %d", count)
	}

	if err := db.migrate(); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
}

func TestDropLegacyTables(t *testing.T) {
	path := t.TempDir() + "/migrate_test.db"

	conn, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Exec(`CREATE TABLE catalog_queue (
		ia_identifier TEXT PRIMARY KEY,
		status TEXT NOT NULL DEFAULT 'pending',
		worker_id TEXT, locked_at TEXT, retry_count INTEGER NOT NULL DEFAULT 0,
		error_message TEXT,
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	conn.Exec(`INSERT INTO catalog_queue(ia_identifier) VALUES('album-a'),('album-b'),('album-c')`)
	conn.Exec(`CREATE TABLE cursor_state (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		last_cursor TEXT, items_indexed INTEGER NOT NULL DEFAULT 0, last_run_at TEXT
	)`)
	conn.Exec(`INSERT INTO cursor_state(id, last_cursor, items_indexed) VALUES(1, 'abc', 42)`)
	conn.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open after migration: %v", err)
	}
	defer db.Close()

	if tableExists(db.Conn, "catalog_queue") {
		t.Error("catalog_queue should have been dropped")
	}
	if tableExists(db.Conn, "cursor_state") {
		t.Error("cursor_state should have been dropped")
	}
}

func openRaw(path string) (*sql.DB, error) {
	return sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
}

func TestGetCombinedStatsEmpty(t *testing.T) {
	db := testDB(t)
	stats, err := GetCombinedStats(db.Conn)
	if err != nil {
		t.Fatalf("GetCombinedStats: %v", err)
	}
	if stats.Albums.Total != 0 || stats.Tracks.Total != 0 {
		t.Errorf("expected all zeros, got albums=%+v tracks=%+v", stats.Albums, stats.Tracks)
	}
}

func TestBulkInsertAlbums(t *testing.T) {
	db := testDB(t)
	n, err := BulkInsertAlbums(db.Conn, testAlbumInserts("album-a", "album-b", "album-c"))
	if err != nil {
		t.Fatalf("BulkInsertAlbums: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 inserted, got %d", n)
	}

	stats, err := GetCombinedStats(db.Conn)
	if err != nil {
		t.Fatalf("GetCombinedStats: %v", err)
	}
	if stats.Albums.Total != 3 || stats.Albums.Pending != 3 {
		t.Errorf("expected 3 total/pending albums, got %+v", stats.Albums)
	}

	n, err = BulkInsertAlbums(db.Conn, testAlbumInserts("album-a", "album-d"))
	if err != nil {
		t.Fatalf("BulkInsertAlbums: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 new, got %d", n)
	}
}

func TestClaimUnresolvedAlbum(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a", "album-b"))

	id, err := ClaimUnresolvedAlbum(db.Conn, "w1")
	if err != nil {
		t.Fatalf("ClaimUnresolvedAlbum: %v", err)
	}
	if id != "album-a" {
		t.Errorf("expected album-a, got %s", id)
	}

	id2, err := ClaimUnresolvedAlbum(db.Conn, "w2")
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "album-b" {
		t.Errorf("expected album-b, got %s", id2)
	}

	id3, err := ClaimUnresolvedAlbum(db.Conn, "w3")
	if err != nil {
		t.Fatal(err)
	}
	if id3 != "" {
		t.Errorf("expected empty (no more), got %s", id3)
	}
}

func TestMarkAlbumResolved(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	ClaimUnresolvedAlbum(db.Conn, "w1")

	err := MarkAlbumResolved(db.Conn, "album-a", "Test Album", "Artist", "etree", "https://archive.org/services/img/album-a", 5)
	if err != nil {
		t.Fatalf("MarkAlbumResolved: %v", err)
	}

	album, err := GetAlbumByID(db.Conn, "album-a")
	if err != nil {
		t.Fatalf("GetAlbumByID: %v", err)
	}
	if album.Title != "Test Album" || album.Creator != "Artist" || album.TrackCount != 5 || album.Status != "resolved" {
		t.Errorf("unexpected album: %+v", album)
	}
}

func TestInsertTracksAndClaim(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	MarkAlbumResolved(db.Conn, "album-a", "Album A", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'album-a'`)

	tracks := []TrackInsert{
		{Filename: "01-song.mp3", Title: "Song One", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/01.mp3"},
		{Filename: "02-song.mp3", Title: "Song Two", TrackNumber: 2, Format: "VBR MP3", DownloadURL: "https://example.com/02.mp3"},
		{Filename: "03-song.mp3", Title: "Song Three", TrackNumber: 3, Format: "MP3", Bitrate: 192, DownloadURL: "https://example.com/03.mp3"},
	}

	n, err := InsertTracks(db.Conn, "album-a", tracks)
	if err != nil {
		t.Fatalf("InsertTracks: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 inserted, got %d", n)
	}

	claimed, err := ClaimNextTrackBatch(db.Conn, "w1", 2)
	if err != nil {
		t.Fatalf("ClaimNextTrackBatch: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("expected 2 claimed, got %d", len(claimed))
	}
	if claimed[0].Title != "Song One" || claimed[1].Title != "Song Two" {
		t.Errorf("unexpected tracks: %+v, %+v", claimed[0], claimed[1])
	}

	stats, _ := GetCombinedStats(db.Conn)
	if stats.Tracks.Processing != 2 || stats.Tracks.Pending != 1 {
		t.Errorf("expected 2 processing, 1 pending, got %+v", stats.Tracks)
	}

	err = MarkTrackCompleted(db.Conn, claimed[0].ID)
	if err != nil {
		t.Fatalf("MarkTrackCompleted: %v", err)
	}
	stats, _ = GetCombinedStats(db.Conn)
	if stats.Tracks.Completed != 1 {
		t.Errorf("expected 1 completed, got %+v", stats.Tracks)
	}
}

func TestMarkTrackFailed(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	MarkAlbumResolved(db.Conn, "album-a", "", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'album-a'`)
	InsertTracks(db.Conn, "album-a", []TrackInsert{
		{Filename: "song.mp3", Title: "Song", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/song.mp3"},
	})

	claimed, _ := ClaimNextTrackBatch(db.Conn, "w1", 1)
	if err := MarkTrackFailed(db.Conn, claimed[0].ID, "test error"); err != nil {
		t.Fatalf("MarkTrackFailed: %v", err)
	}

	stats, _ := GetCombinedStats(db.Conn)
	if stats.Tracks.Failed != 1 {
		t.Errorf("expected 1 failed, got %+v", stats.Tracks)
	}
}

func TestMarkTrackUnavailable(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	MarkAlbumResolved(db.Conn, "album-a", "", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'album-a'`)
	InsertTracks(db.Conn, "album-a", []TrackInsert{
		{Filename: "song.mp3", Title: "Song", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/song.mp3"},
	})

	claimed, _ := ClaimNextTrackBatch(db.Conn, "w1", 1)
	if err := MarkTrackUnavailable(db.Conn, claimed[0].ID, "permanent issue"); err != nil {
		t.Fatalf("MarkTrackUnavailable: %v", err)
	}

	stats, _ := GetCombinedStats(db.Conn)
	if stats.Tracks.Unavailable != 1 {
		t.Errorf("expected 1 unavailable, got %+v", stats.Tracks)
	}
	if stats.Tracks.Failed != 0 {
		t.Errorf("expected 0 failed, got %+v", stats.Tracks)
	}
}

func TestMarkAlbumUnavailable(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	ClaimUnresolvedAlbum(db.Conn, "w1")

	err := MarkAlbumUnavailable(db.Conn, "album-a", "no acceptable MP3 tracks")
	if err != nil {
		t.Fatalf("MarkAlbumUnavailable: %v", err)
	}

	stats, _ := GetCombinedStats(db.Conn)
	if stats.Albums.Unavailable != 1 {
		t.Errorf("expected 1 unavailable album, got %+v", stats.Albums)
	}
	if stats.Albums.Failed != 0 {
		t.Errorf("expected 0 failed albums, got %+v", stats.Albums)
	}
}

func TestFailAlbumAndPendingTracksUnavailable(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	MarkAlbumResolved(db.Conn, "album-a", "", "", "", "", 0)
	InsertTracks(db.Conn, "album-a", []TrackInsert{
		{Filename: "a.mp3", Title: "A", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/a.mp3"},
		{Filename: "b.mp3", Title: "B", TrackNumber: 2, Format: "VBR MP3", DownloadURL: "https://example.com/b.mp3"},
	})

	skipped, err := FailAlbumAndPendingTracksUnavailable(db.Conn, "album-a", "access-restricted")
	if err != nil {
		t.Fatalf("FailAlbumAndPendingTracksUnavailable: %v", err)
	}
	if skipped != 2 {
		t.Errorf("expected 2 skipped pending tracks, got %d", skipped)
	}

	stats, _ := GetCombinedStats(db.Conn)
	if stats.Albums.Unavailable != 1 {
		t.Errorf("expected 1 unavailable album, got %+v", stats.Albums)
	}
	if stats.Tracks.Unavailable != 2 {
		t.Errorf("expected 2 unavailable tracks, got %+v", stats.Tracks)
	}
}

func TestFlagAlbumPoorQualityMarksAsUnavailable(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	MarkAlbumResolved(db.Conn, "album-a", "", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'album-a'`)
	InsertTracks(db.Conn, "album-a", []TrackInsert{
		{Filename: "good.mp3", Title: "Good", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/good.mp3"},
		{Filename: "bad.mp3", Title: "Bad", TrackNumber: 2, Format: "VBR MP3", DownloadURL: "https://example.com/bad.mp3"},
	})

	claimed, _ := ClaimNextTrackBatch(db.Conn, "w1", 1)
	skipped, err := FlagAlbumPoorQuality(db.Conn, claimed[0].ID, "album-a", "decode failure")
	if err != nil {
		t.Fatalf("FlagAlbumPoorQuality: %v", err)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped pending track, got %d", skipped)
	}

	stats, _ := GetCombinedStats(db.Conn)
	if stats.Albums.Unavailable != 1 {
		t.Errorf("expected 1 unavailable album, got %+v", stats.Albums)
	}
	if stats.Albums.Failed != 0 {
		t.Errorf("expected 0 failed albums, got %+v", stats.Albums)
	}
	if stats.Tracks.Unavailable != 2 {
		t.Errorf("expected 2 unavailable tracks (specific + pending), got %+v", stats.Tracks)
	}
	if stats.Tracks.Failed != 0 {
		t.Errorf("expected 0 failed tracks, got %+v", stats.Tracks)
	}
}

func TestResetAllFailedDoesNotResetUnavailable(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-fail", "album-unavail"))
	MarkAlbumResolved(db.Conn, "album-fail", "", "", "", "", 0)
	MarkAlbumResolved(db.Conn, "album-unavail", "", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier IN ('album-fail', 'album-unavail')`)
	InsertTracks(db.Conn, "album-fail", []TrackInsert{
		{Filename: "fail.mp3", Title: "Fail", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/fail.mp3"},
	})
	InsertTracks(db.Conn, "album-unavail", []TrackInsert{
		{Filename: "unavail.mp3", Title: "Unavail", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/unavail.mp3"},
	})

	claimed1, _ := ClaimNextTrackBatch(db.Conn, "w1", 1)
	claimed2, _ := ClaimNextTrackBatch(db.Conn, "w2", 1)
	MarkTrackFailed(db.Conn, claimed1[0].ID, "stream error")
	MarkTrackUnavailable(db.Conn, claimed2[0].ID, "low quality")
	MarkAlbumFailed(db.Conn, "album-fail", "stream error")
	MarkAlbumUnavailable(db.Conn, "album-unavail", "no acceptable MP3s")

	albumCount, trackCount, err := ResetAllFailed(db.Conn)
	if err != nil {
		t.Fatalf("ResetAllFailed: %v", err)
	}
	if albumCount != 1 {
		t.Errorf("expected 1 album reset, got %d", albumCount)
	}
	if trackCount != 1 {
		t.Errorf("expected 1 track reset, got %d", trackCount)
	}

	stats, _ := GetCombinedStats(db.Conn)
	if stats.Albums.Pending != 1 || stats.Albums.Unavailable != 1 {
		t.Errorf("expected 1 pending + 1 unavailable album, got %+v", stats.Albums)
	}
	if stats.Tracks.Pending != 1 || stats.Tracks.Unavailable != 1 {
		t.Errorf("expected 1 pending + 1 unavailable track, got %+v", stats.Tracks)
	}
}

func TestGetCombinedStatsUnavailable(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a", "album-b"))
	ClaimUnresolvedAlbum(db.Conn, "w1")
	MarkAlbumUnavailable(db.Conn, "album-a", "no MP3s")
	MarkAlbumResolved(db.Conn, "album-b", "Album B", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'album-b'`)
	InsertTracks(db.Conn, "album-b", []TrackInsert{
		{Filename: "a.mp3", Title: "A", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/a.mp3"},
		{Filename: "b.mp3", Title: "B", TrackNumber: 2, Format: "VBR MP3", DownloadURL: "https://example.com/b.mp3"},
	})
	claimed, _ := ClaimNextTrackBatch(db.Conn, "w1", 2)
	MarkTrackUnavailable(db.Conn, claimed[0].ID, "poor quality")
	MarkTrackCompleted(db.Conn, claimed[1].ID)

	stats, err := GetCombinedStats(db.Conn)
	if err != nil {
		t.Fatalf("GetCombinedStats: %v", err)
	}
	if stats.Albums.Unavailable != 1 || stats.Albums.Total != 2 {
		t.Errorf("albums: expected 1 unavailable, 2 total, got %+v", stats.Albums)
	}
	if stats.Tracks.Unavailable != 1 || stats.Tracks.Completed != 1 || stats.Tracks.Total != 2 {
		t.Errorf("tracks: expected 1 unavailable, 1 completed, 2 total, got %+v", stats.Tracks)
	}
}

func TestCheckConstraintMigration(t *testing.T) {
	path := t.TempDir() + "/migrate_check.db"

	conn, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Exec(`CREATE TABLE IF NOT EXISTS albums (
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
	)`)
	conn.Exec(`CREATE TABLE IF NOT EXISTS tracks (
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
			CHECK(status IN ('pending','processing','completed','failed')),
		worker_id     TEXT,
		locked_at     TEXT,
		retry_count   INTEGER NOT NULL DEFAULT 0,
		error_message TEXT,
		created_at    TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
		UNIQUE(album_id, filename)
	)`)
	conn.Exec(`INSERT INTO albums(ia_identifier, title, status) VALUES('old-album', 'Old Album', 'pending')`)
	conn.Exec(`INSERT INTO tracks(album_id, filename, download_url, status) VALUES('old-album', 'old.mp3', 'https://example.com/old.mp3', 'pending')`)
	conn.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open after check constraint migration: %v", err)
	}
	defer db.Close()

	hasAlbums, err := checkConstraintHasStatus(db.Conn, "albums", "unavailable")
	if err != nil {
		t.Fatalf("check albums constraint: %v", err)
	}
	if !hasAlbums {
		t.Error("albums CHECK constraint should include 'unavailable' after migration")
	}

	hasTracks, err := checkConstraintHasStatus(db.Conn, "tracks", "unavailable")
	if err != nil {
		t.Fatalf("check tracks constraint: %v", err)
	}
	if !hasTracks {
		t.Error("tracks CHECK constraint should include 'unavailable' after migration")
	}

	var title string
	if err := db.Conn.QueryRow(`SELECT title FROM albums WHERE ia_identifier='old-album'`).Scan(&title); err != nil {
		t.Fatalf("query old album: %v", err)
	}
	if title != "Old Album" {
		t.Errorf("expected 'Old Album', got %q", title)
	}

	var filename string
	if err := db.Conn.QueryRow(`SELECT filename FROM tracks WHERE album_id='old-album'`).Scan(&filename); err != nil {
		t.Fatalf("query old track: %v", err)
	}
	if filename != "old.mp3" {
		t.Errorf("expected 'old.mp3', got %q", filename)
	}

	db.Conn.Exec(`INSERT INTO albums(ia_identifier, status) VALUES('new-unavail', 'unavailable')`)
	stats, _ := GetCombinedStats(db.Conn)
	if stats.Albums.Unavailable != 1 {
		t.Errorf("expected 1 unavailable album after insert, got %d", stats.Albums.Unavailable)
	}

	db.Conn.Exec(`INSERT INTO tracks(album_id, filename, download_url, status) VALUES('old-album', 'new.mp3', 'https://example.com/new.mp3', 'unavailable')`)
	stats, _ = GetCombinedStats(db.Conn)
	if stats.Tracks.Unavailable != 1 {
		t.Errorf("expected 1 unavailable track after insert, got %d", stats.Tracks.Unavailable)
	}
}

func TestResetStuckTracks(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	MarkAlbumResolved(db.Conn, "album-a", "", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'album-a'`)
	InsertTracks(db.Conn, "album-a", []TrackInsert{
		{Filename: "a.mp3", Title: "A", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/a.mp3"},
		{Filename: "b.mp3", Title: "B", TrackNumber: 2, Format: "VBR MP3", DownloadURL: "https://example.com/b.mp3"},
	})
	ClaimNextTrackBatch(db.Conn, "w1", 2)

	db.Conn.Exec(`UPDATE tracks SET locked_at='2020-01-01T00:00:00Z' WHERE filename='a.mp3'`)

	n, err := ResetStuckTracks(db.Conn, 5*time.Minute)
	if err != nil {
		t.Fatalf("ResetStuckTracks: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 reset, got %d", n)
	}

	stats, _ := GetCombinedStats(db.Conn)
	if stats.Tracks.Pending != 1 || stats.Tracks.Processing != 1 {
		t.Errorf("expected 1 pending, 1 processing, got %+v", stats.Tracks)
	}
}

func makeTestClap(fill float32) []float32 {
	v := make([]float32, 512)
	for i := range v {
		v[i] = fill
	}
	return v
}

func makeTestMfcc(fill float32) []float32 {
	v := make([]float32, 40)
	for i := range v {
		v[i] = fill
	}
	return v
}

func makeTestChroma(fill float32) []float32 {
	v := make([]float32, 12)
	for i := range v {
		v[i] = fill
	}
	return v
}

func setupTrackWithEmbedding(t *testing.T, db *DB, albumID, filename, title string, trackNum int, clap, mfcc, chroma []float32, quality float64) int {
	t.Helper()
	BulkInsertAlbums(db.Conn, testAlbumInserts(albumID))
	MarkAlbumResolved(db.Conn, albumID, albumID, "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = ?`, albumID)
	InsertTracks(db.Conn, albumID, []TrackInsert{
		{Filename: filename, Title: title, TrackNumber: trackNum, Format: "VBR MP3", DownloadURL: "https://example.com/" + filename},
	})
	claimed, _ := ClaimNextTrackBatch(db.Conn, "test", 1)
	if len(claimed) == 0 {
		t.Fatal("no tracks claimed")
	}
	MarkTrackCompleted(db.Conn, claimed[0].ID)
	SaveEmbedding(db.Conn, claimed[0].ID, clap, mfcc, chroma, quality)
	return claimed[0].ID
}

func TestEmbeddingRoundtrip(t *testing.T) {
	db := testDB(t)

	clap := make([]float32, 512)
	mfcc := make([]float32, 40)
	chroma := make([]float32, 12)
	for i := range clap {
		clap[i] = float32(i-256) / 512.0
	}
	for i := range mfcc {
		mfcc[i] = float32(i) * 0.1
	}
	for i := range chroma {
		chroma[i] = float32(i) * 0.08
	}

	trackID := setupTrackWithEmbedding(t, db, "album-a", "song.mp3", "Song", 1, clap, mfcc, chroma, 24.5)

	gotClap, gotMfcc, gotChroma, qs, err := GetEmbedding(db.Conn, trackID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(gotClap) != 512 {
		t.Errorf("expected 512 clap dims, got %d", len(gotClap))
	}
	if len(gotMfcc) != 40 {
		t.Errorf("expected 40 mfcc dims, got %d", len(gotMfcc))
	}
	if len(gotChroma) != 12 {
		t.Errorf("expected 12 chroma dims, got %d", len(gotChroma))
	}
	if qs != 24.5 {
		t.Errorf("expected quality 24.5, got %f", qs)
	}
}

func TestEmbeddingBlobSizes(t *testing.T) {
	db := testDB(t)

	clap := makeTestClap(0.5)
	mfcc := makeTestMfcc(0.5)
	chroma := makeTestChroma(0.5)

	setupTrackWithEmbedding(t, db, "album-sz", "sz.mp3", "Size", 1, clap, mfcc, chroma, 0.5)

	var clapBlob, mfccBlob, chromaBlob []byte
	db.Conn.QueryRow(`SELECT clap, mfcc, chroma FROM track_embeddings WHERE track_id = 1`).
		Scan(&clapBlob, &mfccBlob, &chromaBlob)

	if len(clapBlob) != 1024 {
		t.Errorf("expected 1024 bytes for clap f16, got %d", len(clapBlob))
	}
	if len(mfccBlob) != 80 {
		t.Errorf("expected 80 bytes for mfcc f16, got %d", len(mfccBlob))
	}
	if len(chromaBlob) != 24 {
		t.Errorf("expected 24 bytes for chroma f16, got %d", len(chromaBlob))
	}
}

func TestQuerySimilar(t *testing.T) {
	db := testDB(t)

	clap1 := makeTestClap(1)
	clap2 := makeTestClap(1)
	clap3 := makeTestClap(-1)
	clap4 := makeTestClap(0.5)
	mfcc := makeTestMfcc(0)
	chroma := makeTestChroma(0)

	id1 := setupTrackWithEmbedding(t, db, "album-1", "t1.mp3", "Track 1", 1, clap1, mfcc, chroma, 30.0)
	setupTrackWithEmbedding(t, db, "album-2", "t2.mp3", "Track 2", 1, clap2, mfcc, chroma, 25.0)
	setupTrackWithEmbedding(t, db, "album-3", "t3.mp3", "Track 3", 1, clap3, mfcc, chroma, 15.0)
	setupTrackWithEmbedding(t, db, "album-4", "t4.mp3", "Track 4", 1, clap4, mfcc, chroma, 20.0)

	results, err := QuerySimilar(db.Conn, id1, 5)
	if err != nil {
		t.Fatalf("QuerySimilar: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].Title != "Track 2" {
		t.Errorf("expected Track 2 closest, got %s (dist=%f)", results[0].Title, results[0].Distance)
	}
	if results[0].Distance > 0.01 {
		t.Errorf("expected distance ~0 for identical, got %f", results[0].Distance)
	}
	if results[2].Title != "Track 3" {
		t.Errorf("expected Track 3 farthest, got %s (dist=%f)", results[2].Title, results[2].Distance)
	}
	if results[2].Distance < 1.5 {
		t.Errorf("expected distance ~2 for opposite, got %f", results[2].Distance)
	}
}

func TestCosDistanceSelf(t *testing.T) {
	v := []float32{1, 2, 3, 4, 5}
	d := cosineDistance(v, v)
	if d > 0.00001 {
		t.Errorf("self distance should be 0, got %f", d)
	}
}

func TestCosDistanceOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	d := cosineDistance(a, b)
	if d < 0.999 || d > 1.001 {
		t.Errorf("orthogonal distance should be ~1, got %f", d)
	}
}

func TestEncodeDecodeF32(t *testing.T) {
	orig := []float32{1.5, -2.3, 0.0, math.MaxFloat32, -math.MaxFloat32}
	blob := encodeF32(orig)
	decoded := decodeF32(blob)
	for i := range orig {
		if decoded[i] != orig[i] {
			t.Errorf("dim[%d]: expected %f, got %f", i, orig[i], decoded[i])
		}
	}
}

func TestSearchCompletedTracksEmpty(t *testing.T) {
	db := testDB(t)
	tracks, total, err := SearchCompletedTracks(db.Conn, "", 50, 0)
	if err != nil {
		t.Fatalf("SearchCompletedTracks: %v", err)
	}
	if total != 0 || len(tracks) != 0 {
		t.Errorf("expected 0, got total=%d len=%d", total, len(tracks))
	}
}

func TestSearchCompletedTracks(t *testing.T) {
	db := testDB(t)

	clap := makeTestClap(0)
	mfcc := makeTestMfcc(0)
	chroma := makeTestChroma(0)
	setupTrackWithEmbedding(t, db, "etree:gd-1977", "dark-star.mp3", "Dark Star", 1, clap, mfcc, chroma, 0.85)
	setupTrackWithEmbedding(t, db, "georgeblood:victor", "herbert.mp3", "Herbert", 1, clap, mfcc, chroma, 0.72)
	setupTrackWithEmbedding(t, db, "etree:ph-1999", "tweezer.mp3", "Tweezer", 1, clap, mfcc, chroma, 0.91)

	tracks, total, err := SearchCompletedTracks(db.Conn, "", 50, 0)
	if err != nil {
		t.Fatalf("SearchCompletedTracks: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 total, got %d", total)
	}
	if len(tracks) != 3 {
		t.Errorf("expected 3 tracks, got %d", len(tracks))
	}

	tracks, total, err = SearchCompletedTracks(db.Conn, "etree", 50, 0)
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 matching 'etree', got %d", total)
	}

	tracks, total, err = SearchCompletedTracks(db.Conn, "Dark", 50, 0)
	if err != nil {
		t.Fatalf("filtered by title: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 matching 'Dark', got %d", total)
	}

	tracks, total, err = SearchCompletedTracks(db.Conn, "", 2, 0)
	if err != nil {
		t.Fatalf("paginated: %v", err)
	}
	if total != 3 || len(tracks) != 2 {
		t.Errorf("expected total=3 len=2, got total=%d len=%d", total, len(tracks))
	}

	tracks, _, err = SearchCompletedTracks(db.Conn, "nonexistent", 50, 0)
	if err != nil {
		t.Fatalf("no match: %v", err)
	}
	if len(tracks) != 0 {
		t.Errorf("expected 0 for nonexistent, got %d", len(tracks))
	}
}

func TestSearchAlbums(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("etree:gd-1977", "georgeblood:victor"))
	MarkAlbumResolved(db.Conn, "etree:gd-1977", "Grateful Dead 1977", "Grateful Dead", "etree", "", 12)
	MarkAlbumResolved(db.Conn, "georgeblood:victor", "Victor Herbert", "Victor Herbert", "georgeblood", "", 4)

	albums, total, err := SearchAlbums(db.Conn, "", 50, 0, false)
	if err != nil {
		t.Fatalf("SearchAlbums: %v", err)
	}
	if total != 2 || len(albums) != 2 {
		t.Errorf("expected 2 albums, got total=%d len=%d", total, len(albums))
	}

	albums, total, err = SearchAlbums(db.Conn, "Grateful", 50, 0, false)
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1, got %d", total)
	}
}

func TestGetAlbumTracks(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	MarkAlbumResolved(db.Conn, "album-a", "Album A", "", "", "", 0)
	InsertTracks(db.Conn, "album-a", []TrackInsert{
		{Filename: "01.mp3", Title: "First", TrackNumber: 1, Format: "VBR MP3", DownloadURL: "https://example.com/01.mp3"},
		{Filename: "02.mp3", Title: "Second", TrackNumber: 2, Format: "VBR MP3", DownloadURL: "https://example.com/02.mp3"},
	})

	tracks, err := GetAlbumTracks(db.Conn, "album-a")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	if tracks[0].Title != "First" || tracks[1].Title != "Second" {
		t.Errorf("unexpected: %+v, %+v", tracks[0], tracks[1])
	}
	if tracks[0].TrackNumber != 1 || tracks[1].TrackNumber != 2 {
		t.Errorf("track numbers: %d, %d", tracks[0].TrackNumber, tracks[1].TrackNumber)
	}
}

func TestSeedCollectionsIfEmpty(t *testing.T) {
	db := testDB(t)

	n, err := SeedCollectionsIfEmpty(db.Conn)
	if err != nil {
		t.Fatalf("SeedCollectionsIfEmpty: %v", err)
	}
	if n == 0 {
		t.Fatal("expected collections to be seeded")
	}

	count, err := GetCollectionCount(db.Conn)
	if err != nil {
		t.Fatalf("GetCollectionCount: %v", err)
	}
	if count != int(n) {
		t.Errorf("expected %d collections, got %d", n, count)
	}

	n2, err := SeedCollectionsIfEmpty(db.Conn)
	if err != nil {
		t.Fatalf("second SeedCollectionsIfEmpty: %v", err)
	}
	if n2 != 0 {
		t.Errorf("expected 0 on second seed, got %d", n2)
	}
}

func TestCollectionStats(t *testing.T) {
	db := testDB(t)

	InsertCollection(db.Conn, CollectionInsert{
		CollectionID: "test-coll", Title: "Test", Query: "collection:test", ExpectedCount: 100,
	})
	InsertCollection(db.Conn, CollectionInsert{
		CollectionID: "test-coll2", Title: "Test 2", Query: "collection:test2", ExpectedCount: 50,
	})

	stats, err := GetCollectionStats(db.Conn)
	if err != nil {
		t.Fatalf("GetCollectionStats: %v", err)
	}
	if stats.Total != 2 || stats.Pending != 2 {
		t.Errorf("expected 2 total/pending, got %+v", stats)
	}

	MarkCollectionDiscovering(db.Conn, "test-coll")
	stats, _ = GetCollectionStats(db.Conn)
	if stats.Discovering != 1 || stats.Pending != 1 {
		t.Errorf("expected 1 discovering, 1 pending, got %+v", stats)
	}

	MarkCollectionDiscovered(db.Conn, "test-coll", 42)
	stats, _ = GetCollectionStats(db.Conn)
	if stats.Discovered != 1 {
		t.Errorf("expected 1 discovered, got %+v", stats)
	}
}

func TestBulkInsertCollectionAlbums(t *testing.T) {
	db := testDB(t)

	InsertCollection(db.Conn, CollectionInsert{
		CollectionID: "coll-a", Title: "Coll A", Query: "collection:a", ExpectedCount: 10,
	})
	InsertCollection(db.Conn, CollectionInsert{
		CollectionID: "coll-b", Title: "Coll B", Query: "collection:b", ExpectedCount: 5,
	})

	albums := []AlbumInsert{
		{Identifier: "album-1", Downloads: 100},
		{Identifier: "album-2", Downloads: 50},
	}

	n, err := BulkInsertCollectionAlbums(db.Conn, "coll-a", albums)
	if err != nil {
		t.Fatalf("BulkInsertCollectionAlbums: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 inserted, got %d", n)
	}

	count, err := GetCollectionAlbumCount(db.Conn, "coll-a")
	if err != nil {
		t.Fatalf("GetCollectionAlbumCount: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 collection albums, got %d", count)
	}

	n, err = BulkInsertCollectionAlbums(db.Conn, "coll-b", []AlbumInsert{{Identifier: "album-1", Downloads: 100}})
	if err != nil {
		t.Fatalf("BulkInsertCollectionAlbums coll-b: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 new albums (already exists), got %d", n)
	}

	count, err = GetCollectionAlbumCount(db.Conn, "coll-b")
	if err != nil {
		t.Fatalf("GetCollectionAlbumCount coll-b: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 collection album link, got %d", count)
	}
}

func TestRemoveCollection(t *testing.T) {
	db := testDB(t)

	InsertCollection(db.Conn, CollectionInsert{
		CollectionID: "to-remove", Title: "Remove Me", Query: "collection:remove", ExpectedCount: 1,
	})
	BulkInsertCollectionAlbums(db.Conn, "to-remove", []AlbumInsert{{Identifier: "album-x"}})

	if err := RemoveCollection(db.Conn, "to-remove"); err != nil {
		t.Fatalf("RemoveCollection: %v", err)
	}

	count, _ := GetCollectionCount(db.Conn)
	if count != 0 {
		t.Errorf("expected 0 collections after removal, got %d", count)
	}

	var linkCount int
	db.Conn.QueryRow(`SELECT count(*) FROM collection_albums WHERE collection_id='to-remove'`).Scan(&linkCount)
	if linkCount != 0 {
		t.Errorf("expected 0 links after removal, got %d", linkCount)
	}
}

func TestEncodeDecodeF16Roundtrip(t *testing.T) {
	orig := []float32{0.0, 0.5, -0.5, 1.0, -1.0, 0.123, -0.987}
	blob := encodeF16(orig)
	decoded := decodeF16(blob)
	if len(decoded) != len(orig) {
		t.Fatalf("expected %d elements, got %d", len(orig), len(decoded))
	}
	for i := range orig {
		diff := math.Abs(float64(decoded[i] - orig[i]))
		if diff > 1e-3 {
			t.Errorf("dim[%d]: expected %f, got %f (diff=%e)", i, orig[i], decoded[i], diff)
		}
	}
}

func TestEncodeF16BlobSize(t *testing.T) {
	v := make([]float32, 512)
	blob := encodeF16(v)
	if len(blob) != 1024 {
		t.Errorf("expected 1024 bytes for 512 dims, got %d", len(blob))
	}
}

func TestEncodeDecodeF16Zero(t *testing.T) {
	v := make([]float32, 10)
	decoded := decodeF16(encodeF16(v))
	for i, f := range decoded {
		if f != 0 {
			t.Errorf("dim[%d]: expected 0, got %f", i, f)
		}
	}
}

func TestEncodeDecodeF16Negative(t *testing.T) {
	v := []float32{-0.1, -0.5, -1.0}
	decoded := decodeF16(encodeF16(v))
	for i := range v {
		if decoded[i] >= 0 {
			t.Errorf("dim[%d]: expected negative, got %f", i, decoded[i])
		}
	}
}

func TestEncodeDecodeF16NormalizedCLAPValues(t *testing.T) {
	v := make([]float32, 512)
	for i := range v {
		v[i] = float32(i-256) / 512.0
	}
	v = l2Normalize(v)
	decoded := decodeF16(encodeF16(v))
	for i := range v {
		diff := math.Abs(float64(decoded[i] - v[i]))
		if diff > 1e-3 {
			t.Errorf("dim[%d]: expected %f, got %f (diff=%e)", i, v[i], decoded[i], diff)
		}
	}
}

func TestL2Normalize(t *testing.T) {
	v := []float32{3, 4}
	normed := l2Normalize(v)
	var sum float64
	for _, f := range normed {
		sum += float64(f) * float64(f)
	}
	if math.Abs(sum-1.0) > 1e-6 {
		t.Errorf("expected unit norm, got %f", math.Sqrt(sum))
	}
	if math.Abs(float64(normed[0])-0.6) > 1e-6 || math.Abs(float64(normed[1])-0.8) > 1e-6 {
		t.Errorf("expected [0.6, 0.8], got %v", normed)
	}
}

func TestL2NormalizeZero(t *testing.T) {
	v := make([]float32, 5)
	normed := l2Normalize(v)
	for i, f := range normed {
		if f != 0 {
			t.Errorf("dim[%d]: expected 0, got %f", i, f)
		}
	}
}

func TestL2NormalizeIdempotent(t *testing.T) {
	v := []float32{0.6, 0.8}
	once := l2Normalize(v)
	twice := l2Normalize(once)
	for i := range once {
		diff := math.Abs(float64(twice[i] - once[i]))
		if diff > 1e-6 {
			t.Errorf("dim[%d]: not idempotent: %f vs %f", i, once[i], twice[i])
		}
	}
}

func TestDotProduct(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	if d := dotProduct(a, b); math.Abs(d) > 1e-6 {
		t.Errorf("expected 0 for orthogonal, got %f", d)
	}
	if d := dotProduct(a, a); math.Abs(d-1.0) > 1e-6 {
		t.Errorf("expected 1 for self, got %f", d)
	}
}

func TestDotProductNormalized(t *testing.T) {
	a := l2Normalize([]float32{3, 4})
	b := l2Normalize([]float32{3, 4})
	d := dotProduct(a, b)
	if math.Abs(d-1.0) > 1e-5 {
		t.Errorf("expected ~1 for identical unit vectors, got %f", d)
	}
}

func TestSearchByText(t *testing.T) {
	db := testDB(t)

	clap1 := make([]float32, 512)
	clap1[0] = 1.0
	clap2 := make([]float32, 512)
	clap2[1] = 1.0
	clap3 := make([]float32, 512)
	clap3[0] = 0.9
	clap3[1] = 0.1
	mfcc := makeTestMfcc(0)
	chroma := makeTestChroma(0)

	setupTrackWithEmbedding(t, db, "album-1", "t1.mp3", "Target Track", 1, clap1, mfcc, chroma, 0.9)
	setupTrackWithEmbedding(t, db, "album-2", "t2.mp3", "Other Track", 1, clap2, mfcc, chroma, 0.8)
	setupTrackWithEmbedding(t, db, "album-3", "t3.mp3", "Similar Track", 1, clap3, mfcc, chroma, 0.7)

	queryVec := make([]float32, 512)
	queryVec[0] = 1.0

	results, err := SearchByText(db.Conn, queryVec, "target", 10)
	if err != nil {
		t.Fatalf("SearchByText: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Title != "Target Track" {
		t.Errorf("expected Target Track first, got %s (sim=%f)", results[0].Title, results[0].Similarity)
	}
	if results[0].Similarity < results[1].Similarity {
		t.Errorf("expected results sorted by similarity descending")
	}
}

func TestSearchByTextEmpty(t *testing.T) {
	db := testDB(t)
	queryVec := make([]float32, 512)
	queryVec[0] = 1.0
	results, err := SearchByText(db.Conn, queryVec, "", 10)
	if err != nil {
		t.Fatalf("SearchByText empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchCompletedTracksWithTags(t *testing.T) {
	db := testDB(t)

	BulkInsertAlbums(db.Conn, testAlbumInserts("piano-album"))
	MarkAlbumResolved(db.Conn, "piano-album", "Piano Works", "Beethoven", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'piano-album'`)
	InsertTracks(db.Conn, "piano-album", []TrackInsert{
		{Filename: "track1.mp3", Title: "Allegro", Format: "VBR MP3", DownloadURL: "https://example.com/allegro"},
	})
	claimed, _ := ClaimNextTrackBatch(db.Conn, "test", 1)
	if len(claimed) == 0 {
		t.Fatal("no tracks claimed")
	}
	MarkTrackCompleted(db.Conn, claimed[0].ID)
	clap := make([]float32, 512)
	mfcc := make([]float32, 40)
	chroma := make([]float32, 12)
	SaveEmbedding(db.Conn, claimed[0].ID, clap, mfcc, chroma, 0.8)

	db.Conn.Exec(`UPDATE tracks SET tags = 'piano, classical, beethoven' WHERE id = ?`, claimed[0].ID)

	tracks, total, err := SearchCompletedTracks(db.Conn, "piano", 50, 0)
	if err != nil {
		t.Fatalf("SearchCompletedTracks with tags: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 result for 'piano' tag search, got %d", total)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}

	tracks, total, err = SearchCompletedTracks(db.Conn, "classical", 50, 0)
	if err != nil {
		t.Fatalf("SearchCompletedTracks with tags: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 result for 'classical' tag search, got %d", total)
	}

	tracks, total, err = SearchCompletedTracks(db.Conn, "beethoven", 50, 0)
	if err != nil {
		t.Fatalf("SearchCompletedTracks with tags: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 result for 'beethoven' tag search, got %d", total)
	}

	tracks, _, err = SearchCompletedTracks(db.Conn, "nonexistent", 50, 0)
	if err != nil {
		t.Fatalf("SearchCompletedTracks with tags: %v", err)
	}
	if len(tracks) != 0 {
		t.Errorf("expected 0 results for nonexistent tag, got %d", len(tracks))
	}
}

func TestSearchByTextWithPillScore(t *testing.T) {
	db := testDB(t)

	clap1 := make([]float32, 512)
	clap1[0] = 1.0
	clap2 := make([]float32, 512)
	clap2[1] = 1.0
	mfcc := makeTestMfcc(0)
	chroma := makeTestChroma(0)

	setupTrackWithEmbedding(t, db, "album-piano", "t1.mp3", "Track One", 1, clap1, mfcc, chroma, 0.9)
	setupTrackWithEmbedding(t, db, "album-guitar", "t2.mp3", "Track Two", 1, clap2, mfcc, chroma, 0.8)

	db.Conn.Exec(`UPDATE tracks SET tags = 'piano, classical, beethoven' WHERE album_id = 'album-piano'`)
	db.Conn.Exec(`UPDATE tracks SET tags = 'guitar, rock, jazz' WHERE album_id = 'album-guitar'`)

	queryVec := make([]float32, 512)
	queryVec[0] = 1.0

	results, err := SearchByText(db.Conn, queryVec, "piano", 10)
	if err != nil {
		t.Fatalf("SearchByText with pill: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].PillScore <= 0 && results[1].PillScore > 0 {
		t.Errorf("expected first result to have positive pill score for 'piano' query")
	}
}

func TestUpdateAlbumMetadataAndTags(t *testing.T) {
	db := testDB(t)

	BulkInsertAlbums(db.Conn, testAlbumInserts("test-album"))
	MarkAlbumResolved(db.Conn, "test-album", "Test Album", "Test Artist", "", "", 0)

	err := UpdateAlbumMetadata(db.Conn, "test-album", "subject1, subject2", "genre1, genre2")
	if err != nil {
		t.Fatalf("UpdateAlbumMetadata: %v", err)
	}

	var subjects, genres string
	db.Conn.QueryRow(`SELECT subjects, genres FROM albums WHERE ia_identifier = 'test-album'`).Scan(&subjects, &genres)
	if subjects != "subject1, subject2" {
		t.Errorf("expected 'subject1, subject2', got %q", subjects)
	}
	if genres != "genre1, genre2" {
		t.Errorf("expected 'genre1, genre2', got %q", genres)
	}

	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'test-album'`)
	InsertTracks(db.Conn, "test-album", []TrackInsert{
		{Filename: "song.mp3", Title: "Song", Format: "VBR MP3", DownloadURL: "https://example.com/song"},
	})
	claimed, _ := ClaimNextTrackBatch(db.Conn, "test", 1)
	if len(claimed) == 0 {
		t.Fatal("no tracks claimed")
	}
	MarkTrackCompleted(db.Conn, claimed[0].ID)
	clap := make([]float32, 512)
	mfcc := make([]float32, 40)
	chroma := make([]float32, 12)
	SaveEmbedding(db.Conn, claimed[0].ID, clap, mfcc, chroma, 0.8)

	err = UpdateTracksTags(db.Conn, "test-album", "piano, classical")
	if err != nil {
		t.Fatalf("UpdateTracksTags: %v", err)
	}

	var tags string
	db.Conn.QueryRow(`SELECT tags FROM tracks WHERE album_id = 'test-album'`).Scan(&tags)
	if tags != "piano, classical" {
		t.Errorf("expected 'piano, classical', got %q", tags)
	}
}

func TestColumnMigration(t *testing.T) {
	db := testDB(t)

	BulkInsertAlbums(db.Conn, testAlbumInserts("migrate-test"))
	MarkAlbumResolved(db.Conn, "migrate-test", "Migration Test", "", "", "", 0)

	var subjects, genres sql.NullString
	err := db.Conn.QueryRow(`SELECT subjects, genres FROM albums WHERE ia_identifier = 'migrate-test'`).Scan(&subjects, &genres)
	if err != nil {
		t.Fatalf("subjects/genres columns missing: %v", err)
	}
	if !subjects.Valid {
		t.Log("subjects is NULL (expected for fresh insert)")
	}

	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'migrate-test'`)
	InsertTracks(db.Conn, "migrate-test", []TrackInsert{
		{Filename: "track.mp3", Title: "Track", Format: "VBR MP3", DownloadURL: "https://example.com/track"},
	})
	claimed, _ := ClaimNextTrackBatch(db.Conn, "test", 1)
	if len(claimed) > 0 {
		MarkTrackCompleted(db.Conn, claimed[0].ID)
	}

	var tags sql.NullString
	err = db.Conn.QueryRow(`SELECT tags FROM tracks WHERE album_id = 'migrate-test'`).Scan(&tags)
	if err != nil {
		t.Fatalf("tags column missing: %v", err)
	}
}

func TestClaimUntaggedAlbum(t *testing.T) {
	db := testDB(t)

	BulkInsertAlbums(db.Conn, testAlbumInserts("untagged-1"))
	MarkAlbumResolved(db.Conn, "untagged-1", "Untagged Album", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET prechecked = 1 WHERE ia_identifier = 'untagged-1'`)
	InsertTracks(db.Conn, "untagged-1", []TrackInsert{
		{Filename: "u1.mp3", Title: "U1", Format: "VBR MP3", DownloadURL: "https://example.com/u1"},
	})
	claimed, _ := ClaimNextTrackBatch(db.Conn, "test", 1)
	if len(claimed) > 0 {
		MarkTrackCompleted(db.Conn, claimed[0].ID)
		clap := make([]float32, 512)
		mfcc := make([]float32, 40)
		chroma := make([]float32, 12)
		SaveEmbedding(db.Conn, claimed[0].ID, clap, mfcc, chroma, 0.5)
	}

	album, err := ClaimUntaggedAlbum(db.Conn)
	if err != nil {
		t.Fatalf("ClaimUntaggedAlbum: %v", err)
	}
	if album == nil {
		t.Fatal("expected to claim an untagged album")
	}
	if album.IAIdentifier != "untagged-1" {
		t.Errorf("expected 'untagged-1', got %q", album.IAIdentifier)
	}
	if album.TrackCount != 1 {
		t.Errorf("expected 1 untagged track, got %d", album.TrackCount)
	}

	db.Conn.Exec(`UPDATE tracks SET tags = 'test' WHERE album_id = 'untagged-1'`)
	album, err = ClaimUntaggedAlbum(db.Conn)
	if err != nil {
		t.Fatalf("ClaimUntaggedAlbum after tagging: %v", err)
	}
	if album != nil {
		t.Error("expected no untagged albums after tagging")
	}
}

func TestGetAllCollectionsOrdering(t *testing.T) {
	db := testDB(t)
	db.Conn.Exec(`INSERT INTO collections(collection_id, title, query, expected_count, created_at) VALUES('c1', 'Collection 1', 'q1', 10, datetime('now', '-60 seconds'))`)
	db.Conn.Exec(`INSERT INTO collections(collection_id, title, query, expected_count, created_at) VALUES('c2', 'Collection 2', 'q2', 20, datetime('now', '-30 seconds'))`)
	db.Conn.Exec(`INSERT INTO collections(collection_id, title, query, expected_count, created_at) VALUES('c3', 'Collection 3', 'q3', 30, datetime('now', '-0 seconds'))`)

	cols, err := GetAllCollections(db.Conn)
	if err != nil {
		t.Fatalf("GetAllCollections: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 collections, got %d", len(cols))
	}
	if cols[0].CollectionID != "c3" {
		t.Errorf("newest first: expected c3, got %s", cols[0].CollectionID)
	}
	if cols[1].CollectionID != "c2" {
		t.Errorf("second: expected c2, got %s", cols[1].CollectionID)
	}
	if cols[2].CollectionID != "c1" {
		t.Errorf("oldest last: expected c1, got %s", cols[2].CollectionID)
	}
}

func TestClaimUnresolvedAlbumOrdering(t *testing.T) {
	db := testDB(t)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, status, created_at) VALUES('old-album', 'pending', datetime('now', '-120 seconds'))`)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, status, created_at) VALUES('mid-album', 'pending', datetime('now', '-60 seconds'))`)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, status, created_at) VALUES('new-album', 'pending', datetime('now', '-0 seconds'))`)

	id, err := ClaimUnresolvedAlbum(db.Conn, "w1")
	if err != nil {
		t.Fatalf("ClaimUnresolvedAlbum: %v", err)
	}
	if id != "new-album" {
		t.Errorf("expected newest album first, got %s (want new-album)", id)
	}

	id, err = ClaimUnresolvedAlbum(db.Conn, "w2")
	if err != nil {
		t.Fatal(err)
	}
	if id != "mid-album" {
		t.Errorf("expected second newest, got %s (want mid-album)", id)
	}

	id, err = ClaimUnresolvedAlbum(db.Conn, "w3")
	if err != nil {
		t.Fatal(err)
	}
	if id != "old-album" {
		t.Errorf("expected oldest last, got %s (want old-album)", id)
	}
}

func TestClaimUnresolvedAlbumBatchOrdering(t *testing.T) {
	db := testDB(t)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, status, created_at) VALUES('a1', 'pending', datetime('now', '-90 seconds'))`)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, status, created_at) VALUES('a2', 'pending', datetime('now', '-60 seconds'))`)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, status, created_at) VALUES('a3', 'pending', datetime('now', '-30 seconds'))`)

	ids, err := ClaimUnresolvedAlbumBatch(db.Conn, "w1", 3)
	if err != nil {
		t.Fatalf("ClaimUnresolvedAlbumBatch: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 claimed, got %d", len(ids))
	}
	if ids[0] != "a3" {
		t.Errorf("newest first: expected a3, got %s", ids[0])
	}
	if ids[1] != "a2" {
		t.Errorf("second: expected a2, got %s", ids[1])
	}
	if ids[2] != "a1" {
		t.Errorf("oldest last: expected a1, got %s", ids[2])
	}
}

func TestClaimNextTrackBatchOrdering(t *testing.T) {
	db := testDB(t)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, status, prechecked, created_at) VALUES('album-x', 'resolved', 1, datetime('now', '-60 seconds'))`)
	db.Conn.Exec(`INSERT INTO tracks(album_id, filename, title, download_url, status, created_at) VALUES('album-x', 'old.mp3', 'Old', 'https://x/1', 'pending', datetime('now', '-90 seconds'))`)
	db.Conn.Exec(`INSERT INTO tracks(album_id, filename, title, download_url, status, created_at) VALUES('album-x', 'mid.mp3', 'Mid', 'https://x/2', 'pending', datetime('now', '-60 seconds'))`)
	db.Conn.Exec(`INSERT INTO tracks(album_id, filename, title, download_url, status, created_at) VALUES('album-x', 'new.mp3', 'New', 'https://x/3', 'pending', datetime('now', '-30 seconds'))`)

	claimed, err := ClaimNextTrackBatch(db.Conn, "w1", 3)
	if err != nil {
		t.Fatalf("ClaimNextTrackBatch: %v", err)
	}
	if len(claimed) != 3 {
		t.Fatalf("expected 3 claimed, got %d", len(claimed))
	}
	if claimed[0].Title != "New" {
		t.Errorf("newest first: expected New, got %s", claimed[0].Title)
	}
	if claimed[1].Title != "Mid" {
		t.Errorf("second: expected Mid, got %s", claimed[1].Title)
	}
	if claimed[2].Title != "Old" {
		t.Errorf("oldest last: expected Old, got %s", claimed[2].Title)
	}
}

func TestClaimUnprecheckedAlbumOrdering(t *testing.T) {
	db := testDB(t)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, title, status, prechecked, track_count, created_at) VALUES('old-resolved', 'Old', 'resolved', 0, 5, datetime('now', '-120 seconds'))`)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, title, status, prechecked, track_count, created_at) VALUES('mid-resolved', 'Mid', 'resolved', 0, 5, datetime('now', '-60 seconds'))`)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, title, status, prechecked, track_count, created_at) VALUES('new-resolved', 'New', 'resolved', 0, 5, datetime('now', '-0 seconds'))`)

	r, err := ClaimUnprecheckedAlbum(db.Conn)
	if err != nil {
		t.Fatalf("ClaimUnprecheckedAlbum: %v", err)
	}
	if r == nil {
		t.Fatal("expected to claim an album")
	}
	if r.IAIdentifier != "new-resolved" {
		t.Errorf("newest first: expected new-resolved, got %s", r.IAIdentifier)
	}
	MarkAlbumPrechecked(db.Conn, r.IAIdentifier)

	r, err = ClaimUnprecheckedAlbum(db.Conn)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected second album")
	}
	if r.IAIdentifier != "mid-resolved" {
		t.Errorf("second: expected mid-resolved, got %s", r.IAIdentifier)
	}
	MarkAlbumPrechecked(db.Conn, r.IAIdentifier)

	r, err = ClaimUnprecheckedAlbum(db.Conn)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected third album")
	}
	if r.IAIdentifier != "old-resolved" {
		t.Errorf("oldest last: expected old-resolved, got %s", r.IAIdentifier)
	}
	MarkAlbumPrechecked(db.Conn, r.IAIdentifier)
}

func TestClaimUntaggedAlbumOrdering(t *testing.T) {
	db := testDB(t)

	db.Conn.Exec(`INSERT INTO albums(ia_identifier, title, creator, status, prechecked, track_count, created_at) VALUES('a-old', 'Old', 'Creator', 'resolved', 1, 1, datetime('now', '-120 seconds'))`)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, title, creator, status, prechecked, track_count, created_at) VALUES('a-mid', 'Mid', 'Creator', 'resolved', 1, 1, datetime('now', '-60 seconds'))`)
	db.Conn.Exec(`INSERT INTO albums(ia_identifier, title, creator, status, prechecked, track_count, created_at) VALUES('a-new', 'New', 'Creator', 'resolved', 1, 1, datetime('now', '-0 seconds'))`)

	for _, aid := range []string{"a-old", "a-mid", "a-new"} {
		db.Conn.Exec(`INSERT INTO tracks(album_id, filename, title, download_url, status, created_at) VALUES(?, ?, ?, ?, 'completed', datetime('now'))`, aid, aid+"-t.mp3", aid, "https://x/"+aid)
		var trackID int
		db.Conn.QueryRow(`SELECT id FROM tracks WHERE album_id=?`, aid).Scan(&trackID)
		clap := make([]byte, 1024)
		mfcc := make([]byte, 80)
		chroma := make([]byte, 24)
		db.Conn.Exec(`INSERT INTO track_embeddings(track_id, clap, mfcc, chroma) VALUES(?, ?, ?, ?)`, trackID, clap, mfcc, chroma)
	}

	album, err := ClaimUntaggedAlbum(db.Conn)
	if err != nil {
		t.Fatalf("ClaimUntaggedAlbum: %v", err)
	}
	if album == nil {
		t.Fatal("expected to claim an album")
	}
	if album.IAIdentifier != "a-new" {
		t.Errorf("newest first: expected a-new, got %s", album.IAIdentifier)
	}
	UpdateTracksTags(db.Conn, album.IAIdentifier, "tagged")

	album, err = ClaimUntaggedAlbum(db.Conn)
	if err != nil {
		t.Fatal(err)
	}
	if album == nil {
		t.Fatal("expected second album")
	}
	if album.IAIdentifier != "a-mid" {
		t.Errorf("second: expected a-mid, got %s", album.IAIdentifier)
	}
	UpdateTracksTags(db.Conn, album.IAIdentifier, "tagged")

	album, err = ClaimUntaggedAlbum(db.Conn)
	if err != nil {
		t.Fatal(err)
	}
	if album == nil {
		t.Fatal("expected third album")
	}
	if album.IAIdentifier != "a-old" {
		t.Errorf("oldest last: expected a-old, got %s", album.IAIdentifier)
	}
	UpdateTracksTags(db.Conn, album.IAIdentifier, "tagged")
}
