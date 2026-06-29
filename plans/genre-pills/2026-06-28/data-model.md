# Data Model

## New table: pills
```sql
CREATE TABLE IF NOT EXISTS pills (
  pill_id            TEXT PRIMARY KEY,
  label              TEXT NOT NULL,
  clap_prompt        TEXT NOT NULL,   -- CLAP text-embedding prompt
  keywords           TEXT NOT NULL,   -- comma-separated recovery keywords (subjects/tags)
  sort_order         INTEGER NOT NULL DEFAULT 0,
  enabled            INTEGER NOT NULL DEFAULT 1,
  min_library_count  INTEGER NOT NULL DEFAULT 10,
  created_at         TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at         TEXT NOT NULL DEFAULT (datetime('now'))
);
```
Seeded from `internal/db/pills.json` with INSERT OR IGNORE so manual DB tuning persists.

## Keyword source rationale
Use `albums.subjects` + `tracks.tags` (populated, rich) rather than `albums.genres`
(near-empty). Keywords per pill derived from observed subject tokens (see overview).

## Listenable pool (coverage denominator)
albums with >=1 track where:
- t.status='completed' AND a track embedding exists, AND
- listenability not excluded/longform (mirrors SearchByText filters).
