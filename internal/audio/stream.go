package audio

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/ia"
	"golang.org/x/time/rate"
)

var debugLog *log.Logger

func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

func init() {
	root := findProjectRoot()
	logDir := filepath.Join(root, "data")
	os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(filepath.Join(logDir, "http_debug.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	debugLog = log.New(f, "", log.LstdFlags)
}

func logDebug(format string, args ...interface{}) {
	if debugLog != nil {
		debugLog.Printf(format, args...)
	}
}

func StreamAudioFromURL(ctx context.Context, client *http.Client, mp3URL string, maxBytes int, bwLimiter *rate.Limiter) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mp3URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "ParsoIAIndexer/1.0")

	logDebug("=== REQUEST %s ===", time.Now().Format(time.RFC3339))
	logDebug("  URL: %s", mp3URL)
	logDebug("  User-Agent: %s", req.Header.Get("User-Agent"))
	for k, v := range req.Header {
		if k != "User-Agent" && k != "Range" {
			logDebug("  %s: %v", k, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		logDebug("  ERROR: %v", err)
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	logDebug("  STATUS: %d %s", resp.StatusCode, resp.Status)
	for k, v := range resp.Header {
		logDebug("  RESP %s: %v", k, v)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		bodyPrefix, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		logDebug("  BODY (1KB): %s", string(bodyPrefix))
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, mp3URL)
	}
	logDebug("  OK — streaming %d bytes", maxBytes)

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
