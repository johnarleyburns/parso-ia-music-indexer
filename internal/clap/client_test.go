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
