package db

import (
	"database/sql"
	"testing"
)

func TestGetCollectionTrackStats(t *testing.T) {
	d := testDB(t)
	conn := d.Conn

	if err := InsertCollection(conn, CollectionInsert{CollectionID: "col1", Title: "Col1", Query: "collection:col1", SourceType: "collection"}); err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	if _, err := BulkInsertAlbums(conn, testAlbumInserts("alb1", "alb2")); err != nil {
		t.Fatalf("insert albums: %v", err)
	}
	if err := BulkLinkAlbumsToCollection(conn, "col1", []string{"alb1", "alb2"}); err != nil {
		t.Fatalf("link albums: %v", err)
	}

	tracks := []struct{ album, file, status string }{
		{"alb1", "a1.mp3", "completed"},
		{"alb1", "a2.mp3", "completed"},
		{"alb1", "a3.mp3", "pending"},
		{"alb2", "b1.mp3", "completed"},
		{"alb2", "b2.mp3", "failed"},
	}
	for _, tr := range tracks {
		if _, err := conn.Exec(`INSERT INTO tracks(album_id, filename, download_url, status) VALUES(?, ?, ?, ?)`,
			tr.album, tr.file, "https://x/"+tr.file, tr.status); err != nil {
			t.Fatalf("insert track: %v", err)
		}
	}

	stats, err := GetCollectionTrackStats(conn)
	if err != nil {
		t.Fatalf("GetCollectionTrackStats: %v", err)
	}
	got := stats["col1"]
	if got.Total != 5 {
		t.Errorf("Total = %d, want 5", got.Total)
	}
	if got.Analyzed != 3 {
		t.Errorf("Analyzed = %d, want 3", got.Analyzed)
	}

	if err := InsertCollection(conn, CollectionInsert{CollectionID: "empty", Title: "Empty", Query: "q", SourceType: "collection"}); err != nil {
		t.Fatalf("insert empty collection: %v", err)
	}
	stats, err = GetCollectionTrackStats(conn)
	if err != nil {
		t.Fatalf("GetCollectionTrackStats: %v", err)
	}
	if _, ok := stats["empty"]; ok {
		t.Errorf("expected collection with no tracks to be absent from stats map")
	}
}

func TestReplaceCollectionAlbums(t *testing.T) {
	d := testDB(t)
	conn := d.Conn

	if err := InsertCollection(conn, CollectionInsert{CollectionID: "col1", Title: "Col1", Query: "q", SourceType: "playlist"}); err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	if _, err := BulkInsertAlbums(conn, testAlbumInserts("a1", "a2", "a3", "a4")); err != nil {
		t.Fatalf("insert albums: %v", err)
	}
	if err := BulkLinkAlbumsToCollection(conn, "col1", []string{"a1", "a2", "a3"}); err != nil {
		t.Fatalf("link albums: %v", err)
	}

	if _, err := conn.Exec(`INSERT INTO tracks(album_id, filename, download_url, status) VALUES('a1','t.mp3','https://x/t','completed')`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	if err := ReplaceCollectionAlbums(conn, "col1", []string{"a2", "a3", "a4"}); err != nil {
		t.Fatalf("ReplaceCollectionAlbums: %v", err)
	}

	got := linkedAlbums(t, conn, "col1")
	want := map[string]bool{"a2": true, "a3": true, "a4": true}
	if len(got) != len(want) {
		t.Fatalf("linked = %v, want %v", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("missing link %s", id)
		}
	}
	if got["a1"] {
		t.Errorf("stale link a1 should have been removed")
	}

	var albumCount int
	if err := conn.QueryRow(`SELECT count(*) FROM albums`).Scan(&albumCount); err != nil {
		t.Fatalf("count albums: %v", err)
	}
	if albumCount != 4 {
		t.Errorf("albums = %d, want 4 (untouched)", albumCount)
	}

	var trackCount int
	if err := conn.QueryRow(`SELECT count(*) FROM tracks WHERE album_id='a1'`).Scan(&trackCount); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if trackCount != 1 {
		t.Errorf("a1 tracks = %d, want 1 (untouched)", trackCount)
	}
}

func linkedAlbums(t *testing.T, conn *sql.DB, collectionID string) map[string]bool {
	t.Helper()
	rows, err := conn.Query(`SELECT album_id FROM collection_albums WHERE collection_id=?`, collectionID)
	if err != nil {
		t.Fatalf("query links: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[id] = true
	}
	return out
}
