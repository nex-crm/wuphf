package team

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// BridgeBackoff produces exponential-with-jitter delays for reconnect loops.
// Suitable for any bridge (Telegram, OpenClaw, future).
type BridgeBackoff struct {
	base, cap time.Duration
	attempt   int
	mu        sync.Mutex
	rng       *rand.Rand
}

func NewBridgeBackoff(base, cap time.Duration) *BridgeBackoff {
	return &BridgeBackoff{base: base, cap: cap, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// Next returns the next delay; safe for concurrent callers.
func (b *BridgeBackoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	d := b.base << b.attempt
	if d <= 0 || d > b.cap {
		d = b.cap
	}
	// ±20% jitter
	jitter := b.rng.Float64()*0.4 - 0.2
	d = d + time.Duration(float64(d)*jitter)
	if d < b.base/2 {
		d = b.base / 2
	}
	b.attempt++
	return d
}

// Reset zeroes the attempt counter.
func (b *BridgeBackoff) Reset() {
	b.mu.Lock()
	b.attempt = 0
	b.mu.Unlock()
}

// Wait sleeps for the next delay, respecting ctx cancellation.
func (b *BridgeBackoff) Wait(ctx context.Context) error {
	d := b.Next()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CircuitBreaker trips open after N consecutive failures; stays open for pauseDur,
// then half-open (one allowed trial). RecordSuccess closes it and zeroes the counter.
type CircuitBreaker struct {
	threshold int
	pauseDur  time.Duration
	mu        sync.Mutex
	fails     int
	openedAt  time.Time
}

func NewCircuitBreaker(threshold int, pauseDur time.Duration) *CircuitBreaker {
	return &CircuitBreaker{threshold: threshold, pauseDur: pauseDur}
}

func (c *CircuitBreaker) RecordFailure() {
	c.mu.Lock()
	c.fails++
	if c.fails >= c.threshold && c.openedAt.IsZero() {
		c.openedAt = time.Now()
	}
	c.mu.Unlock()
}

// RecordSuccess resets the breaker fully. Per pre-impl decision 5: only call
// this after BOTH a successful Dial AND a successful hello-ok response.
func (c *CircuitBreaker) RecordSuccess() {
	c.mu.Lock()
	c.fails = 0
	c.openedAt = time.Time{}
	c.mu.Unlock()
}

// Open reports whether the breaker is currently blocking attempts.
func (c *CircuitBreaker) Open() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.openedAt.IsZero() {
		return false
	}
	if time.Since(c.openedAt) >= c.pauseDur {
		return false // half-open; caller may trial
	}
	return true
}
