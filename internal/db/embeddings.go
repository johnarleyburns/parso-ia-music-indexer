package db

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/hybrid"
	"github.com/x448/float16"
)

type SimilarTrack struct {
	TrackID      int
	Title        string
	AlbumID      string
	QualityScore float64
	Distance     float64
}

type candidate struct {
	trackID int
	title   string
	albumID string
	quality float64
	dist    float64
}

func SaveEmbedding(db *sql.DB, trackID int, clap, mfcc, chroma []float32, qualityScore float64) error {
	return SaveEmbeddingWithStrategy(db, trackID, clap, mfcc, chroma, qualityScore, "head")
}

func SaveEmbeddingWithStrategy(db *sql.DB, trackID int, clap, mfcc, chroma []float32, qualityScore float64, strategy string) error {
	clapBlob := encodeF16(l2Normalize(clap))
	mfccBlob := encodeF16(l2Normalize(mfcc))
	chromaBlob := encodeF16(l2Normalize(chroma))
	_, err := db.Exec(
		`INSERT OR REPLACE INTO track_embeddings(track_id, clap, mfcc, chroma, quality_score, sample_strategy) VALUES(?, ?, ?, ?, ?, ?)`,
		trackID, clapBlob, mfccBlob, chromaBlob, qualityScore, strategy,
	)
	return err
}

func QuerySimilar(db *sql.DB, trackID int, limit int) ([]SimilarTrack, error) {
	var qClap, qMfcc, qChroma []byte
	err := db.QueryRow(`SELECT clap, mfcc, chroma FROM track_embeddings WHERE track_id = ?`, trackID).
		Scan(&qClap, &qMfcc, &qChroma)
	if err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
	}
	queryVec := hybrid.FuseFeatures(decodeF16(qClap), decodeF16(qMfcc), decodeF16(qChroma))

	rows, err := db.Query(`SELECT e.track_id, COALESCE(t.title, t.filename), t.album_id, e.clap, e.mfcc, e.chroma, e.quality_score
		FROM track_embeddings e
		INNER JOIN tracks t ON e.track_id = t.id
		WHERE e.track_id != ?
		  AND t.status = 'completed'
		  AND (t.listenability_decision IS NULL OR t.listenability_decision != 'exclude')
		  AND (t.listenability_stream IS NULL OR t.listenability_stream != 'excluded')
		  AND (t.listenability_stream IS NULL OR t.listenability_stream != 'longform_candidate')`, trackID)
	if err != nil {
		return nil, fmt.Errorf("select all: %w", err)
	}
	defer rows.Close()

	var candidates []candidate

	for rows.Next() {
		var c candidate
		var cBlob, mBlob, chBlob []byte
		if err := rows.Scan(&c.trackID, &c.title, &c.albumID, &cBlob, &mBlob, &chBlob, &c.quality); err != nil {
			return nil, err
		}
		vec := hybrid.FuseFeatures(decodeF16(cBlob), decodeF16(mBlob), decodeF16(chBlob))
		c.dist = cosineDistance(queryVec, vec)
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sortByDist(candidates)

	if limit > len(candidates) {
		limit = len(candidates)
	}

	result := make([]SimilarTrack, limit)
	for i := 0; i < limit; i++ {
		result[i] = SimilarTrack{
			TrackID:      candidates[i].trackID,
			Title:        candidates[i].title,
			AlbumID:      candidates[i].albumID,
			QualityScore: candidates[i].quality,
			Distance:     candidates[i].dist,
		}
	}
	return result, nil
}

func GetEmbeddingCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT count(*) FROM track_embeddings`).Scan(&count)
	return count, err
}

func GetEmbeddingStrategyCounts(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query(`SELECT sample_strategy, count(*) FROM track_embeddings GROUP BY sample_strategy`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var strategy string
		var count int
		if err := rows.Scan(&strategy, &count); err != nil {
			return nil, err
		}
		counts[strategy] = count
	}
	return counts, rows.Err()
}

func ResetTracksWithStrategy(db *sql.DB, targetStrategy string, newStatus string) (int64, error) {
	result, err := db.Exec(
		`UPDATE tracks SET status=?, error_message=NULL, retry_count=0
		 WHERE id IN (
			SELECT e.track_id FROM track_embeddings e
			WHERE e.sample_strategy != ?
		 ) AND status = 'completed'`,
		newStatus, targetStrategy,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type LexicalCoverage struct {
	TotalCompletedTracks  int
	TracksWithEmptyTags   int
	TracksWithTags        int
	AlbumsMissingSubjects int
	AlbumsMissingGenres   int
	AlbumsWithMetadata    int
	TotalCompletedAlbums  int
}

func GetLexicalCoverage(db *sql.DB) (*LexicalCoverage, error) {
	c := &LexicalCoverage{}

	db.QueryRow(`SELECT count(*) FROM tracks WHERE status = 'completed'`).Scan(&c.TotalCompletedTracks)
	db.QueryRow(`SELECT count(*) FROM tracks WHERE status = 'completed' AND (tags IS NULL OR tags = '')`).Scan(&c.TracksWithEmptyTags)
	db.QueryRow(`SELECT count(DISTINCT t.album_id) FROM tracks t JOIN albums a ON t.album_id = a.ia_identifier WHERE t.status = 'completed'`).Scan(&c.TotalCompletedAlbums)
	db.QueryRow(`SELECT count(DISTINCT a.ia_identifier) FROM albums a JOIN tracks t ON t.album_id = a.ia_identifier WHERE t.status = 'completed' AND (a.subjects IS NULL OR a.subjects = '')`).Scan(&c.AlbumsMissingSubjects)
	db.QueryRow(`SELECT count(DISTINCT a.ia_identifier) FROM albums a JOIN tracks t ON t.album_id = a.ia_identifier WHERE t.status = 'completed' AND (a.genres IS NULL OR a.genres = '')`).Scan(&c.AlbumsMissingGenres)

	c.TracksWithTags = c.TotalCompletedTracks - c.TracksWithEmptyTags
	c.AlbumsWithMetadata = c.TotalCompletedAlbums
	if c.AlbumsMissingSubjects > 0 || c.AlbumsMissingGenres > 0 {
		c.AlbumsWithMetadata = c.TotalCompletedAlbums - max(c.AlbumsMissingSubjects, c.AlbumsMissingGenres)
	}

	return c, nil
}

func GetEmbedding(db *sql.DB, trackID int) (clap, mfcc, chroma []float32, quality float64, err error) {
	var clapBlob, mfccBlob, chromaBlob []byte
	err = db.QueryRow(`SELECT clap, mfcc, chroma, quality_score FROM track_embeddings WHERE track_id = ?`, trackID).
		Scan(&clapBlob, &mfccBlob, &chromaBlob, &quality)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	return decodeF16(clapBlob), decodeF16(mfccBlob), decodeF16(chromaBlob), quality, nil
}

type TextSearchResult struct {
	TrackID        int
	Title          string
	AlbumID        string
	AlbumTitle     string
	AlbumCreator   string
	TrackTags      string
	QualityScore   float64
	CLAPSimilarity float64
	PillScore      float64
	Similarity     float64
}

func SearchByText(db *sql.DB, queryVec []float32, queryText string, limit int) ([]TextSearchResult, error) {
	qv := l2Normalize(queryVec)

	rows, err := db.Query(`SELECT e.track_id, COALESCE(t.title, t.filename), t.album_id,
			COALESCE(a.title, a.ia_identifier), COALESCE(a.creator, ''),
			COALESCE(t.tags, ''), e.clap, e.quality_score
		FROM track_embeddings e
		INNER JOIN tracks t ON e.track_id = t.id
		INNER JOIN albums a ON t.album_id = a.ia_identifier
		WHERE t.status = 'completed'
		  AND (t.listenability_decision IS NULL OR t.listenability_decision != 'exclude')
		  AND (t.listenability_stream IS NULL OR t.listenability_stream != 'excluded')
		  AND (t.listenability_stream IS NULL OR t.listenability_stream != 'longform_candidate')`)
	if err != nil {
		return nil, fmt.Errorf("search by text: %w", err)
	}
	defer rows.Close()

	var results []TextSearchResult
	for rows.Next() {
		var r TextSearchResult
		var clapBlob []byte
		if err := rows.Scan(&r.TrackID, &r.Title, &r.AlbumID, &r.AlbumTitle, &r.AlbumCreator, &r.TrackTags, &clapBlob, &r.QualityScore); err != nil {
			return nil, err
		}
		clapVec := decodeF16(clapBlob)
		r.CLAPSimilarity = dotProduct(qv, clapVec)
		r.PillScore = ComputePillScore(queryText, r.Title, r.TrackTags, r.AlbumTitle, r.AlbumCreator)
		r.Similarity = 0.50*r.CLAPSimilarity + 0.50*r.PillScore
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}
	return results, nil
}

func dotProduct(a, b []float32) float64 {
	var sum float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

func encodeF32(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeF32(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	norm := math.Sqrt(sum)
	out := make([]float32, len(v))
	if norm == 0 {
		return out
	}
	for i, f := range v {
		out[i] = float32(float64(f) / norm)
	}
	return out
}

func encodeF16(v []float32) []byte {
	buf := make([]byte, len(v)*2)
	for i, f := range v {
		h := float16.Fromfloat32(f)
		binary.LittleEndian.PutUint16(buf[i*2:], h.Bits())
	}
	return buf
}

func decodeF16(b []byte) []float32 {
	v := make([]float32, len(b)/2)
	for i := range v {
		bits := binary.LittleEndian.Uint16(b[i*2:])
		v[i] = float16.Frombits(bits).Float32()
	}
	return v
}

func cosineDistance(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	cosine := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if cosine > 1 {
		cosine = 1
	}
	if cosine < -1 {
		cosine = -1
	}
	return 1.0 - cosine
}

func sortByDist(c []candidate) {
	for i := 0; i < len(c); i++ {
		for j := i + 1; j < len(c); j++ {
			if c[j].dist < c[i].dist {
				c[i], c[j] = c[j], c[i]
			}
		}
	}
}
