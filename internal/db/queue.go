package db

import (
	"database/sql"
	"fmt"
	"time"
)

type AlbumStats struct {
	Total    int
	Pending  int
	Resolving int
	Resolved int
	Failed   int
}

type TrackStats struct {
	Total      int
	Pending    int
	Processing int
	Completed  int
	Failed     int
}

type CombinedStats struct {
	Albums AlbumStats
	Tracks TrackStats
}

type ClaimedTrack struct {
	ID          int
	AlbumID     string
	Filename    string
	Title       string
	DownloadURL string
}

type TrackInsert struct {
	Filename    string
	Title       string
	TrackNumber int
	Format      string
	Bitrate     int
	Duration    float64
	DownloadURL string
}

type AlbumInsert struct {
	Identifier string
	Downloads  int
}

type TrackResult struct {
	TrackID      int
	Title        string
	Filename     string
	AlbumID      string
	AlbumTitle   string
	DownloadURL  string
	QualityScore float64
}

type AlbumResult struct {
	IAIdentifier   string
	Title          string
	Creator        string
	Collection     string
	ArtURL         string
	TrackCount     int
	Status         string
	CompletedCount int
	Downloads      int
	AvgQuality     float64
}

type TrackDetail struct {
	ID           int
	Filename     string
	Title        string
	TrackNumber  int
	Format       string
	DownloadURL  string
	Status       string
	QualityScore float64
}

func GetCombinedStats(db *sql.DB) (*CombinedStats, error) {
	s := &CombinedStats{}

	rows, err := db.Query(`SELECT status, count(*) FROM albums GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("album stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		s.Albums.Total += count
		switch status {
		case "pending":
			s.Albums.Pending = count
		case "resolving":
			s.Albums.Resolving = count
		case "resolved":
			s.Albums.Resolved = count
		case "failed":
			s.Albums.Failed = count
		}
	}

	rows2, err := db.Query(`SELECT status, count(*) FROM tracks GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("track stats: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var status string
		var count int
		if err := rows2.Scan(&status, &count); err != nil {
			return nil, err
		}
		s.Tracks.Total += count
		switch status {
		case "pending":
			s.Tracks.Pending = count
		case "processing":
			s.Tracks.Processing = count
		case "completed":
			s.Tracks.Completed = count
		case "failed":
			s.Tracks.Failed = count
		}
	}

	return s, nil
}

func BulkInsertAlbums(db *sql.DB, albums []AlbumInsert) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO albums(ia_identifier, downloads, status) VALUES(?, ?, 'pending')`)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var inserted int64
	for _, a := range albums {
		res, err := stmt.Exec(a.Identifier, a.Downloads)
		if err != nil {
			return inserted, fmt.Errorf("insert %s: %w", a.Identifier, err)
		}
		n, _ := res.RowsAffected()
		inserted += n
	}

	if err := tx.Commit(); err != nil {
		return inserted, fmt.Errorf("commit: %w", err)
	}

	return inserted, nil
}

func BulkInsertCollectionAlbums(sqlDB *sql.DB, collectionID string, albums []AlbumInsert) (int64, error) {
	tx, err := sqlDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	albumStmt, err := tx.Prepare(`INSERT OR IGNORE INTO albums(ia_identifier, downloads, status) VALUES(?, ?, 'pending')`)
	if err != nil {
		return 0, fmt.Errorf("prepare album: %w", err)
	}
	defer albumStmt.Close()

	linkStmt, err := tx.Prepare(`INSERT OR IGNORE INTO collection_albums(collection_id, album_id) VALUES(?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare link: %w", err)
	}
	defer linkStmt.Close()

	var inserted int64
	for _, a := range albums {
		res, err := albumStmt.Exec(a.Identifier, a.Downloads)
		if err != nil {
			return inserted, fmt.Errorf("insert album %s: %w", a.Identifier, err)
		}
		n, _ := res.RowsAffected()
		inserted += n

		if _, err := linkStmt.Exec(collectionID, a.Identifier); err != nil {
			return inserted, fmt.Errorf("link %s to %s: %w", a.Identifier, collectionID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return inserted, fmt.Errorf("commit: %w", err)
	}

	return inserted, nil
}

func ClaimUnresolvedAlbum(db *sql.DB, workerID string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var identifier string
	err = tx.QueryRow(
		`SELECT ia_identifier FROM albums WHERE status = 'pending' ORDER BY created_at LIMIT 1`,
	).Scan(&identifier)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("select pending album: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE albums SET status='resolving', updated_at=? WHERE ia_identifier=?`,
		now, identifier,
	)
	if err != nil {
		return "", fmt.Errorf("update album status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	return identifier, nil
}

func ClaimUnresolvedAlbumBatch(db *sql.DB, workerID string, batchSize int) ([]string, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT ia_identifier FROM albums WHERE status = 'pending' ORDER BY created_at LIMIT ?`,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("select pending albums: %w", err)
	}
	defer rows.Close()

	var identifiers []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		identifiers = append(identifiers, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(identifiers) == 0 {
		return nil, nil
	}

	for _, id := range identifiers {
		if _, err := tx.Exec(
			`UPDATE albums SET status='resolving', updated_at=? WHERE ia_identifier=?`,
			now, id,
		); err != nil {
			return nil, fmt.Errorf("update album %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return identifiers, nil
}

func MarkAlbumResolved(db *sql.DB, identifier, title, creator, collection, artURL string, trackCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE albums SET status='resolved', title=?, creator=?, collection=?, art_url=?, track_count=?, updated_at=?
		 WHERE ia_identifier=?`,
		title, creator, collection, artURL, trackCount, now, identifier,
	)
	return err
}

func MarkAlbumFailed(db *sql.DB, identifier string, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE albums SET status='failed', error_message=?, updated_at=? WHERE ia_identifier=?`,
		errMsg, now, identifier,
	)
	return err
}

func FlagAlbumPoorQuality(sqlDB *sql.DB, trackID int, albumID string, reason string) (int64, error) {
	tx, err := sqlDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	_, err = tx.Exec(
		`UPDATE tracks SET status='failed', error_message=?, updated_at=? WHERE id=?`,
		reason, now, trackID,
	)
	if err != nil {
		return 0, err
	}

	result, err := tx.Exec(
		`UPDATE tracks SET status='failed', error_message=?, updated_at=?
		 WHERE album_id=? AND status='pending'`,
		reason, now, albumID,
	)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(
		`UPDATE albums SET status='failed', error_message=?, updated_at=? WHERE ia_identifier=?`,
		reason, now, albumID,
	)
	if err != nil {
		return 0, err
	}

	skipped, _ := result.RowsAffected()
	return skipped, tx.Commit()
}

func InsertTracks(db *sql.DB, albumID string, tracks []TrackInsert) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO tracks(album_id, filename, title, track_number, format, bitrate, duration, download_url)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var inserted int
	for _, t := range tracks {
		res, err := stmt.Exec(albumID, t.Filename, t.Title, t.TrackNumber, t.Format, t.Bitrate, t.Duration, t.DownloadURL)
		if err != nil {
			return inserted, fmt.Errorf("insert track %s: %w", t.Filename, err)
		}
		n, _ := res.RowsAffected()
		inserted += int(n)
	}

	if err := tx.Commit(); err != nil {
		return inserted, fmt.Errorf("commit: %w", err)
	}

	return inserted, nil
}

func ClaimNextTrackBatch(db *sql.DB, workerID string, batchSize int) ([]ClaimedTrack, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := tx.Query(
		`SELECT id, album_id, filename, title, download_url FROM tracks
		 WHERE status = 'pending'
		 ORDER BY album_id, track_number, created_at
		 LIMIT ?`,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("select pending tracks: %w", err)
	}
	defer rows.Close()

	var tracks []ClaimedTrack
	for rows.Next() {
		var t ClaimedTrack
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Filename, &t.Title, &t.DownloadURL); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(tracks) == 0 {
		return nil, nil
	}

	for _, t := range tracks {
		if _, err := tx.Exec(
			`UPDATE tracks SET status='processing', worker_id=?, locked_at=?, updated_at=? WHERE id=?`,
			workerID, now, now, t.ID,
		); err != nil {
			return nil, fmt.Errorf("update track %d: %w", t.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return tracks, nil
}

func MarkTrackCompleted(db *sql.DB, trackID int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE tracks SET status='completed', updated_at=? WHERE id=?`,
		now, trackID,
	)
	return err
}

func MarkTrackFailed(db *sql.DB, trackID int, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE tracks SET status='failed', error_message=?, retry_count=retry_count+1, updated_at=? WHERE id=?`,
		errMsg, now, trackID,
	)
	return err
}

func ResetStuckTracks(db *sql.DB, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
	result, err := db.Exec(
		`UPDATE tracks SET status='pending', worker_id=NULL, locked_at=NULL, updated_at=datetime('now')
		 WHERE status='processing' AND locked_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func RequeueTrackForRetry(db *sql.DB, trackID int, maxRetries int, errMsg string) (bool, error) {
	var retryCount int
	err := db.QueryRow(`SELECT retry_count FROM tracks WHERE id = ?`, trackID).Scan(&retryCount)
	if err != nil {
		return false, err
	}
	if retryCount >= maxRetries {
		return false, MarkTrackFailed(db, trackID, errMsg)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`UPDATE tracks SET status='pending', worker_id=NULL, locked_at=NULL, retry_count=retry_count+1, error_message=?, updated_at=? WHERE id=?`,
		errMsg, now, trackID,
	)
	return err == nil, err
}

func RequeueAlbumForRetry(db *sql.DB, identifier string, maxRetries int, errMsg string) (bool, error) {
	var retryCount int
	err := db.QueryRow(`SELECT retry_count FROM albums WHERE ia_identifier = ?`, identifier).Scan(&retryCount)
	if err != nil {
		return false, err
	}
	if retryCount >= maxRetries {
		return false, MarkAlbumFailed(db, identifier, errMsg)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`UPDATE albums SET status='pending', retry_count=retry_count+1, error_message=?, updated_at=? WHERE ia_identifier=?`,
		errMsg, now, identifier,
	)
	return err == nil, err
}

func ResetAllFailed(db *sql.DB) (int64, int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	albumResult, err := db.Exec(
		`UPDATE albums SET status='pending', error_message=NULL, retry_count=0, updated_at=? WHERE status='failed'`,
		now,
	)
	if err != nil {
		return 0, 0, err
	}
	albumCount, _ := albumResult.RowsAffected()

	trackResult, err := db.Exec(
		`UPDATE tracks SET status='pending', error_message=NULL, worker_id=NULL, locked_at=NULL, retry_count=0, updated_at=? WHERE status='failed'`,
		now,
	)
	if err != nil {
		return albumCount, 0, err
	}
	trackCount, _ := trackResult.RowsAffected()

	return albumCount, trackCount, nil
}

func GetPendingTrackCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT count(*) FROM tracks WHERE status='pending'`).Scan(&count)
	return count, err
}

func GetCompletedTrackCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT count(*) FROM tracks WHERE status='completed'`).Scan(&count)
	return count, err
}

func SearchCompletedTracks(sqlDB *sql.DB, query string, limit, offset int) ([]TrackResult, int, error) {
	var totalCount int
	var rows *sql.Rows
	var err error

	if query == "" {
		err = sqlDB.QueryRow(`SELECT count(*) FROM tracks t
			INNER JOIN track_embeddings e ON t.id = e.track_id
			WHERE t.status = 'completed'`).Scan(&totalCount)
		if err != nil {
			return nil, 0, fmt.Errorf("count: %w", err)
		}
		rows, err = sqlDB.Query(`SELECT t.id, COALESCE(t.title, t.filename), t.filename, t.album_id,
				COALESCE(a.title, a.ia_identifier), t.download_url, COALESCE(e.quality_score, 0.0)
			FROM tracks t
			INNER JOIN track_embeddings e ON t.id = e.track_id
			INNER JOIN albums a ON t.album_id = a.ia_identifier
			WHERE t.status = 'completed'
			ORDER BY a.downloads DESC, e.quality_score DESC, t.updated_at DESC
			LIMIT ? OFFSET ?`, limit, offset)
	} else {
		pattern := "%" + query + "%"
		err = sqlDB.QueryRow(`SELECT count(*) FROM tracks t
			INNER JOIN track_embeddings e ON t.id = e.track_id
			INNER JOIN albums a ON t.album_id = a.ia_identifier
			WHERE t.status = 'completed'
			  AND (t.title LIKE ? OR t.filename LIKE ? OR t.album_id LIKE ? OR a.title LIKE ?)`,
			pattern, pattern, pattern, pattern).Scan(&totalCount)
		if err != nil {
			return nil, 0, fmt.Errorf("count: %w", err)
		}
		rows, err = sqlDB.Query(`SELECT t.id, COALESCE(t.title, t.filename), t.filename, t.album_id,
				COALESCE(a.title, a.ia_identifier), t.download_url, COALESCE(e.quality_score, 0.0)
			FROM tracks t
			INNER JOIN track_embeddings e ON t.id = e.track_id
			INNER JOIN albums a ON t.album_id = a.ia_identifier
			WHERE t.status = 'completed'
			  AND (t.title LIKE ? OR t.filename LIKE ? OR t.album_id LIKE ? OR a.title LIKE ?)
			ORDER BY t.updated_at DESC
			LIMIT ? OFFSET ?`, pattern, pattern, pattern, pattern, limit, offset)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var results []TrackResult
	for rows.Next() {
		var r TrackResult
		if err := rows.Scan(&r.TrackID, &r.Title, &r.Filename, &r.AlbumID, &r.AlbumTitle, &r.DownloadURL, &r.QualityScore); err != nil {
			return nil, 0, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return results, totalCount, nil
}

func SearchAlbums(sqlDB *sql.DB, query string, limit, offset int) ([]AlbumResult, int, error) {
	var totalCount int
	var rows *sql.Rows
	var err error

	if query == "" {
		err = sqlDB.QueryRow(`SELECT count(*) FROM albums WHERE status = 'resolved'`).Scan(&totalCount)
		if err != nil {
			return nil, 0, fmt.Errorf("count: %w", err)
		}
		rows, err = sqlDB.Query(`SELECT a.ia_identifier, COALESCE(a.title, a.ia_identifier), COALESCE(a.creator, ''),
				COALESCE(a.collection, ''), COALESCE(a.art_url, ''), a.track_count,
				a.status, COALESCE((SELECT count(*) FROM tracks t WHERE t.album_id = a.ia_identifier AND t.status = 'completed'), 0),
				COALESCE(a.downloads, 0),
				COALESCE((SELECT AVG(e.quality_score) FROM tracks t INNER JOIN track_embeddings e ON t.id = e.track_id WHERE t.album_id = a.ia_identifier AND t.status = 'completed'), 0.0)
			FROM albums a
			WHERE a.status = 'resolved'
			ORDER BY a.downloads DESC, a.updated_at DESC
			LIMIT ? OFFSET ?`, limit, offset)
	} else {
		pattern := "%" + query + "%"
		err = sqlDB.QueryRow(`SELECT count(*) FROM albums
			WHERE status = 'resolved'
			  AND (ia_identifier LIKE ? OR title LIKE ? OR creator LIKE ? OR collection LIKE ?)`,
			pattern, pattern, pattern, pattern).Scan(&totalCount)
		if err != nil {
			return nil, 0, fmt.Errorf("count: %w", err)
		}
		rows, err = sqlDB.Query(`SELECT a.ia_identifier, COALESCE(a.title, a.ia_identifier), COALESCE(a.creator, ''),
				COALESCE(a.collection, ''), COALESCE(a.art_url, ''), a.track_count,
				a.status, COALESCE((SELECT count(*) FROM tracks t WHERE t.album_id = a.ia_identifier AND t.status = 'completed'), 0),
				COALESCE(a.downloads, 0),
				COALESCE((SELECT AVG(e.quality_score) FROM tracks t INNER JOIN track_embeddings e ON t.id = e.track_id WHERE t.album_id = a.ia_identifier AND t.status = 'completed'), 0.0)
			FROM albums a
			WHERE a.status = 'resolved'
			  AND (a.ia_identifier LIKE ? OR a.title LIKE ? OR a.creator LIKE ? OR a.collection LIKE ?)
			ORDER BY a.downloads DESC, a.updated_at DESC
			LIMIT ? OFFSET ?`, pattern, pattern, pattern, pattern, limit, offset)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var results []AlbumResult
	for rows.Next() {
		var r AlbumResult
		if err := rows.Scan(&r.IAIdentifier, &r.Title, &r.Creator, &r.Collection, &r.ArtURL, &r.TrackCount, &r.Status, &r.CompletedCount, &r.Downloads, &r.AvgQuality); err != nil {
			return nil, 0, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return results, totalCount, nil
}

func GetAlbumTracks(sqlDB *sql.DB, albumID string) ([]TrackDetail, error) {
	rows, err := sqlDB.Query(`SELECT t.id, t.filename, COALESCE(t.title, t.filename), COALESCE(t.track_number, 0),
			COALESCE(t.format, ''), t.download_url, t.status,
			COALESCE(e.quality_score, 0.0)
		FROM tracks t
		LEFT JOIN track_embeddings e ON t.id = e.track_id
		WHERE t.album_id = ?
		ORDER BY t.track_number, t.filename`, albumID)
	if err != nil {
		return nil, fmt.Errorf("query album tracks: %w", err)
	}
	defer rows.Close()

	var results []TrackDetail
	for rows.Next() {
		var r TrackDetail
		if err := rows.Scan(&r.ID, &r.Filename, &r.Title, &r.TrackNumber, &r.Format, &r.DownloadURL, &r.Status, &r.QualityScore); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func GetAlbumByID(sqlDB *sql.DB, albumID string) (*AlbumResult, error) {
	var r AlbumResult
	err := sqlDB.QueryRow(`SELECT a.ia_identifier, COALESCE(a.title, a.ia_identifier), COALESCE(a.creator, ''),
			COALESCE(a.collection, ''), COALESCE(a.art_url, ''), a.track_count, a.status,
			COALESCE((SELECT count(*) FROM tracks t WHERE t.album_id = a.ia_identifier AND t.status = 'completed'), 0),
			COALESCE(a.downloads, 0),
			COALESCE((SELECT AVG(e.quality_score) FROM tracks t INNER JOIN track_embeddings e ON t.id = e.track_id WHERE t.album_id = a.ia_identifier AND t.status = 'completed'), 0.0)
		FROM albums a WHERE a.ia_identifier = ?`, albumID).
		Scan(&r.IAIdentifier, &r.Title, &r.Creator, &r.Collection, &r.ArtURL, &r.TrackCount, &r.Status, &r.CompletedCount, &r.Downloads, &r.AvgQuality)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
