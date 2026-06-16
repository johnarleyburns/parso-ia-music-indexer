package db

import (
	"math"
	"os"
	"testing"
	"time"
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

func TestOpenMigrate(t *testing.T) {
	db := testDB(t)

	var count int
	if err := db.Conn.QueryRow(`SELECT count(*) FROM catalog_queue`).Scan(&count); err != nil {
		t.Fatalf("query catalog_queue: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}

	var id int
	if err := db.Conn.QueryRow(`SELECT id FROM cursor_state WHERE id=1`).Scan(&id); err != nil {
		t.Fatalf("query cursor_state: %v", err)
	}

	if err := db.migrate(); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
}

func TestGetStatsEmpty(t *testing.T) {
	db := testDB(t)
	stats, err := GetStats(db.Conn)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.Total != 0 || stats.Pending != 0 {
		t.Errorf("expected all zeros, got %+v", stats)
	}
}

func TestBulkInsertPending(t *testing.T) {
	db := testDB(t)
	ids := []string{"id-a", "id-b", "id-c"}
	n, err := BulkInsertPending(db.Conn, ids)
	if err != nil {
		t.Fatalf("BulkInsertPending: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 inserted, got %d", n)
	}

	stats, err := GetStats(db.Conn)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.Total != 3 || stats.Pending != 3 {
		t.Errorf("expected 3 total/pending, got %+v", stats)
	}

	n, err = BulkInsertPending(db.Conn, []string{"id-a", "id-d"})
	if err != nil {
		t.Fatalf("BulkInsertPending: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 new, got %d", n)
	}
}

func TestClaimAndComplete(t *testing.T) {
	db := testDB(t)
	BulkInsertPending(db.Conn, []string{"a", "b", "c", "d", "e"})

	batch, err := ClaimNextBatch(db.Conn, "w1", 3)
	if err != nil {
		t.Fatalf("ClaimNextBatch: %v", err)
	}
	if len(batch) != 3 {
		t.Fatalf("expected 3 claimed, got %d", len(batch))
	}

	stats, _ := GetStats(db.Conn)
	if stats.Processing != 3 || stats.Pending != 2 {
		t.Errorf("expected 3 processing, 2 pending, got %+v", stats)
	}

	batch2, err := ClaimNextBatch(db.Conn, "w2", 5)
	if err != nil {
		t.Fatalf("ClaimNextBatch 2: %v", err)
	}
	if len(batch2) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(batch2))
	}

	batch3, err := ClaimNextBatch(db.Conn, "w3", 10)
	if err != nil {
		t.Fatalf("ClaimNextBatch 3: %v", err)
	}
	if len(batch3) != 0 {
		t.Fatalf("expected 0, got %d", len(batch3))
	}

	if err := MarkCompleted(db.Conn, "a"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	stats, _ = GetStats(db.Conn)
	if stats.Completed != 1 || stats.Processing != 4 {
		t.Errorf("expected 1 completed, 4 processing, got %+v", stats)
	}
}

func TestMarkFailed(t *testing.T) {
	db := testDB(t)
	BulkInsertPending(db.Conn, []string{"a"})
	ClaimNextBatch(db.Conn, "w1", 1)

	if err := MarkFailed(db.Conn, "a", "test error"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	stats, _ := GetStats(db.Conn)
	if stats.Failed != 1 || stats.Processing != 0 {
		t.Errorf("expected 1 failed, 0 processing, got %+v", stats)
	}
}

func TestResetStuckJobs(t *testing.T) {
	db := testDB(t)
	BulkInsertPending(db.Conn, []string{"x", "y"})
	ClaimNextBatch(db.Conn, "w1", 2)

	_, err := db.Conn.Exec(`UPDATE catalog_queue SET locked_at='2020-01-01T00:00:00Z' WHERE ia_identifier='x'`)
	if err != nil {
		t.Fatalf("backdate: %v", err)
	}

	n, err := ResetStuckJobs(db.Conn, 5*time.Minute)
	if err != nil {
		t.Fatalf("ResetStuckJobs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 reset, got %d", n)
	}

	stats, _ := GetStats(db.Conn)
	if stats.Pending != 1 || stats.Processing != 1 {
		t.Errorf("expected 1 pending, 1 processing after reset, got %+v", stats)
	}
}

func TestEmbeddingRoundtrip(t *testing.T) {
	db := testDB(t)
	vec := make([]float32, 40)
	for i := range vec {
		vec[i] = float32(i) * 0.1
	}

	if err := SaveEmbedding(db.Conn, "track-a", vec, 24.5); err != nil {
		t.Fatalf("SaveEmbedding: %v", err)
	}

	got, qs, err := GetEmbedding(db.Conn, "track-a")
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

func TestQuerySimilar(t *testing.T) {
	db := testDB(t)

	SaveEmbedding(db.Conn, "track-1", []float32{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, 30.0)
	SaveEmbedding(db.Conn, "track-2", []float32{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, 25.0)
	SaveEmbedding(db.Conn, "track-3", []float32{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}, 15.0)
	SaveEmbedding(db.Conn, "track-4", []float32{0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5}, 20.0)

	results, err := QuerySimilar(db.Conn, "track-1", 5)
	if err != nil {
		t.Fatalf("QuerySimilar: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].Identifier != "track-2" {
		t.Errorf("expected track-2 closest (identical vector), got %s (dist=%f)", results[0].Identifier, results[0].Distance)
	}
	if results[0].Distance > 0.001 {
		t.Errorf("expected distance ~0 for identical vector, got %f", results[0].Distance)
	}
	if results[2].Identifier != "track-3" {
		t.Errorf("expected track-3 farthest (opposite vector), got %s (dist=%f)", results[2].Identifier, results[2].Distance)
	}
	if results[2].Distance < 1.5 {
		t.Errorf("expected distance ~2 for opposite vector, got %f", results[2].Distance)
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
