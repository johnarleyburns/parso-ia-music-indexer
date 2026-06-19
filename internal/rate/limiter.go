package rate

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

type Limiter struct {
	lim *rate.Limiter
}

func NewLimiter(reqPerMin int) *Limiter {
	return NewBurstLimiter(reqPerMin, 1)
}

func NewBurstLimiter(reqPerMin int, burst int) *Limiter {
	interval := time.Minute / time.Duration(reqPerMin)
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		lim: rate.NewLimiter(rate.Every(interval), burst),
	}
}

func (l *Limiter) Wait(ctx context.Context) error {
	return l.lim.Wait(ctx)
}
