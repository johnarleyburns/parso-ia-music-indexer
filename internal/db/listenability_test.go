package db

import (
	"database/sql"
	"testing"
	"time"
)

func TestRecoverStaleListenabilityLocks(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC()
	stale := now.Add(-30 * time.Minute).Format(time.RFC3339)
	fresh := now.Add(-1 * time.Minute).Format(time.RFC3339)

	insert := func(id int, lockedAt interface{}) {
		t.Helper()
		_, err := d.Conn.Exec(
			`INSERT INTO tracks(id, album_id, filename, download_url, status,
				listenability_locked_at, listenability_worker_id)
			 VALUES(?, ?, ?, ?, 'completed', ?, ?)`,
			id, "alb", "f"+string(rune('0'+id))+".mp3", "https://x/"+string(rune('0'+id)),
			lockedAt, "cleaner-1",
		)
		if err != nil {
			t.Fatalf("insert track %d: %v", id, err)
		}
	}

	insert(1, stale)
	insert(2, fresh)
	insert(3, nil) // never locked

	n, err := RecoverStaleListenabilityLocks(d.Conn, 10*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleListenabilityLocks: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 recovered, got %d", n)
	}

	lockedAt := func(id int) sql.NullString {
		var v sql.NullString
		if err := d.Conn.QueryRow(`SELECT listenability_locked_at FROM tracks WHERE id=?`, id).Scan(&v); err != nil {
			t.Fatalf("query lock %d: %v", id, err)
		}
		return v
	}

	if lockedAt(1).Valid {
		t.Errorf("track 1 (stale) should be unlocked, still locked at %q", lockedAt(1).String)
	}
	if !lockedAt(2).Valid {
		t.Errorf("track 2 (fresh) should remain locked")
	}

	// Idempotent: a second sweep recovers nothing.
	n2, err := RecoverStaleListenabilityLocks(d.Conn, 10*time.Minute)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if n2 != 0 {
		t.Errorf("expected 0 on second sweep, got %d", n2)
	}
}
