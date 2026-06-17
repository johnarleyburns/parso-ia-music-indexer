package ia

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const userAgent = "ParsoIAIndexer/1.0"

func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
	}
}

func DoWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}

	var lastErr error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		req := req.Clone(ctx)
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < len(backoffs) {
				select {
				case <-time.After(backoffs[attempt]):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			resp.Body.Close()
			if attempt < len(backoffs) {
				select {
				case <-time.After(backoffs[attempt]):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			continue
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, fmt.Errorf("IA API error: HTTP %d: %s", resp.StatusCode, string(body))
		}

		return resp, nil
	}

	return nil, fmt.Errorf("exhausted retries: %w", lastErr)
}
