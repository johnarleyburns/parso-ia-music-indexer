package clap

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	pb "github.com/johnarleyburns/parso-ia-music-indexer/internal/clap/clap_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type grpcCLAPClient struct {
	conn   *grpc.ClientConn
	client pb.CLAPEmbedderClient
}

const maxMsgSize = 50 * 1024 * 1024

func NewGRPCClient(host string, port int) (CLAPClient, error) {
	target := fmt.Sprintf("%s:%d", host, port)
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(maxMsgSize),
			grpc.MaxCallRecvMsgSize(maxMsgSize),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("clap grpc dial: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := pb.NewCLAPEmbedderClient(conn)
	_, err = client.GetEmbedding(ctx, &pb.EmbeddingRequest{
		PcmData:    make([]byte, 4),
		SampleRate: 48000,
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("clap grpc health probe: %w", err)
	}

	return &grpcCLAPClient{conn: conn, client: client}, nil
}

func (c *grpcCLAPClient) GetEmbedding(ctx context.Context, pcmData []byte, sampleRate int32) ([]float32, error) {
	resp, err := c.client.GetEmbedding(ctx, &pb.EmbeddingRequest{
		PcmData:    pcmData,
		SampleRate: sampleRate,
	})
	if err != nil {
		return nil, fmt.Errorf("clap grpc: %w", err)
	}
	return resp.GetEmbedding(), nil
}

func (c *grpcCLAPClient) GetTextEmbedding(ctx context.Context, text string) ([]float32, error) {
	resp, err := c.client.GetTextEmbedding(ctx, &pb.TextEmbeddingRequest{
		Text: text,
	})
	if err != nil {
		return nil, fmt.Errorf("clap grpc text: %w", err)
	}
	return resp.GetEmbedding(), nil
}

func (c *grpcCLAPClient) HealthCheck(ctx context.Context) error {
	_, err := c.client.GetEmbedding(ctx, &pb.EmbeddingRequest{
		PcmData:    make([]byte, 4),
		SampleRate: 48000,
	})
	return err
}

func (c *grpcCLAPClient) Close() error {
	return c.conn.Close()
}

func Float32ToBytes(samples []float64) []byte {
	buf := make([]byte, len(samples)*4)
	for i, s := range samples {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(float32(s)))
	}
	return buf
}
