package ia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func LookupAlbumMetadata(ctx context.Context, client *http.Client, identifier string) (*AlbumMetadata, error) {
	u := &url.URL{
		Scheme: "https",
		Host:   "archive.org",
		Path:   fmt.Sprintf("/metadata/%s", identifier),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build metadata request: %w", err)
	}

	resp, err := DoWithRetry(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("metadata request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var full IAFullMetadataResponse
	if err := json.Unmarshal(body, &full); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}

	album := &AlbumMetadata{
		Identifier: identifier,
		Title:      full.Metadata.Title,
		Creator:    full.Metadata.CreatorString(),
		Collection: full.Metadata.CollectionString(),
		ArtURL:     fmt.Sprintf("https://archive.org/services/img/%s", identifier),
	}

	if full.Metadata.AccessRestrictedItem {
		return album, nil
	}

	for _, f := range full.Files {
		if !IsAcceptableMP3(f.Format, f.Bitrate) {
			continue
		}

		duration := parseDuration(f.Length)
		if duration > MaxTrackDurationSec {
			continue
		}

		trackNum := parseTrackNumber(f.Track)
		title := f.Title
		if title == "" {
			title = DeriveTitle(f.Name)
		}
		bitrate := parseBitrate(f.Bitrate)

		album.Tracks = append(album.Tracks, TrackFile{
			Filename:    f.Name,
			Title:       title,
			TrackNumber: trackNum,
			Format:      f.Format,
			Bitrate:     bitrate,
			Duration:    duration,
			DownloadURL: fmt.Sprintf("https://archive.org/download/%s/%s", identifier, url.PathEscape(f.Name)),
		})
	}

	return album, nil
}

func LookupMP3URL(ctx context.Context, client *http.Client, identifier string) (string, error) {
	album, err := LookupAlbumMetadata(ctx, client, identifier)
	if err != nil {
		return "", err
	}
	if len(album.Tracks) == 0 {
		return "", fmt.Errorf("no acceptable MP3 found for %s", identifier)
	}
	return album.Tracks[0].DownloadURL, nil
}

func parseTrackNumber(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if idx := strings.Index(s, "/"); idx > 0 {
		s = s[:idx]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

var trackNumPrefixRe = regexp.MustCompile(`^(?:\d{1,3}[\s._\-]+(?:-\s*)?|[Tt]rack\s*\d+[\s._\-]+)`)

func DeriveTitle(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	name = trackNumPrefixRe.ReplaceAllString(name, "")
	name = strings.NewReplacer("_", " ", "-", " ").Replace(name)
	name = strings.Join(strings.Fields(name), " ")

	if name == "" {
		name = strings.TrimSuffix(filename, filepath.Ext(filename))
	}

	return titleCase(name)
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}
