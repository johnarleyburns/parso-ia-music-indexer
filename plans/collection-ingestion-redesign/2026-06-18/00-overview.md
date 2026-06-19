# Collection-Based Ingestion Redesign

## Problem
Ingesting albums by popularity (downloads desc) via a global scrape is futile - the dataset is too large and unfocused.

## Solution
Replace popularity-based scraping with collection-based ingestion. Users configure IA collections, and the coordinator discovers albums per-collection.

## Schema Changes
- New `collections` table: stores IA collection metadata, cursor state, discovery status
- New `collection_albums` junction table: many-to-many (albums can belong to multiple collections)
- Remove `cursor_state` table (cursor now lives per-collection)
- Keep `downloads` column on albums (populated by scrape, useful for browse ordering)

## Coordinator Redesign
- On start: reset all collection statuses to 'pending', clear cursors (fresh sync)
- Iterate each collection, use scrape API with `collection:<id>` query
- Paginate through all results, insert albums + collection_albums links
- Track per-collection progress (discovered_count, status)
- Emit collection-level events for UI

## UI Changes
- Dashboard: show collection stats (total, pending, discovering, discovered)
- Coordinator section: show current collection being processed
- Status bar: collection progress

## Seeding
- Pre-seed DB with 26 curated IA collections from JSON
- Seed runs on startup if collections table is empty

## Phases
1. Schema + DB layer (db.go, collections.go, cursor.go, queue.go)
2. Seed data (seed_collections.json, embed + seed logic)
3. Coordinator rewrite (cmd/tui/main.go)
4. UI updates (dashboard.go, events.go, statusbar.go)
5. Cleanup (remove dead code, update types.go)
6. Build + test
