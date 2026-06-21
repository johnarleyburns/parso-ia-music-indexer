package db

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed seed_collections.json
var seedCollectionsJSON []byte

type seedCollection struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Category  string `json:"category"`
	Curator   string `json:"curator"`
	URL       string `json:"url"`
	Query     string `json:"query"`
	ItemCount int    `json:"item_count"`
	Notes     string `json:"notes"`
}

func SeedCollectionsIfEmpty(sqlDB *sql.DB) (int64, error) {
	count, err := GetCollectionCount(sqlDB)
	if err != nil {
		return 0, fmt.Errorf("check collection count: %w", err)
	}
	if count > 0 {
		return 0, nil
	}
	return SeedCollections(sqlDB)
}

func SeedCollections(sqlDB *sql.DB) (int64, error) {
	var seeds []seedCollection
	if err := json.Unmarshal(seedCollectionsJSON, &seeds); err != nil {
		return 0, fmt.Errorf("parse seed data: %w", err)
	}

	inserts := make([]CollectionInsert, len(seeds))
	for i, s := range seeds {
		inserts[i] = CollectionInsert{
			CollectionID:  s.ID,
			Title:         s.Title,
			Description:   s.Notes,
			Category:      s.Category,
			Curator:       s.Curator,
			URL:           s.URL,
			Query:         s.Query,
			ExpectedCount: s.ItemCount,
		}
	}

	return BulkInsertCollections(sqlDB, inserts)
}
