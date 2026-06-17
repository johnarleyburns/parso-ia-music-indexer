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
	interval := time.Minute / time.Duration(reqPerMin)
	return &Limiter{
		lim: rate.NewLimiter(rate.Every(interval), 1),
	}
}

func (l *Limiter) Wait(ctx context.Context) error {
	return l.lim.Wait(ctx)
}
