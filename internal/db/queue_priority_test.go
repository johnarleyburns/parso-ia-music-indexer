package db

import "testing"

// TestClaimUnresolvedAlbumPrioritizesMusopen verifies that pending albums in the
// priority collection (musopen-free) are resolved ahead of the general backlog,
// even when a non-priority album's collection was created more recently.
func TestClaimUnresolvedAlbumPrioritizesMusopen(t *testing.T) {
	db := testDB(t)

	BulkInsertAlbums(db.Conn, testAlbumInserts("musopen-album", "netlabel-album"))

	// Priority collection linked first (older), netlabels linked after (newer),
	// so without the priority bias the newer netlabels album would win.
	testLinkAlbumToCollection(db.Conn, priorityCollectionID, "musopen-album")
	testLinkAlbumToCollection(db.Conn, "netlabels-free", "netlabel-album")

	got, err := ClaimUnresolvedAlbum(db.Conn, "worker-1")
	if err != nil {
		t.Fatalf("ClaimUnresolvedAlbum: %v", err)
	}
	if got != "musopen-album" {
		t.Errorf("expected musopen-album to be claimed first, got %q", got)
	}
}

func TestClaimUnresolvedAlbumBatchPrioritizesMusopen(t *testing.T) {
	db := testDB(t)

	BulkInsertAlbums(db.Conn, testAlbumInserts("nl-1", "nl-2", "mo-1"))
	testLinkAlbumToCollection(db.Conn, "netlabels-free", "nl-1")
	testLinkAlbumToCollection(db.Conn, "netlabels-free", "nl-2")
	testLinkAlbumToCollection(db.Conn, priorityCollectionID, "mo-1")

	ids, err := ClaimUnresolvedAlbumBatch(db.Conn, "worker-1", 3)
	if err != nil {
		t.Fatalf("ClaimUnresolvedAlbumBatch: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("expected at least one album claimed")
	}
	if ids[0] != "mo-1" {
		t.Errorf("expected mo-1 first in batch, got %v", ids)
	}
}
