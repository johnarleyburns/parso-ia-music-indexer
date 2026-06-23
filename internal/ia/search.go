package ia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type SearchResult struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Creator    string `json:"creator"`
	Downloads  int    `json:"downloads"`
	MediaType  string `json:"mediatype"`
}

type SearchResponse struct {
	Response struct {
		NumFound int            `json:"numFound"`
		Docs     []SearchResult `json:"docs"`
	} `json:"response"`
}

const (
	MaxSearchRows = 1000
)

func AdvancedSearch(ctx context.Context, client *http.Client, query string, limit int) ([]SearchResult, int, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0, fmt.Errorf("empty search query")
	}

	fl := []string{"identifier", "title", "creator", "downloads", "mediatype"}

	q := url.Values{}
	q.Set("q", query)
	q.Set("fl", strings.Join(fl, ","))
	q.Set("output", "json")
	q.Set("sort[]", "downloads desc")

	rows := limit
	if rows <= 0 || rows > MaxSearchRows {
		rows = MaxSearchRows
	}
	q.Set("rows", fmt.Sprintf("%d", rows))

	apiURL := "https://archive.org/advancedsearch.php?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("search request: %w", err)
	}

	resp, err := DoWithRetry(ctx, client, req)
	if err != nil {
		return nil, 0, fmt.Errorf("search: %w", err)
	}
	defer resp.Body.Close()

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("decode search: %w", err)
	}

	return result.Response.Docs, result.Response.NumFound, nil
}
