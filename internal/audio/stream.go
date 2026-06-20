package audio

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/ia"
	"golang.org/x/time/rate"
)

func StreamAudioFromURL(ctx context.Context, client *http.Client, mp3URL string, maxBytes int, bwLimiter *rate.Limiter) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mp3URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", maxBytes-1))
	req.Header.Set("User-Agent", "ParsoIAIndexer/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, mp3URL)
	}

	var reader io.Reader = io.LimitReader(resp.Body, int64(maxBytes))
	if bwLimiter != nil {
		reader = &throttledReader{reader: reader, limiter: bwLimiter, ctx: ctx}
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty response for %s", mp3URL)
	}

	return data, nil
}

func StreamAudio(ctx context.Context, client *http.Client, identifier string, maxBytes int) ([]byte, error) {
	mp3URL, err := ia.LookupMP3URL(ctx, client, identifier)
	if err != nil {
		return nil, fmt.Errorf("lookup mp3 url for %s: %w", identifier, err)
	}
	return StreamAudioFromURL(ctx, client, mp3URL, maxBytes, nil)
}

type throttledReader struct {
	reader  io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

func (tr *throttledReader) Read(p []byte) (int, error) {
	n, err := tr.reader.Read(p)
	if n > 0 {
		if waitErr := tr.limiter.WaitN(tr.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}
