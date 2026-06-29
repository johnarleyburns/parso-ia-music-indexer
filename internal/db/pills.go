package db

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed pills.json
var seedPillsJSON []byte

// Pill is a selectable genre/mood chip the listener app surfaces for cold-start
// discovery. clap_prompt is embedded with the CLAP text encoder for text->audio
// similarity; keywords are matched lexically against album subjects and track
// tags (see ComputePillScore and CountPillCoverage).
type Pill struct {
	PillID          string
	Label           string
	ClapPrompt      string
	Keywords        string
	SortOrder       int
	Enabled         bool
	MinLibraryCount int
}

// ActivePill is a Pill that passed the coverage gate, carrying the live count of
// listenable albums matching its keywords.
type ActivePill struct {
	Pill
	LibraryCount int
}

type seedPill struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	ClapPrompt      string `json:"clap_prompt"`
	Keywords        string `json:"keywords"`
	SortOrder       int    `json:"sort_order"`
	Enabled         *bool  `json:"enabled"`
	MinLibraryCount *int   `json:"min_library_count"`
}

// SeedPillsIfEmpty seeds the pills table from embedded JSON only when empty.
func SeedPillsIfEmpty(sqlDB *sql.DB) (int64, error) {
	count, err := GetPillCount(sqlDB)
	if err != nil {
		return 0, fmt.Errorf("check pill count: %w", err)
	}
	if count > 0 {
		return 0, nil
	}
	return SeedPills(sqlDB)
}

// SeedPills upserts pills from embedded JSON using INSERT OR IGNORE so any
// in-database tuning of existing pills is preserved across restarts.
func SeedPills(sqlDB *sql.DB) (int64, error) {
	var seeds []seedPill
	if err := json.Unmarshal(seedPillsJSON, &seeds); err != nil {
		return 0, fmt.Errorf("parse pills seed: %w", err)
	}

	inserts := make([]Pill, len(seeds))
	for i, s := range seeds {
		enabled := true
		if s.Enabled != nil {
			enabled = *s.Enabled
		}
		minCount := 10
		if s.MinLibraryCount != nil {
			minCount = *s.MinLibraryCount
		}
		inserts[i] = Pill{
			PillID:          s.ID,
			Label:           s.Label,
			ClapPrompt:      s.ClapPrompt,
			Keywords:        s.Keywords,
			SortOrder:       s.SortOrder,
			Enabled:         enabled,
			MinLibraryCount: minCount,
		}
	}
	return BulkInsertPills(sqlDB, inserts)
}

func BulkInsertPills(db *sql.DB, pills []Pill) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO pills(pill_id, label, clap_prompt, keywords, sort_order, enabled, min_library_count)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var inserted int64
	for _, p := range pills {
		res, err := stmt.Exec(p.PillID, p.Label, p.ClapPrompt, p.Keywords, p.SortOrder, boolToInt(p.Enabled), p.MinLibraryCount)
		if err != nil {
			return inserted, fmt.Errorf("insert pill %s: %w", p.PillID, err)
		}
		n, _ := res.RowsAffected()
		inserted += n
	}

	if err := tx.Commit(); err != nil {
		return inserted, fmt.Errorf("commit: %w", err)
	}
	return inserted, nil
}

func GetPillCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT count(*) FROM pills`).Scan(&count)
	return count, err
}

func GetPillByID(db *sql.DB, pillID string) (*Pill, error) {
	var p Pill
	var enabled int
	err := db.QueryRow(
		`SELECT pill_id, label, clap_prompt, keywords, sort_order, enabled, min_library_count
		 FROM pills WHERE pill_id = ?`, pillID,
	).Scan(&p.PillID, &p.Label, &p.ClapPrompt, &p.Keywords, &p.SortOrder, &enabled, &p.MinLibraryCount)
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled != 0
	return &p, nil
}

// ListAllPills returns every pill sorted by display order (for tuning/admin).
func ListAllPills(db *sql.DB) ([]Pill, error) {
	rows, err := db.Query(
		`SELECT pill_id, label, clap_prompt, keywords, sort_order, enabled, min_library_count
		 FROM pills ORDER BY sort_order, label`,
	)
	if err != nil {
		return nil, fmt.Errorf("query pills: %w", err)
	}
	defer rows.Close()

	var result []Pill
	for rows.Next() {
		var p Pill
		var enabled int
		if err := rows.Scan(&p.PillID, &p.Label, &p.ClapPrompt, &p.Keywords, &p.SortOrder, &enabled, &p.MinLibraryCount); err != nil {
			return nil, err
		}
		p.Enabled = enabled != 0
		result = append(result, p)
	}
	return result, rows.Err()
}

// ListActivePills returns enabled pills whose listenable-album coverage meets
// their min_library_count, sorted by display order, each carrying its live count.
// Pills below threshold are hidden so the UI never surfaces a pill that returns
// little or no music; they reappear automatically as the library grows.
func ListActivePills(db *sql.DB) ([]ActivePill, error) {
	all, err := ListAllPills(db)
	if err != nil {
		return nil, err
	}
	var active []ActivePill
	for _, p := range all {
		if !p.Enabled {
			continue
		}
		n, err := CountPillCoverage(db, p.Keywords)
		if err != nil {
			return nil, fmt.Errorf("coverage %s: %w", p.PillID, err)
		}
		if n >= p.MinLibraryCount {
			active = append(active, ActivePill{Pill: p, LibraryCount: n})
		}
	}
	return active, nil
}

// PillWithCount is a pill paired with its live listenable-album coverage and
// whether it currently meets its activation gate (enabled and count >= min).
type PillWithCount struct {
	Pill
	LibraryCount int
	Active       bool
}

// ListPillsWithCoverage returns every pill (sorted by display order) with its
// current listenable-album count and active flag. Used by the Pills tab so the
// user sees all pills and counts, including those below their activation gate.
func ListPillsWithCoverage(db *sql.DB) ([]PillWithCount, error) {
	all, err := ListAllPills(db)
	if err != nil {
		return nil, err
	}
	out := make([]PillWithCount, 0, len(all))
	for _, p := range all {
		n, err := CountPillCoverage(db, p.Keywords)
		if err != nil {
			return nil, fmt.Errorf("coverage %s: %w", p.PillID, err)
		}
		out = append(out, PillWithCount{
			Pill:         p,
			LibraryCount: n,
			Active:       p.Enabled && n >= p.MinLibraryCount,
		})
	}
	return out, nil
}

// PillTrack is a listenable track matching a pill's keywords, for the drill-down
// "matching tracks" view.
type PillTrack struct {
	TrackID            int
	Title              string
	AlbumID            string
	AlbumTitle         string
	AlbumCreator       string
	DownloadURL        string
	ListenabilityScore float64
	QualityScore       float64
}

// TracksForPill returns listenable tracks whose album subjects or own tags match
// any of the pill keywords, best-first by listenability then quality. Uses the
// same listenable filter as CountPillCoverage / SearchByText.
func TracksForPill(db *sql.DB, keywords string, limit int) ([]PillTrack, error) {
	clause, args := pillKeywordClause(keywords)
	if clause == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT t.id, COALESCE(NULLIF(t.title,''), t.filename), t.album_id,
			COALESCE(NULLIF(a.title,''), a.ia_identifier), COALESCE(a.creator,''),
			t.download_url, COALESCE(t.listenability_score,0), COALESCE(e.quality_score,0)
		FROM albums a
		INNER JOIN tracks t ON t.album_id = a.ia_identifier
		INNER JOIN track_embeddings e ON e.track_id = t.id
		WHERE t.status = 'completed'
		  AND (t.listenability_decision IS NULL OR t.listenability_decision != 'exclude')
		  AND (t.listenability_stream IS NULL OR t.listenability_stream != 'excluded')
		  AND (t.listenability_stream IS NULL OR t.listenability_stream != 'longform_candidate')
		  AND (` + clause + `)
		ORDER BY t.listenability_score DESC, e.quality_score DESC, t.id ASC
		LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("tracks for pill: %w", err)
	}
	defer rows.Close()

	var out []PillTrack
	for rows.Next() {
		var t PillTrack
		if err := rows.Scan(&t.TrackID, &t.Title, &t.AlbumID, &t.AlbumTitle, &t.AlbumCreator,
			&t.DownloadURL, &t.ListenabilityScore, &t.QualityScore); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CountPillCoverage returns the number of distinct listenable albums whose
// subjects or track tags match any of the pill's comma-separated keywords. The
// listenable filter mirrors SearchByText: completed tracks with an embedding that
// are not excluded or longform candidates.
func CountPillCoverage(db *sql.DB, keywords string) (int, error) {
	clause, args := pillKeywordClause(keywords)
	if clause == "" {
		return 0, nil
	}

	query := `SELECT COUNT(DISTINCT a.ia_identifier)
		FROM albums a
		INNER JOIN tracks t ON t.album_id = a.ia_identifier
		INNER JOIN track_embeddings e ON e.track_id = t.id
		WHERE t.status = 'completed'
		  AND (t.listenability_decision IS NULL OR t.listenability_decision != 'exclude')
		  AND (t.listenability_stream IS NULL OR t.listenability_stream != 'excluded')
		  AND (t.listenability_stream IS NULL OR t.listenability_stream != 'longform_candidate')
		  AND (` + clause + `)`

	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pill coverage: %w", err)
	}
	return count, nil
}

// pillKeywordClause builds the OR-of-LIKE predicate (and its bind args) matching
// any keyword against album subjects or track tags. Returns "" when no keywords.
func pillKeywordClause(keywords string) (string, []any) {
	terms := splitKeywords(keywords)
	if len(terms) == 0 {
		return "", nil
	}
	clauses := make([]string, 0, len(terms))
	args := make([]any, 0, len(terms)*2)
	for _, t := range terms {
		like := "%" + t + "%"
		clauses = append(clauses, "(LOWER(a.subjects) LIKE ? OR LOWER(COALESCE(t.tags,'')) LIKE ?)")
		args = append(args, like, like)
	}
	return strings.Join(clauses, " OR "), args
}

func splitKeywords(keywords string) []string {
	var out []string
	for _, k := range strings.Split(keywords, ",") {
		k = strings.ToLower(strings.TrimSpace(k))
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
