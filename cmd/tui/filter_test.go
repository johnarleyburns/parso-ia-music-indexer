package main

import (
	"os"
	"testing"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"
)

func TestFilterDenylisted(t *testing.T) {
	denylist := map[string]bool{
		"genesis_librivox":    true,
		"relativity_librivox": true,
	}

	albums := []db.AlbumInsert{
		{Identifier: "genesis_librivox", Downloads: 1000},
		{Identifier: "good-music-album", Downloads: 500},
		{Identifier: "relativity_librivox", Downloads: 800},
		{Identifier: "etree:gd-1977", Downloads: 300},
		{Identifier: "another-music", Downloads: 100},
	}

	result := filterDenylisted(albums, denylist)
	if len(result) != 3 {
		t.Fatalf("expected 3 albums after filter, got %d", len(result))
	}
	if result[0].Identifier != "good-music-album" {
		t.Errorf("expected good-music-album first, got %s", result[0].Identifier)
	}
}

func TestFilterDenylistedSubstringMatch(t *testing.T) {
	denylist := map[string]bool{}

	albums := []db.AlbumInsert{
		{Identifier: "some_unknown_librivox_title"},
		{Identifier: "librivoxaudio_collection"},
		{Identifier: "good-music-album"},
	}

	result := filterDenylisted(albums, denylist)
	if len(result) != 1 {
		t.Fatalf("expected 1 album after substring filter, got %d", len(result))
	}
	if result[0].Identifier != "good-music-album" {
		t.Errorf("expected good-music-album, got %s", result[0].Identifier)
	}
}

func TestFilterDenylistedEmpty(t *testing.T) {
	albums := []db.AlbumInsert{
		{Identifier: "album-a"},
		{Identifier: "album-b"},
	}

	result := filterDenylisted(albums, nil)
	if len(result) != 2 {
		t.Errorf("expected 2 (no filter), got %d", len(result))
	}
}

func TestFilterDenylistedAllBlocked(t *testing.T) {
	denylist := map[string]bool{
		"a_librivox": true,
		"b_librivox": true,
	}

	albums := []db.AlbumInsert{
		{Identifier: "a_librivox"},
		{Identifier: "b_librivox"},
	}

	result := filterDenylisted(albums, denylist)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestLoadDenylist(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "denylist-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte(`["id-a", "id-b", "id-c"]`))
	f.Close()

	m := loadDenylist(f.Name())
	if len(m) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(m))
	}
	if !m["id-a"] || !m["id-b"] || !m["id-c"] {
		t.Errorf("missing entries: %v", m)
	}
}

func TestLoadDenylistMissing(t *testing.T) {
	m := loadDenylist("/nonexistent/path/denylist.json")
	if m != nil {
		t.Errorf("expected nil for missing file, got %v", m)
	}
}
