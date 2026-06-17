package db

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
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

func SaveEmbedding(db *sql.DB, trackID int, embedding []float32, qualityScore float64) error {
	blob := encodeF32(embedding)
	_, err := db.Exec(
		`INSERT OR REPLACE INTO track_embeddings(track_id, embedding, quality_score) VALUES(?, ?, ?)`,
		trackID, blob, qualityScore,
	)
	return err
}

func QuerySimilar(db *sql.DB, trackID int, limit int) ([]SimilarTrack, error) {
	var queryBlob []byte
	err := db.QueryRow(`SELECT embedding FROM track_embeddings WHERE track_id = ?`, trackID).Scan(&queryBlob)
	if err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
	}
	queryVec := decodeF32(queryBlob)

	rows, err := db.Query(`SELECT e.track_id, COALESCE(t.title, t.filename), t.album_id, e.embedding, e.quality_score
		FROM track_embeddings e
		INNER JOIN tracks t ON e.track_id = t.id
		WHERE e.track_id != ?`, trackID)
	if err != nil {
		return nil, fmt.Errorf("select all: %w", err)
	}
	defer rows.Close()

	var candidates []candidate

	for rows.Next() {
		var c candidate
		var blob []byte
		if err := rows.Scan(&c.trackID, &c.title, &c.albumID, &blob, &c.quality); err != nil {
			return nil, err
		}
		vec := decodeF32(blob)
		if len(vec) != len(queryVec) {
			continue
		}
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

func GetEmbedding(db *sql.DB, trackID int) ([]float32, float64, error) {
	var blob []byte
	var qs float64
	err := db.QueryRow(`SELECT embedding, quality_score FROM track_embeddings WHERE track_id = ?`, trackID).Scan(&blob, &qs)
	if err != nil {
		return nil, 0, err
	}
	return decodeF32(blob), qs, nil
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
