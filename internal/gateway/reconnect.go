package gateway

import (
	"math/rand/v2"
	"time"
)

type Backoff struct {
	Min     time.Duration
	Max     time.Duration
	Factor  float64
	current time.Duration
}

func NewBackoff(min, max time.Duration) *Backoff {
	return &Backoff{Min: min, Max: max, Factor: 2}
}

func (b *Backoff) Reset() {
	b.current = 0
}

func (b *Backoff) Next() time.Duration {
	if b.current == 0 {
		b.current = b.Min
	} else {
		next := time.Duration(float64(b.current) * b.Factor)
		if next > b.Max {
			next = b.Max
		}
		b.current = next
	}
	jitter := time.Duration(rand.Int64N(int64(b.current / 2)))
	return b.current + jitter
}
