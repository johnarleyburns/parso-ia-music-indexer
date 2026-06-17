package ia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

func ScrapePage(ctx context.Context, client *http.Client, cursor, query, sort string, count int) (*ScrapeResponse, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("sorts", sort)
	params.Set("count", fmt.Sprintf("%d", count))
	params.Set("fields", "identifier,downloads")
	if cursor != "" {
		params.Set("cursor", cursor)
	}

	u := &url.URL{
		Scheme:   "https",
		Host:     "archive.org",
		Path:     "/services/search/v1/scrape",
		RawQuery: params.Encode(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := DoWithRetry(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("scrape request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result ScrapeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &result, nil
}
