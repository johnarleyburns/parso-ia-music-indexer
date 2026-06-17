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

	var id int
	if err := db.Conn.QueryRow(`SELECT id FROM cursor_state WHERE id=1`).Scan(&id); err != nil {
		t.Fatalf("query cursor_state: %v", err)
	}

	if err := db.migrate(); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
}

func TestMigrateFromOldSchema(t *testing.T) {
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
	conn.Exec(`CREATE TABLE track_embeddings (
		ia_identifier TEXT PRIMARY KEY,
		embedding BLOB NOT NULL,
		quality_score REAL,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
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

	var albumCount int
	db.Conn.QueryRow(`SELECT count(*) FROM albums WHERE status='pending'`).Scan(&albumCount)
	if albumCount != 3 {
		t.Errorf("expected 3 migrated albums, got %d", albumCount)
	}

	if tableExists(db.Conn, "catalog_queue") {
		t.Error("catalog_queue should have been dropped")
	}

	cursor, err := GetCursor(db.Conn)
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if cursor.Cursor != "abc" || cursor.ItemsIndexed != 42 {
		t.Errorf("cursor not preserved: %+v", cursor)
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

func TestResetStuckTracks(t *testing.T) {
	db := testDB(t)
	BulkInsertAlbums(db.Conn, testAlbumInserts("album-a"))
	MarkAlbumResolved(db.Conn, "album-a", "", "", "", "", 0)
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

func setupTrackWithEmbedding(t *testing.T, db *DB, albumID, filename, title string, trackNum int, vec []float32, quality float64) int {
	t.Helper()
	BulkInsertAlbums(db.Conn, testAlbumInserts(albumID))
	MarkAlbumResolved(db.Conn, albumID, albumID, "", "", "", 0)
	InsertTracks(db.Conn, albumID, []TrackInsert{
		{Filename: filename, Title: title, TrackNumber: trackNum, Format: "VBR MP3", DownloadURL: "https://example.com/" + filename},
	})
	claimed, _ := ClaimNextTrackBatch(db.Conn, "test", 1)
	if len(claimed) == 0 {
		t.Fatal("no tracks claimed")
	}
	MarkTrackCompleted(db.Conn, claimed[0].ID)
	SaveEmbedding(db.Conn, claimed[0].ID, vec, quality)
	return claimed[0].ID
}

func TestEmbeddingRoundtrip(t *testing.T) {
	db := testDB(t)
	vec := make([]float32, 40)
	for i := range vec {
		vec[i] = float32(i) * 0.1
	}

	trackID := setupTrackWithEmbedding(t, db, "album-a", "song.mp3", "Song", 1, vec, 24.5)

	got, qs, err := GetEmbedding(db.Conn, trackID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(got) != 40 {
		t.Errorf("expected 40 dims, got %d", len(got))
	}
	if qs != 24.5 {
		t.Errorf("expected quality 24.5, got %f", qs)
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Errorf("dim[%d]: expected %f, got %f", i, vec[i], got[i])
		}
	}
}

func TestEmbeddingRoundtrip564Dim(t *testing.T) {
	db := testDB(t)
	vec := make([]float32, 564)
	for i := range vec {
		vec[i] = float32(i%100) / 100.0
	}

	trackID := setupTrackWithEmbedding(t, db, "album-564", "song564.mp3", "Song 564", 1, vec, 0.75)

	got, qs, err := GetEmbedding(db.Conn, trackID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 564 {
		t.Fatalf("expected 564 dims, got %d", len(got))
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Errorf("dim[%d]: expected %f, got %f", i, vec[i], got[i])
		}
	}
	if qs != 0.75 {
		t.Errorf("expected quality 0.75, got %f", qs)
	}
}

func TestQuerySimilar(t *testing.T) {
	db := testDB(t)
	vec1 := make([]float32, 40)
	vec2 := make([]float32, 40)
	vec3 := make([]float32, 40)
	vec4 := make([]float32, 40)
	for i := 0; i < 40; i++ {
		vec1[i] = 1
		vec2[i] = 1
		vec3[i] = -1
		vec4[i] = 0.5
	}

	id1 := setupTrackWithEmbedding(t, db, "album-1", "t1.mp3", "Track 1", 1, vec1, 30.0)
	setupTrackWithEmbedding(t, db, "album-2", "t2.mp3", "Track 2", 1, vec2, 25.0)
	setupTrackWithEmbedding(t, db, "album-3", "t3.mp3", "Track 3", 1, vec3, 15.0)
	setupTrackWithEmbedding(t, db, "album-4", "t4.mp3", "Track 4", 1, vec4, 20.0)

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
	if results[0].Distance > 0.001 {
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

func TestMixedDimensionSkipped(t *testing.T) {
	db := testDB(t)
	vec40 := make([]float32, 40)
	vec564 := make([]float32, 564)
	for i := range vec564 {
		vec564[i] = 1.0
	}

	setupTrackWithEmbedding(t, db, "album-old", "old.mp3", "Old", 1, vec40, 0.5)
	id564 := setupTrackWithEmbedding(t, db, "album-new", "new.mp3", "New", 1, vec564, 0.5)

	results, err := QuerySimilar(db.Conn, id564, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 0 {
		t.Errorf("expected no results (40-dim skipped), got %d", len(results))
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

	vec := make([]float32, 40)
	setupTrackWithEmbedding(t, db, "etree:gd-1977", "dark-star.mp3", "Dark Star", 1, vec, 0.85)
	setupTrackWithEmbedding(t, db, "georgeblood:victor", "herbert.mp3", "Herbert", 1, vec, 0.72)
	setupTrackWithEmbedding(t, db, "etree:ph-1999", "tweezer.mp3", "Tweezer", 1, vec, 0.91)

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

	albums, total, err := SearchAlbums(db.Conn, "", 50, 0)
	if err != nil {
		t.Fatalf("SearchAlbums: %v", err)
	}
	if total != 2 || len(albums) != 2 {
		t.Errorf("expected 2 albums, got total=%d len=%d", total, len(albums))
	}

	albums, total, err = SearchAlbums(db.Conn, "Grateful", 50, 0)
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
