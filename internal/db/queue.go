package db

import (
	"database/sql"
	"fmt"
	"time"
)

type QueueStats struct {
	Total      int
	Pending    int
	Processing int
	Completed  int
	Failed     int
}

func GetStats(db *sql.DB) (*QueueStats, error) {
	s := &QueueStats{}
	row := db.QueryRow(`SELECT count(*) FROM catalog_queue`)
	if err := row.Scan(&s.Total); err != nil {
		return nil, fmt.Errorf("total: %w", err)
	}
	rows, err := db.Query(`SELECT status, count(*) FROM catalog_queue GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("by status: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		switch status {
		case "pending":
			s.Pending = count
		case "processing":
			s.Processing = count
		case "completed":
			s.Completed = count
		case "failed":
			s.Failed = count
		}
	}
	return s, nil
}

func ClaimNextBatch(db *sql.DB, workerID string, batchSize int) ([]string, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := tx.Query(
		`SELECT ia_identifier FROM catalog_queue
		 WHERE status = 'pending'
		 ORDER BY created_at
		 LIMIT ?`,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("select pending: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return nil, nil
	}

	for _, id := range ids {
		if _, err := tx.Exec(
			`UPDATE catalog_queue SET status='processing', worker_id=?, locked_at=?, updated_at=? WHERE ia_identifier=?`,
			workerID, now, now, id,
		); err != nil {
			return nil, fmt.Errorf("update %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return ids, nil
}

func MarkCompleted(db *sql.DB, identifier string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE catalog_queue SET status='completed', updated_at=? WHERE ia_identifier=?`,
		now, identifier,
	)
	return err
}

func MarkFailed(db *sql.DB, identifier string, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE catalog_queue SET status='failed', error_message=?, retry_count=retry_count+1, updated_at=? WHERE ia_identifier=?`,
		errMsg, now, identifier,
	)
	return err
}

func ResetStuckJobs(db *sql.DB, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
	result, err := db.Exec(
		`UPDATE catalog_queue SET status='pending', worker_id=NULL, locked_at=NULL, updated_at=datetime('now')
		 WHERE status='processing' AND locked_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func BulkInsertPending(db *sql.DB, identifiers []string) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO catalog_queue(ia_identifier, status) VALUES(?, 'pending')`)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var inserted int64
	for _, id := range identifiers {
		res, err := stmt.Exec(id)
		if err != nil {
			return inserted, fmt.Errorf("insert %s: %w", id, err)
		}
		n, _ := res.RowsAffected()
		inserted += n
	}

	if err := tx.Commit(); err != nil {
		return inserted, fmt.Errorf("commit: %w", err)
	}

	return inserted, nil
}

func GetPendingCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT count(*) FROM catalog_queue WHERE status='pending'`).Scan(&count)
	return count, err
}

func GetCompletedCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT count(*) FROM catalog_queue WHERE status='completed'`).Scan(&count)
	return count, err
}
