package audio

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

	logDebug("=== REQUEST %s ===", time.Now().Format(time.RFC3339))
	logDebug("  URL: %s", mp3URL)

	resp, err := ia.DoWithRetry(ctx, client, req)
	if err != nil {
		logDebug("  ERROR: %v", err)
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	logDebug("  STATUS: %d %s", resp.StatusCode, resp.Status)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
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

func getFileSize(ctx context.Context, client *http.Client, mp3URL string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, mp3URL, nil)
	if err != nil {
		return 0, fmt.Errorf("build HEAD request: %w", err)
	}

	resp, err := ia.DoWithRetry(ctx, client, req)
	if err != nil {
		return 0, fmt.Errorf("HEAD request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HEAD returned status %d", resp.StatusCode)
	}

	cl := resp.Header.Get("Content-Length")
	if cl == "" {
		return 0, fmt.Errorf("no Content-Length in HEAD response")
	}

	size, err := strconv.ParseInt(cl, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid Content-Length %q: %w", cl, err)
	}

	return size, nil
}

func computeMidpointStartByte(fileSize int64, maxBytes int, skipSeconds int) int64 {
	bytesPerSec := float64(maxBytes) / 30.0
	floorSkip := int64(float64(skipSeconds) * bytesPerSec)
	ratioStart := int64(float64(fileSize) * 0.35)

	start := ratioStart
	if floorSkip > start {
		start = floorSkip
	}

	if start+int64(maxBytes) > fileSize {
		start = fileSize - int64(maxBytes)
		if start < 0 {
			start = 0
		}
	}

	return start
}

func StreamAudioFromURLWithRange(ctx context.Context, client *http.Client, mp3URL string, startByte, maxBytes int64, bwLimiter *rate.Limiter) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mp3URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	rangeHeader := fmt.Sprintf("bytes=%d-%d", startByte, startByte+maxBytes-1)
	req.Header.Set("Range", rangeHeader)

	logDebug("=== RANGE REQUEST %s ===", time.Now().Format(time.RFC3339))
	logDebug("  URL: %s", mp3URL)
	logDebug("  Range: %s", rangeHeader)

	resp, err := ia.DoWithRetry(ctx, client, req)
	if err != nil {
		logDebug("  ERROR: %v", err)
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	logDebug("  STATUS: %d %s", resp.StatusCode, resp.Status)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, mp3URL)
	}
	logDebug("  OK — streaming %d bytes from offset %d", maxBytes, startByte)

	var reader io.Reader = io.LimitReader(resp.Body, maxBytes)
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

func StreamAudioMidpoint(ctx context.Context, client *http.Client, mp3URL string, maxBytes int, skipSeconds int, bwLimiter *rate.Limiter) ([]byte, error) {
	fileSize, err := getFileSize(ctx, client, mp3URL)
	if err != nil {
		logDebug("  HEAD request failed (%v), falling back to head streaming", err)
		return StreamAudioFromURL(ctx, client, mp3URL, maxBytes, bwLimiter)
	}

	windowSize := int64(maxBytes)
	if fileSize <= windowSize {
		logDebug("  file too small (%d <= %d), falling back to head streaming", fileSize, windowSize)
		return StreamAudioFromURL(ctx, client, mp3URL, maxBytes, bwLimiter)
	}

	startByte := computeMidpointStartByte(fileSize, maxBytes, skipSeconds)
	logDebug("  midpoint: fileSize=%d maxBytes=%d skipSeconds=%d startByte=%d", fileSize, maxBytes, skipSeconds, startByte)

	return StreamAudioFromURLWithRange(ctx, client, mp3URL, startByte, windowSize, bwLimiter)
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
