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

type ProgressCallback func(state string, current, total int)

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

	db.MarkCollectionDiscovered(sqlDB.Conn, collectionID, len(members))

	for i, m := range members {
		onProgress(fmt.Sprintf("Importing..."), i+1, len(members))
		_ = m
	}

	onProgress(fmt.Sprintf("Done! %d items imported", len(members)), len(members), len(members))

	return len(members), nil
}

func SyncPlaylist(sqlDB *db.DB, iaClient *http.Client, col db.Collection, onProgress ProgressCallback) (int, error) {
	if col.SourceType != "playlist" && col.SourceType != "simplelist" {
		return 0, fmt.Errorf("collection %q is not a playlist (source_type=%q)", col.CollectionID, col.SourceType)
	}

	onProgress("Fetching playlist from IA...", 0, 0)

	var identifiers []string

	if col.ParentID != "" && col.ListName != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		entries, err := ia.ListItems(ctx, iaClient, col.ParentID, col.ListName)
		cancel()
		if err != nil {
			return 0, fmt.Errorf("fetch list items: %w", err)
		}
		identifiers = make([]string, len(entries))
		for i, e := range entries {
			identifiers[i] = e.Identifier
		}
	} else {
		if col.Query == "" {
			return 0, fmt.Errorf("playlist %q has no list URL to sync from", col.CollectionID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		list, err := ia.FetchPatronList(ctx, iaClient, col.Query)
		cancel()
		if err != nil {
			return 0, fmt.Errorf("fetch patron list: %w", err)
		}
		members := list.Value.Members
		identifiers = make([]string, len(members))
		for i, m := range members {
			identifiers[i] = m.Identifier
		}
	}

	if len(identifiers) == 0 {
		return 0, fmt.Errorf("no items found when syncing playlist %q", col.CollectionID)
	}

	onProgress(fmt.Sprintf("Found %d items, syncing...", len(identifiers)), 0, len(identifiers))

	albums := make([]db.AlbumInsert, len(identifiers))
	for i, id := range identifiers {
		albums[i] = db.AlbumInsert{Identifier: id, Downloads: 0}
	}

	if _, err := db.BulkInsertAlbums(sqlDB.Conn, albums); err != nil {
		return 0, fmt.Errorf("insert albums: %w", err)
	}

	if err := db.ReplaceCollectionAlbums(sqlDB.Conn, col.CollectionID, identifiers); err != nil {
		return 0, fmt.Errorf("replace collection albums: %w", err)
	}

	if err := db.MarkCollectionDiscovered(sqlDB.Conn, col.CollectionID, len(identifiers)); err != nil {
		return 0, fmt.Errorf("mark discovered: %w", err)
	}

	log.Printf("[playlist] sync: %s now has %d items", col.CollectionID, len(identifiers))
	onProgress(fmt.Sprintf("Done! Synced %d items", len(identifiers)), len(identifiers), len(identifiers))

	return len(identifiers), nil
}
