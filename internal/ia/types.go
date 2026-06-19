package ia

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ScrapeItem struct {
	Identifier string `json:"identifier"`
	Downloads  int    `json:"downloads"`
}

type ScrapeResponse struct {
	Items  []ScrapeItem `json:"items"`
	Count  int          `json:"count"`
	Total  int          `json:"total"`
	Cursor string       `json:"cursor"`
}

const (
	DefaultSort  = "downloads desc"
	DefaultCount = 1000
)

type IAFullMetadataResponse struct {
	Metadata IAItemMetadata   `json:"metadata"`
	Files    []IAMetadataFile `json:"files"`
}

type IAItemMetadata struct {
	Identifier string          `json:"identifier"`
	Title      string          `json:"title"`
	Creator    json.RawMessage `json:"creator"`
	Collection json.RawMessage `json:"collection"`
}

func (m *IAItemMetadata) CreatorString() string {
	return rawMessageToString(m.Creator)
}

func (m *IAItemMetadata) CollectionString() string {
	return rawMessageToString(m.Collection)
}

func rawMessageToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return strings.Join(arr, ", ")
	}

	return ""
}

type IAMetadataFile struct {
	Name    string `json:"name"`
	Format  string `json:"format"`
	Title   string `json:"title"`
	Track   string `json:"track"`
	Bitrate string `json:"bitrate"`
	Length  string `json:"length"`
}

type AlbumMetadata struct {
	Identifier string
	Title      string
	Creator    string
	Collection string
	ArtURL     string
	Tracks     []TrackFile
}

type TrackFile struct {
	Filename    string
	Title       string
	TrackNumber int
	Format      string
	Bitrate     int
	Duration    float64
	DownloadURL string
}

const MaxTrackDurationSec = 32 * 60

var legacyBlacklist = map[string]bool{
	"64Kbps MP3":  true,
	"128Kbps MP3": true,
}

func IsAcceptableMP3(format string, bitrateStr string) bool {
	if legacyBlacklist[format] {
		return false
	}

	if format == "VBR MP3" {
		return true
	}

	if format == "MP3" || strings.Contains(format, "MP3") {
		bitrate := parseBitrate(bitrateStr)
		if bitrate > 0 {
			return bitrate >= 192
		}
		if format == "MP3" {
			return false
		}
		return true
	}

	return false
}

func parseBitrate(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
		return n
	}
	return 0
}

func parseDuration(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
		return f
	}
	return 0
}
