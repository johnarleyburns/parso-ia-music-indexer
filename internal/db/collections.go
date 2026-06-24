package db

import (
	"database/sql"
	"fmt"
	"time"
)

type Collection struct {
	CollectionID    string
	Title           string
	Description     string
	Category        string
	Curator         string
	URL             string
	Query           string
	ExpectedCount   int
	DiscoveredCount int
	Status          string
	LastCursor      string
	ErrorMessage    string
	LastSyncedAt    string
	SourceType      string
	ListName        string
	ParentID        string
}

func (c Collection) IAURL() string {
	if c.SourceType == "playlist" || c.SourceType == "simplelist" {
		if c.ParentID != "" {
			return "https://archive.org/details/" + c.ParentID
		}
	}
	if c.URL != "" {
		return c.URL
	}
	return "https://archive.org/details/" + c.CollectionID
}

type CollectionInsert struct {
	CollectionID  string
	Title         string
	Description   string
	Category      string
	Curator       string
	URL           string
	Query         string
	ExpectedCount int
	SourceType    string
	ListName      string
	ParentID      string
}

type CollectionStats struct {
	Total       int
	Pending     int
	Discovering int
	Discovered  int
	Failed      int
}

type CollectionTrackStat struct {
	Total    int
	Analyzed int
}

func InsertCollection(db *sql.DB, c CollectionInsert) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO collections(collection_id, title, description, category, curator, url, query, expected_count, source_type, list_name, parent_id)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.CollectionID, c.Title, c.Description, c.Category, c.Curator, c.URL, c.Query, c.ExpectedCount, c.SourceType, c.ListName, c.ParentID,
	)
	return err
}

func BulkInsertCollections(db *sql.DB, collections []CollectionInsert) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO collections(collection_id, title, description, category, curator, url, query, expected_count, source_type, list_name, parent_id)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var inserted int64
	for _, c := range collections {
		res, err := stmt.Exec(c.CollectionID, c.Title, c.Description, c.Category, c.Curator, c.URL, c.Query, c.ExpectedCount, c.SourceType, c.ListName, c.ParentID)
		if err != nil {
			return inserted, fmt.Errorf("insert %s: %w", c.CollectionID, err)
		}
		n, _ := res.RowsAffected()
		inserted += n
	}

	if err := tx.Commit(); err != nil {
		return inserted, fmt.Errorf("commit: %w", err)
	}
	return inserted, nil
}

func RemoveCollection(db *sql.DB, collectionID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM collection_albums WHERE collection_id = ?`, collectionID)
	if err != nil {
		return fmt.Errorf("delete collection_albums: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM collections WHERE collection_id = ?`, collectionID)
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}

	return tx.Commit()
}

func GetAllCollections(db *sql.DB) ([]Collection, error) {
	rows, err := db.Query(
		`SELECT collection_id, title, COALESCE(description,''), COALESCE(category,''),
		        COALESCE(curator,''), COALESCE(url,''), query, expected_count,
		        discovered_count, status, COALESCE(last_cursor,''),
		        COALESCE(error_message,''), COALESCE(last_synced_at,''),
		        COALESCE(source_type,'collection'), COALESCE(list_name,''), COALESCE(parent_id,'')
		 FROM collections ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query collections: %w", err)
	}
	defer rows.Close()

	var result []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(
			&c.CollectionID, &c.Title, &c.Description, &c.Category,
			&c.Curator, &c.URL, &c.Query, &c.ExpectedCount,
			&c.DiscoveredCount, &c.Status, &c.LastCursor,
			&c.ErrorMessage, &c.LastSyncedAt,
			&c.SourceType, &c.ListName, &c.ParentID,
		); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func GetPendingCollections(db *sql.DB) ([]Collection, error) {
	rows, err := db.Query(
		`SELECT collection_id, title, COALESCE(description,''), COALESCE(category,''),
		        COALESCE(curator,''), COALESCE(url,''), query, expected_count,
		        discovered_count, status, COALESCE(last_cursor,''),
		        COALESCE(error_message,''), COALESCE(last_synced_at,''),
		        COALESCE(source_type,'collection'), COALESCE(list_name,''), COALESCE(parent_id,'')
		 FROM collections WHERE status IN ('pending','discovering') ORDER BY expected_count`,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending collections: %w", err)
	}
	defer rows.Close()

	var result []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(
			&c.CollectionID, &c.Title, &c.Description, &c.Category,
			&c.Curator, &c.URL, &c.Query, &c.ExpectedCount,
			&c.DiscoveredCount, &c.Status, &c.LastCursor,
			&c.ErrorMessage, &c.LastSyncedAt,
			&c.SourceType, &c.ListName, &c.ParentID,
		); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func GetCollectionCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT count(*) FROM collections`).Scan(&count)
	return count, err
}

func GetCollectionStats(db *sql.DB) (*CollectionStats, error) {
	s := &CollectionStats{}
	rows, err := db.Query(`SELECT status, count(*) FROM collections GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("collection stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		s.Total += count
		switch status {
		case "pending":
			s.Pending = count
		case "discovering":
			s.Discovering = count
		case "discovered":
			s.Discovered = count
		case "failed":
			s.Failed = count
		}
	}
	return s, rows.Err()
}

func GetCollectionTrackStats(db *sql.DB) (map[string]CollectionTrackStat, error) {
	rows, err := db.Query(
		`SELECT ca.collection_id,
		        count(t.id) AS total,
		        count(CASE WHEN t.status='completed' THEN 1 END) AS analyzed
		 FROM collection_albums ca
		 JOIN tracks t ON t.album_id = ca.album_id
		 GROUP BY ca.collection_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("collection track stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]CollectionTrackStat)
	for rows.Next() {
		var collectionID string
		var s CollectionTrackStat
		if err := rows.Scan(&collectionID, &s.Total, &s.Analyzed); err != nil {
			return nil, err
		}
		stats[collectionID] = s
	}
	return stats, rows.Err()
}

func MarkCollectionDiscovering(db *sql.DB, collectionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE collections SET status='discovering', updated_at=? WHERE collection_id=?`,
		now, collectionID,
	)
	return err
}

func MarkCollectionDiscovered(db *sql.DB, collectionID string, discoveredCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE collections SET status='discovered', discovered_count=?, last_synced_at=?, last_cursor='', updated_at=?
		 WHERE collection_id=?`,
		discoveredCount, now, now, collectionID,
	)
	return err
}

func MarkCollectionFailed(db *sql.DB, collectionID string, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE collections SET status='failed', error_message=?, updated_at=? WHERE collection_id=?`,
		errMsg, now, collectionID,
	)
	return err
}

func SaveCollectionCursor(db *sql.DB, collectionID string, cursor string, discoveredCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE collections SET last_cursor=?, discovered_count=?, updated_at=? WHERE collection_id=?`,
		cursor, discoveredCount, now, collectionID,
	)
	return err
}

func ResetAllCollectionsForSync(db *sql.DB) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE collections SET status='pending', last_cursor='', discovered_count=0, error_message=NULL, updated_at=?`,
		now,
	)
	return err
}

func LinkAlbumToCollection(db *sql.DB, collectionID, albumID string) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO collection_albums(collection_id, album_id) VALUES(?, ?)`,
		collectionID, albumID,
	)
	return err
}

func BulkLinkAlbumsToCollection(sqlDB *sql.DB, collectionID string, albumIDs []string) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO collection_albums(collection_id, album_id) VALUES(?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, id := range albumIDs {
		if _, err := stmt.Exec(collectionID, id); err != nil {
			return fmt.Errorf("link %s to %s: %w", id, collectionID, err)
		}
	}

	return tx.Commit()
}

func ReplaceCollectionAlbums(sqlDB *sql.DB, collectionID string, albumIDs []string) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM collection_albums WHERE collection_id = ?`, collectionID); err != nil {
		return fmt.Errorf("clear collection_albums: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO collection_albums(collection_id, album_id) VALUES(?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, id := range albumIDs {
		if _, err := stmt.Exec(collectionID, id); err != nil {
			return fmt.Errorf("link %s to %s: %w", id, collectionID, err)
		}
	}

	return tx.Commit()
}

func GetCollectionAlbumCount(db *sql.DB, collectionID string) (int, error) {
	var count int
	err := db.QueryRow(
		`SELECT count(*) FROM collection_albums WHERE collection_id = ?`, collectionID,
	).Scan(&count)
	return count, err
}
