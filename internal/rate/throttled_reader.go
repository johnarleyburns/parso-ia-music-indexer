package rate

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

type ThrottledReader struct {
	reader  io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

func NewThrottledReader(ctx context.Context, reader io.Reader, bytesPerSec int) *ThrottledReader {
	return &ThrottledReader{
		reader:  reader,
		limiter: rate.NewLimiter(rate.Limit(bytesPerSec), bytesPerSec),
		ctx:     ctx,
	}
}

func (tr *ThrottledReader) Read(p []byte) (int, error) {
	n, err := tr.reader.Read(p)
	if n > 0 {
		if waitErr := tr.limiter.WaitN(tr.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}
