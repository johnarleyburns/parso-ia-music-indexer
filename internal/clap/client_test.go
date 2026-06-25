package clap

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
)

func TestMockClientEmbeddingDimensions(t *testing.T) {
	c := NewMockClient()
	vec, err := c.GetEmbedding(context.Background(), nil, 48000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 512 {
		t.Fatalf("expected 512 dimensions, got %d", len(vec))
	}
}

func TestMockClientEmbeddingValues(t *testing.T) {
	c := NewMockClient()
	vec, err := c.GetEmbedding(context.Background(), nil, 48000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, v := range vec {
		if math.Abs(float64(v-0.01)) > 1e-6 {
			t.Fatalf("position %d: expected 0.01, got %f", i, v)
		}
	}
}

func TestMockClientHealthCheck(t *testing.T) {
	c := NewMockClient()
	if err := c.HealthCheck(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockClientClose(t *testing.T) {
	c := NewMockClient()
	if err := c.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFloat32ToBytesEmpty(t *testing.T) {
	result := Float32ToBytes(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty bytes, got %d", len(result))
	}
}

func TestFloat32ToBytesLength(t *testing.T) {
	samples := make([]float64, 100)
	result := Float32ToBytes(samples)
	if len(result) != 400 {
		t.Fatalf("expected 400 bytes, got %d", len(result))
	}
}

func TestFloat32ToBytesRoundtrip(t *testing.T) {
	samples := []float64{0.0, 1.0, -1.0, 0.5, -0.5, 3.14}
	buf := Float32ToBytes(samples)

	for i, expected := range samples {
		bits := binary.LittleEndian.Uint32(buf[i*4:])
		got := math.Float32frombits(bits)
		if math.Abs(float64(got)-expected) > 1e-6 {
			t.Fatalf("position %d: expected %f, got %f", i, expected, got)
		}
	}
}

func TestFloat32ToBytesKnownEncoding(t *testing.T) {
	samples := []float64{1.0}
	buf := Float32ToBytes(samples)
	bits := binary.LittleEndian.Uint32(buf)
	if bits != 0x3F800000 {
		t.Fatalf("expected 0x3F800000 for 1.0, got 0x%08X", bits)
	}
}

func TestNewGRPCClientUnreachable(t *testing.T) {
	_, err := NewGRPCClient("127.0.0.1", 59999)
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
}

func TestMaxPCMBytesInvariants(t *testing.T) {
	if maxPCMBytes >= maxMsgSize {
		t.Fatalf("maxPCMBytes (%d) must stay below maxMsgSize (%d) to avoid 'message larger than max'", maxPCMBytes, maxMsgSize)
	}
	if maxPCMBytes%4 != 0 {
		t.Fatalf("maxPCMBytes (%d) must be a multiple of 4 so truncation never splits a float32 sample", maxPCMBytes)
	}
}

func TestMockClientTextEmbeddingDimensions(t *testing.T) {
	c := NewMockClient()
	vec, err := c.GetTextEmbedding(context.Background(), "melancholy piano")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 512 {
		t.Fatalf("expected 512 dimensions, got %d", len(vec))
	}
}

func TestMockClientTextEmbeddingNonZeroNorm(t *testing.T) {
	c := NewMockClient()
	vec, err := c.GetTextEmbedding(context.Background(), "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	norm := math.Sqrt(sum)
	if norm < 0.99 || norm > 1.01 {
		t.Fatalf("expected unit norm, got %f", norm)
	}
}

func TestMockClientTextEmbeddingDeterministic(t *testing.T) {
	c := NewMockClient()
	vec1, _ := c.GetTextEmbedding(context.Background(), "query a")
	vec2, _ := c.GetTextEmbedding(context.Background(), "query b")
	for i := range vec1 {
		if vec1[i] != vec2[i] {
			t.Fatalf("position %d: %f != %f", i, vec1[i], vec2[i])
		}
	}
}
