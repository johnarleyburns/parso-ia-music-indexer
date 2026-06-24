package playlist

import (
	"testing"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"
)

func TestSyncPlaylistRejectsNonPlaylist(t *testing.T) {
	_, err := SyncPlaylist(nil, nil, db.Collection{CollectionID: "c", SourceType: "collection"}, func(string, int, int) {})
	if err == nil {
		t.Fatal("expected error when syncing a non-playlist collection")
	}
}
