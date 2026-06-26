package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/listenability"
)

type ListenabilityCleanupClaim struct {
	TrackID   int
	AlbumID   string
	Title     string
	Filename  string
	Duration  float64
	Bitrate   int
	Tags      string
	QualityScore sql.NullFloat64

	AlbumTitle    string
	AlbumCreator  string
	AlbumSubjects string
	AlbumGenres   string
	AlbumListenabilityScore sql.NullFloat64

	HasEmbedding bool
	ClapBlob     []byte
}

type ListenabilityCoverage struct {
	TotalCompletedTracks          int
	TracksWithCurrentVersion      int
	TracksMissingVersion          int
	TracksWithStaleVersion        int
	CountByTier                   map[string]int
	CountByDecision               map[string]int
	WouldMarkUnavailable          int
	CompletedTracksUnder60        int
}

func UpdateTrackListenability(db *sql.DB, trackID int, r listenability.Result, workerID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	reasonsJSON, _ := json.Marshal(r.Reasons)
	componentsJSON, _ := json.Marshal(r.Components)
	_, err := db.Exec(
		`UPDATE tracks SET listenability_score=?, listenability_tier=?, listenability_decision=?,
		 listenability_stream=?, listenability_reasons=?, listenability_components=?,
		 listenability_version=?, listenability_checked_at=?, listenability_worker_id=?,
		 listenability_locked_at=NULL WHERE id=?`,
		r.Score, r.Tier, r.Decision, r.Stream, string(reasonsJSON), string(componentsJSON),
		r.Version, now, workerID, trackID,
	)
	return err
}

func UpdateAlbumListenability(db *sql.DB, albumID string, r listenability.Result) error {
	now := time.Now().UTC().Format(time.RFC3339)
	reasonsJSON, _ := json.Marshal(r.Reasons)
	componentsJSON, _ := json.Marshal(r.Components)
	_, err := db.Exec(
		`UPDATE albums SET listenability_score=?, listenability_tier=?, listenability_decision=?,
		 listenability_stream=?, listenability_reasons=?, listenability_components=?,
		 listenability_version=?, listenability_checked_at=?
		 WHERE ia_identifier=?`,
		r.Score, r.Tier, r.Decision, r.Stream, string(reasonsJSON), string(componentsJSON),
		r.Version, now, albumID,
	)
	return err
}

func GetAlbumListenabilityEvidence(db *sql.DB, albumID string) (listenability.AlbumEvidence, error) {
	e := listenability.AlbumEvidence{AlbumID: albumID}
	err := db.QueryRow(
		`SELECT COALESCE(title,''), COALESCE(creator,''), COALESCE(subjects,''), COALESCE(genres,''), track_count
		 FROM albums WHERE ia_identifier=?`, albumID,
	).Scan(&e.Title, &e.Creator, &e.Subjects, &e.Genres, &e.TrackCount)
	if err != nil {
		return e, err
	}

	rows, err := db.Query(
		`SELECT duration FROM tracks WHERE album_id=? AND duration > 0 ORDER BY duration`, albumID,
	)
	if err != nil {
		return e, err
	}
	defer rows.Close()

	var durations []float64
	for rows.Next() {
		var d float64
		if err := rows.Scan(&d); err != nil {
			return e, err
		}
		durations = append(durations, d)
	}
	if err := rows.Err(); err != nil {
		return e, err
	}

	e.PositiveDurationCnt = len(durations)
	if len(durations) == 0 {
		return e, nil
	}

	var total float64
	for _, d := range durations {
		total += d
	}
	e.AvgDurationSec = total / float64(len(durations))
	e.TotalDurationSec = total

	if len(durations) > 0 {
		mid := len(durations) / 2
		if len(durations)%2 == 0 {
			e.MedianDurationSec = (durations[mid-1] + durations[mid]) / 2.0
		} else {
			e.MedianDurationSec = durations[mid]
		}
	}

	var under30, under60, under90 int
	for _, d := range durations {
		if d < 30 {
			under30++
		}
		if d < 60 {
			under60++
		}
		if d < 90 {
			under90++
		}
	}
	e.Short30Ratio = float64(under30) / float64(len(durations))
	e.Short60Ratio = float64(under60) / float64(len(durations))
	e.Short90Ratio = float64(under90) / float64(len(durations))

	return e, nil
}

func ClaimListenabilityCleanupBatch(db *sql.DB, workerID string, version string, batchSize int) ([]ListenabilityCleanupClaim, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT t.id, t.album_id, COALESCE(t.title,''), t.filename,
		       COALESCE(t.duration,0), COALESCE(t.bitrate,0), COALESCE(t.tags,''),
		       COALESCE(a.title,''), COALESCE(a.creator,''),
		       COALESCE(a.subjects,''), COALESCE(a.genres,''),
		       COALESCE(a.listenability_score, -1),
		       (e.clap IS NOT NULL)
		FROM tracks t
		INNER JOIN albums a ON t.album_id = a.ia_identifier
		LEFT JOIN track_embeddings e ON t.id = e.track_id
		WHERE t.status = 'completed'
		  AND (t.listenability_version IS NULL OR t.listenability_version != ?)
		  AND (t.listenability_locked_at IS NULL)
		ORDER BY t.id ASC
		LIMIT ?`,
		version, batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("query cleanup batch: %w", err)
	}
	defer rows.Close()

	var claims []ListenabilityCleanupClaim
	for rows.Next() {
		var c ListenabilityCleanupClaim
		var hasEmbedding bool
		var albumScore float64
		if err := rows.Scan(&c.TrackID, &c.AlbumID, &c.Title, &c.Filename,
			&c.Duration, &c.Bitrate, &c.Tags,
			&c.AlbumTitle, &c.AlbumCreator,
			&c.AlbumSubjects, &c.AlbumGenres,
			&albumScore, &hasEmbedding); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		c.HasEmbedding = hasEmbedding
		if albumScore >= 0 {
			c.AlbumListenabilityScore = sql.NullFloat64{Float64: albumScore, Valid: true}
		}
		claims = append(claims, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(claims) == 0 {
		return nil, nil
	}

	for _, c := range claims {
		if _, err := tx.Exec(
			`UPDATE tracks SET listenability_locked_at=?, listenability_worker_id=? WHERE id=?`,
			now, workerID, c.TrackID,
		); err != nil {
			return nil, fmt.Errorf("lock track %d: %w", c.TrackID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return claims, nil
}

func ReleaseListenabilityCleanupClaim(db *sql.DB, trackID int) error {
	_, err := db.Exec(`UPDATE tracks SET listenability_locked_at=NULL, listenability_worker_id=NULL WHERE id=?`, trackID)
	return err
}

func GetTrackEmbeddingForCleanup(db *sql.DB, trackID int) ([]byte, float64, error) {
	var clapBlob []byte
	var qualityScore float64
	err := db.QueryRow(
		`SELECT clap, COALESCE(quality_score, 0) FROM track_embeddings WHERE track_id=?`, trackID,
	).Scan(&clapBlob, &qualityScore)
	if err != nil {
		return nil, 0, err
	}
	return clapBlob, qualityScore, nil
}

func GetListenabilityCoverage(db *sql.DB, version string) (*ListenabilityCoverage, error) {
	c := &ListenabilityCoverage{
		CountByTier:    make(map[string]int),
		CountByDecision: make(map[string]int),
	}

	db.QueryRow(`SELECT count(*) FROM tracks WHERE status='completed'`).Scan(&c.TotalCompletedTracks)
	db.QueryRow(`SELECT count(*) FROM tracks WHERE status='completed' AND listenability_version=?`, version).Scan(&c.TracksWithCurrentVersion)
	db.QueryRow(`SELECT count(*) FROM tracks WHERE status='completed' AND listenability_version IS NULL`).Scan(&c.TracksMissingVersion)
	db.QueryRow(`SELECT count(*) FROM tracks WHERE status='completed' AND listenability_version IS NOT NULL AND listenability_version != ?`, version).Scan(&c.TracksWithStaleVersion)
	db.QueryRow(`SELECT count(*) FROM tracks WHERE status='completed' AND listenability_decision='exclude'`).Scan(&c.WouldMarkUnavailable)
	db.QueryRow(`SELECT count(*) FROM tracks WHERE status='completed' AND duration > 0 AND duration < 60`).Scan(&c.CompletedTracksUnder60)

	rows, err := db.Query(`SELECT COALESCE(listenability_tier,''), count(*) FROM tracks WHERE status='completed' AND listenability_tier IS NOT NULL GROUP BY listenability_tier`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var tier string
			var cnt int
			if rows.Scan(&tier, &cnt) == nil {
				c.CountByTier[tier] = cnt
			}
		}
	}

	rows2, err := db.Query(`SELECT COALESCE(listenability_decision,''), count(*) FROM tracks WHERE status='completed' AND listenability_decision IS NOT NULL GROUP BY listenability_decision`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var dec string
			var cnt int
			if rows2.Scan(&dec, &cnt) == nil {
				c.CountByDecision[dec] = cnt
			}
		}
	}

	return c, nil
}

func GetCollectionListenabilityStats(db *sql.DB) (map[string]float64, error) {
	rows, err := db.Query(`
		SELECT t.album_id, AVG(t.listenability_score)
		FROM tracks t
		WHERE t.status='completed' AND t.listenability_score IS NOT NULL
		GROUP BY t.album_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]float64)
	for rows.Next() {
		var aid string
		var avg float64
		if err := rows.Scan(&aid, &avg); err != nil {
			return nil, err
		}
		result[aid] = avg
	}
	return result, rows.Err()
}

func GetTrackClaimWithListenability(db *sql.DB, trackID int) (TrackDetail, listenability.Result, error) {
	var t TrackDetail
	err := db.QueryRow(`SELECT t.id, t.filename, COALESCE(t.title, t.filename), COALESCE(t.track_number, 0),
			COALESCE(t.format, ''), t.download_url, t.status,
			COALESCE(e.quality_score, 0.0)
		FROM tracks t
		LEFT JOIN track_embeddings e ON t.id = e.track_id
		WHERE t.id = ?`, trackID).
		Scan(&t.ID, &t.Filename, &t.Title, &t.TrackNumber, &t.Format, &t.DownloadURL, &t.Status, &t.QualityScore)
	if err != nil {
		return t, listenability.Result{}, err
	}

	var score sql.NullFloat64
	var tier, decision, stream, reasonsJSON, componentsJSON, version sql.NullString
	err = db.QueryRow(`SELECT listenability_score, listenability_tier, listenability_decision,
			listenability_stream, listenability_reasons, listenability_components, listenability_version
		FROM tracks WHERE id = ?`, trackID).
		Scan(&score, &tier, &decision, &stream, &reasonsJSON, &componentsJSON, &version)
	if err != nil {
		return t, listenability.Result{}, nil
	}

	r := listenability.Result{}
	if score.Valid {
		r.Score = score.Float64
	}
	if tier.Valid {
		r.Tier = tier.String
	}
	if decision.Valid {
		r.Decision = decision.String
	}
	if stream.Valid {
		r.Stream = stream.String
	}
	if version.Valid {
		r.Version = version.String
	}
	if reasonsJSON.Valid {
		json.Unmarshal([]byte(reasonsJSON.String), &r.Reasons)
	}
	if componentsJSON.Valid {
		json.Unmarshal([]byte(componentsJSON.String), &r.Components)
	}
	return t, r, nil
}
