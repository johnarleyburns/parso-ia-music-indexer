package rate

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestThrottledReader(t *testing.T) {
	data := strings.Repeat("abcdefghij", 100)
	reader := strings.NewReader(data)

	bps := 100
	throttled := NewThrottledReader(context.Background(), reader, bps)

	buf := make([]byte, 50)
	start := time.Now()
	totalRead := 0
	for totalRead < len(data) {
		n, err := throttled.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		totalRead += n
	}
	elapsed := time.Since(start)

	if totalRead != len(data) {
		t.Fatalf("expected %d bytes read, got %d", len(data), totalRead)
	}

	expectedMin := time.Duration(len(data)/bps) * time.Second
	if elapsed < expectedMin/2 {
		t.Errorf("throttling too fast: read %d bytes in %v, expected at least %v", totalRead, elapsed, expectedMin)
	}
}

func TestThrottledReaderContextCancel(t *testing.T) {
	data := strings.Repeat("a", 10000)
	reader := strings.NewReader(data)

	ctx, cancel := context.WithCancel(context.Background())
	throttled := NewThrottledReader(ctx, reader, 10)

	cancel()

	buf := make([]byte, 100)
	_, err := throttled.Read(buf)
	if err == nil {
		t.Error("expected error after context cancellation")
	}
}
