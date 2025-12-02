package connection

import (
	"sync"
	"time"
)

// backoff implements exponential backoff with jitter.
type backoff struct {
	initial    time.Duration
	max        time.Duration
	multiplier float64
	current    time.Duration
	mu         sync.Mutex
}

// newBackoff creates a new backoff calculator.
func newBackoff(initial, max time.Duration, multiplier float64) *backoff {
	return &backoff{
		initial:    initial,
		max:        max,
		multiplier: multiplier,
		current:    initial,
	}
}

// Next returns the next backoff duration and advances the state.
func (b *backoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	d := b.current
	b.current = time.Duration(float64(b.current) * b.multiplier)
	if b.current > b.max {
		b.current = b.max
	}
	return d
}

// Reset resets the backoff to initial state.
func (b *backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = b.initial
}
