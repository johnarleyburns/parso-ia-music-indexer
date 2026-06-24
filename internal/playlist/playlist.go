package playlist

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"
	"github.com/johnarleyburns/parso-ia-music-indexer/internal/ia"
)

type CreateInput struct {
	Name     string
	Query    string
	Limit    int
	ParentID string
}

type ImportInput struct {
	ParentID string
	ListName string
	Title    string
}

type ProgressCallback func(state string, current, total int)

func CreateFromSearch(sqlDB *db.DB, iaClient *http.Client, creds *ia.IACredentials, input CreateInput, onProgress ProgressCallback) (int, error) {
	if input.ParentID == "" {
		input.ParentID = creds.FavCollection()
	}
	if input.ParentID == "" {
		return 0, fmt.Errorf("no parent ID: set --parent or ensure IA_USERNAME is configured")
	}
	if input.Limit <= 0 {
		input.Limit = 50
	}
	if input.Limit > 500 {
		input.Limit = 500
	}

	onProgress("Searching IA...", 0, input.Limit)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	results, total, err := ia.AdvancedSearch(ctx, iaClient, input.Query, input.Limit)
	cancel()
	if err != nil {
		return 0, fmt.Errorf("search: %w", err)
	}

	if len(results) == 0 {
		return 0, fmt.Errorf("no results for query: %s", input.Query)
	}

	log.Printf("[playlist] search returned %d results (total=%d) for query=%q", len(results), total, input.Query)

	onProgress(fmt.Sprintf("Found %d results, adding to list...", len(results)), 0, len(results))

	collectionID := input.ParentID
	queryForDB := input.Query
	title := input.Name
	if title == "" {
		title = input.Query
	}

	listName := input.Name
	if listName == "" {
		listName = input.Query
	}

	albums := make([]db.AlbumInsert, len(results))
	identifiers := make([]string, len(results))
	for i, r := range results {
		identifiers[i] = r.Identifier
		albums[i] = db.AlbumInsert{
			Identifier: r.Identifier,
			Downloads:  r.Downloads,
		}
	}

	db.BulkInsertAlbums(sqlDB.Conn, albums)

	if err := db.BulkLinkAlbumsToCollection(sqlDB.Conn, collectionID, identifiers); err != nil {
		return 0, fmt.Errorf("link albums: %w", err)
	}

	insertErr := db.InsertCollection(sqlDB.Conn, db.CollectionInsert{
		CollectionID:  collectionID,
		Title:         title,
		Query:         queryForDB,
		ExpectedCount: len(results),
		SourceType:    "playlist",
		ListName:      listName,
		ParentID:      input.ParentID,
	})
	if insertErr != nil {
		log.Printf("[playlist] insert collection warning: %v", insertErr)
	}

	for i, r := range results {
		select {
		case <-time.After(200 * time.Millisecond):
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := ia.AddToList(ctx, iaClient, creds, input.ParentID, listName, r.Identifier)
		cancel()
		if err != nil {
			log.Printf("[playlist] failed to add %s to list: %v", r.Identifier, err)
		}

		onProgress(fmt.Sprintf("Adding to IA list..."), i+1, len(results))
	}

	onProgress(fmt.Sprintf("Done! %d items added to playlist", len(results)), len(results), len(results))

	return len(results), nil
}

func ImportExistingPlaylist(sqlDB *db.DB, iaClient *http.Client, input ImportInput, onProgress ProgressCallback) (int, error) {
	if input.ParentID == "" {
		return 0, fmt.Errorf("parent_id is required for importing an existing playlist")
	}
	if input.ListName == "" {
		return 0, fmt.Errorf("list_name is required")
	}
	if input.Title == "" {
		input.Title = input.ListName
	}

	onProgress("Fetching playlist items from IA...", 0, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	entries, err := ia.ListItems(ctx, iaClient, input.ParentID, input.ListName)
	cancel()
	if err != nil {
		return 0, fmt.Errorf("fetch list items: %w", err)
	}

	if len(entries) == 0 {
		return 0, fmt.Errorf("no items found in playlist %s/%s", input.ParentID, input.ListName)
	}

	log.Printf("[playlist] import: found %d items in %s/%s", len(entries), input.ParentID, input.ListName)

	onProgress(fmt.Sprintf("Found %d items, importing...", len(entries)), 0, len(entries))

	collectionID := input.ParentID
	queryForDB := fmt.Sprintf("simplelists__%s:%s", input.ListName, input.ParentID)

	albums := make([]db.AlbumInsert, len(entries))
	identifiers := make([]string, len(entries))
	for i, e := range entries {
		identifiers[i] = e.Identifier
		albums[i] = db.AlbumInsert{Identifier: e.Identifier, Downloads: 0}
	}

	db.BulkInsertAlbums(sqlDB.Conn, albums)

	if err := db.BulkLinkAlbumsToCollection(sqlDB.Conn, collectionID, identifiers); err != nil {
		return 0, fmt.Errorf("link albums: %w", err)
	}

	insertErr := db.InsertCollection(sqlDB.Conn, db.CollectionInsert{
		CollectionID:  collectionID,
		Title:         input.Title,
		Query:         queryForDB,
		ExpectedCount: len(entries),
		SourceType:    "playlist",
		ListName:      input.ListName,
		ParentID:      input.ParentID,
	})
	if insertErr != nil {
		log.Printf("[playlist] insert collection warning: %v", insertErr)
	}

	onProgress(fmt.Sprintf("Done! %d items imported", len(entries)), len(entries), len(entries))

	return len(entries), nil
}

func ImportPatronList(sqlDB *db.DB, iaClient *http.Client, rawURL, title string, onProgress ProgressCallback) (int, error) {
	onProgress("Fetching playlist from IA...", 0, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	list, err := ia.FetchPatronList(ctx, iaClient, rawURL)
	cancel()
	if err != nil {
		return 0, fmt.Errorf("fetch patron list: %w", err)
	}

	members := list.Value.Members
	if len(members) == 0 {
		return 0, fmt.Errorf("no items found in playlist")
	}

	log.Printf("[playlist] patron import: found %d items in list %q", len(members), list.Value.ListName)

	if title == "" {
		title = list.Value.ListName
	}

	onProgress(fmt.Sprintf("Found %d items, importing...", len(members)), 0, len(members))

	apiURL, _ := ia.ListAPIURL(rawURL)
	collectionID := fmt.Sprintf("@%d-%s", list.Value.ID, strings.ToLower(strings.ReplaceAll(list.Value.ListName, " ", "-")))
	queryForDB := apiURL

	albums := make([]db.AlbumInsert, len(members))
	identifiers := make([]string, len(members))
	for i, m := range members {
		identifiers[i] = m.Identifier
		albums[i] = db.AlbumInsert{Identifier: m.Identifier, Downloads: 0}
	}

	db.BulkInsertAlbums(sqlDB.Conn, albums)

	if err := db.BulkLinkAlbumsToCollection(sqlDB.Conn, collectionID, identifiers); err != nil {
		return 0, fmt.Errorf("link albums: %w", err)
	}

	insertErr := db.InsertCollection(sqlDB.Conn, db.CollectionInsert{
		CollectionID:  collectionID,
		Title:         title,
		Query:         queryForDB,
		ExpectedCount: len(members),
		SourceType:    "playlist",
	})
	if insertErr != nil {
		log.Printf("[playlist] insert collection warning: %v", insertErr)
	}

	for i, m := range members {
		onProgress(fmt.Sprintf("Importing..."), i+1, len(members))
		_ = m
	}

	onProgress(fmt.Sprintf("Done! %d items imported", len(members)), len(members), len(members))

	return len(members), nil
}
