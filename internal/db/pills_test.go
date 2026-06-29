package db

import "testing"

func makeListenableAlbum(t *testing.T, db *DB, albumID, subjects, tags string) {
	t.Helper()
	clap := makeTestClap(0.5)
	mfcc := makeTestMfcc(0.5)
	chroma := makeTestChroma(0.5)
	trackID := setupTrackWithEmbedding(t, db, albumID, albumID+".mp3", albumID, 1, clap, mfcc, chroma, 0.5)
	if subjects != "" {
		if _, err := db.Conn.Exec(`UPDATE albums SET subjects=? WHERE ia_identifier=?`, subjects, albumID); err != nil {
			t.Fatalf("set subjects: %v", err)
		}
	}
	if tags != "" {
		if _, err := db.Conn.Exec(`UPDATE tracks SET tags=? WHERE id=?`, tags, trackID); err != nil {
			t.Fatalf("set tags: %v", err)
		}
	}
}

func TestSeedPillsContent(t *testing.T) {
	db := testDB(t)

	n, err := SeedPills(db.Conn)
	if err != nil {
		t.Fatalf("SeedPills: %v", err)
	}
	if n == 0 {
		t.Fatal("expected pills to be seeded, got 0")
	}

	p, err := GetPillByID(db.Conn, "ambient-drone")
	if err != nil {
		t.Fatalf("GetPillByID: %v", err)
	}
	if p.Label == "" || p.ClapPrompt == "" || p.Keywords == "" {
		t.Errorf("seeded pill missing fields: %+v", p)
	}
	if p.MinLibraryCount != 10 {
		t.Errorf("expected default min_library_count 10, got %d", p.MinLibraryCount)
	}
	if !p.Enabled {
		t.Errorf("expected pill enabled by default")
	}
}

func TestSeedPillsIdempotent(t *testing.T) {
	db := testDB(t)

	first, err := SeedPillsIfEmpty(db.Conn)
	if err != nil {
		t.Fatalf("seed first: %v", err)
	}
	if first == 0 {
		t.Fatal("expected first seed to insert pills")
	}

	second, err := SeedPillsIfEmpty(db.Conn)
	if err != nil {
		t.Fatalf("seed second: %v", err)
	}
	if second != 0 {
		t.Errorf("expected idempotent re-seed to insert 0, got %d", second)
	}

	got, _ := GetPillCount(db.Conn)
	if int64(got) != first {
		t.Errorf("pill count drifted: seeded %d, have %d", first, got)
	}
}

func TestGetPillByIDNotFound(t *testing.T) {
	db := testDB(t)
	SeedPills(db.Conn)
	if _, err := GetPillByID(db.Conn, "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown pill id")
	}
}

func TestCountPillCoverageBySubjects(t *testing.T) {
	db := testDB(t)
	makeListenableAlbum(t, db, "amb-1", "ambient; drone; experimental", "")
	makeListenableAlbum(t, db, "amb-2", "ambient soundscape", "")
	makeListenableAlbum(t, db, "tech-1", "techno; house", "")

	n, err := CountPillCoverage(db.Conn, "ambient, drone")
	if err != nil {
		t.Fatalf("CountPillCoverage: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 ambient albums, got %d", n)
	}

	n, _ = CountPillCoverage(db.Conn, "techno")
	if n != 1 {
		t.Errorf("expected 1 techno album, got %d", n)
	}

	n, _ = CountPillCoverage(db.Conn, "reggae")
	if n != 0 {
		t.Errorf("expected 0 reggae albums, got %d", n)
	}
}

func TestCountPillCoverageByTags(t *testing.T) {
	db := testDB(t)
	makeListenableAlbum(t, db, "tagged-1", "", "ambient, field recording")

	n, err := CountPillCoverage(db.Conn, "ambient")
	if err != nil {
		t.Fatalf("CountPillCoverage: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 album matched via track tags, got %d", n)
	}
}

func TestCountPillCoverageExcludesNonListenable(t *testing.T) {
	db := testDB(t)
	makeListenableAlbum(t, db, "excluded-1", "ambient", "")
	if _, err := db.Conn.Exec(
		`UPDATE tracks SET listenability_decision='exclude' WHERE album_id='excluded-1'`); err != nil {
		t.Fatalf("mark exclude: %v", err)
	}

	// Resolved album with subjects but no completed/embedded track must not count.
	BulkInsertAlbums(db.Conn, testAlbumInserts("noembed-1"))
	MarkAlbumResolved(db.Conn, "noembed-1", "noembed-1", "", "", "", 0)
	db.Conn.Exec(`UPDATE albums SET subjects='ambient' WHERE ia_identifier='noembed-1'`)

	n, err := CountPillCoverage(db.Conn, "ambient")
	if err != nil {
		t.Fatalf("CountPillCoverage: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 listenable ambient albums, got %d", n)
	}
}

func TestListActivePillsCoverageGate(t *testing.T) {
	db := testDB(t)
	makeListenableAlbum(t, db, "amb-a", "ambient", "")
	makeListenableAlbum(t, db, "amb-b", "ambient", "")

	if _, err := BulkInsertPills(db.Conn, []Pill{
		{PillID: "p-low", Label: "Low", ClapPrompt: "ambient", Keywords: "ambient", SortOrder: 2, Enabled: true, MinLibraryCount: 1},
		{PillID: "p-high", Label: "High", ClapPrompt: "ambient", Keywords: "ambient", SortOrder: 1, Enabled: true, MinLibraryCount: 5},
		{PillID: "p-off", Label: "Off", ClapPrompt: "ambient", Keywords: "ambient", SortOrder: 3, Enabled: false, MinLibraryCount: 1},
	}); err != nil {
		t.Fatalf("BulkInsertPills: %v", err)
	}

	active, err := ListActivePills(db.Conn)
	if err != nil {
		t.Fatalf("ListActivePills: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active pill, got %d: %+v", len(active), active)
	}
	if active[0].PillID != "p-low" {
		t.Errorf("expected p-low active, got %s", active[0].PillID)
	}
	if active[0].LibraryCount != 2 {
		t.Errorf("expected library count 2, got %d", active[0].LibraryCount)
	}
}

func TestListActivePillsSortOrder(t *testing.T) {
	db := testDB(t)
	makeListenableAlbum(t, db, "x-1", "ambient techno", "")

	BulkInsertPills(db.Conn, []Pill{
		{PillID: "second", Label: "Second", ClapPrompt: "techno", Keywords: "techno", SortOrder: 20, Enabled: true, MinLibraryCount: 1},
		{PillID: "first", Label: "First", ClapPrompt: "ambient", Keywords: "ambient", SortOrder: 10, Enabled: true, MinLibraryCount: 1},
	})

	active, err := ListActivePills(db.Conn)
	if err != nil {
		t.Fatalf("ListActivePills: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active pills, got %d", len(active))
	}
	if active[0].PillID != "first" || active[1].PillID != "second" {
		t.Errorf("pills not sorted by sort_order: %s, %s", active[0].PillID, active[1].PillID)
	}
}
