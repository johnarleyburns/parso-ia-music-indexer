package ia

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

		if isRetryableStatus(resp.StatusCode) {
			resp.Body.Close()
			wait := backoffs[attempt]
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if d := parseRetryAfter(ra); d > 0 {
					wait = d
				}
			}
			lastErr = fmt.Errorf("IA API error: HTTP %d", resp.StatusCode)
			if attempt < len(backoffs) {
				select {
				case <-time.After(wait):
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

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

func parseRetryAfter(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if secs, err := strconv.Atoi(s); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := time.Parse(time.RFC1123, s); err == nil {
		return time.Until(t)
	}
	if t, err := time.Parse(time.RFC1123Z, s); err == nil {
		return time.Until(t)
	}
	return 0
}
