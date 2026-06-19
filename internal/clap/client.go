package clap

import (
	"context"
	"math"
)

type CLAPClient interface {
	GetEmbedding(ctx context.Context, pcmData []byte, sampleRate int32) ([]float32, error)
	GetTextEmbedding(ctx context.Context, text string) ([]float32, error)
	HealthCheck(ctx context.Context) error
	Close() error
}

type mockCLAPClient struct{}

func NewMockClient() CLAPClient {
	return &mockCLAPClient{}
}

func (m *mockCLAPClient) GetEmbedding(_ context.Context, _ []byte, _ int32) ([]float32, error) {
	vec := make([]float32, 512)
	for i := range vec {
		vec[i] = 0.01
	}
	return vec, nil
}

func (m *mockCLAPClient) GetTextEmbedding(_ context.Context, _ string) ([]float32, error) {
	val := float32(1.0 / math.Sqrt(512))
	vec := make([]float32, 512)
	for i := range vec {
		vec[i] = val
	}
	return vec, nil
}

func (m *mockCLAPClient) HealthCheck(_ context.Context) error {
	return nil
}

func (m *mockCLAPClient) Close() error {
	return nil
}
