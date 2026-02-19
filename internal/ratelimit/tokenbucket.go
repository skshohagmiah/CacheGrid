package ratelimit

import (
	"sync"
	"time"
)

// State represents the current state of a rate limiter.
type State struct {
	Allowed   bool
	Remaining int64
	Limit     int64
	ResetsAt  time.Time
}

type bucket struct {
	tokens   float64
	limit    int64
	lastFill time.Time
	window   time.Duration
}

// TokenBucket implements a token bucket rate limiter.
type TokenBucket struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

// NewTokenBucket creates a new token bucket rate limiter.
func NewTokenBucket() *TokenBucket {
	return &TokenBucket{
		buckets: make(map[string]*bucket),
	}
}

// Allow checks if a request is allowed under the rate limit.
func (tb *TokenBucket) Allow(key string, limit int64, window time.Duration) State {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	b, ok := tb.buckets[key]
	if !ok {
		b = &bucket{
			tokens:   float64(limit),
			limit:    limit,
			lastFill: now,
			window:   window,
		}
		tb.buckets[key] = b
	}

	// Update limit/window if changed
	b.limit = limit
	b.window = window

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastFill)
	refillRate := float64(limit) / window.Seconds()
	b.tokens += elapsed.Seconds() * refillRate
	if b.tokens > float64(limit) {
		b.tokens = float64(limit)
	}
	b.lastFill = now

	resetsAt := now.Add(window)

	if b.tokens >= 1 {
		b.tokens--
		return State{
			Allowed:   true,
			Remaining: int64(b.tokens),
			Limit:     limit,
			ResetsAt:  resetsAt,
		}
	}

	return State{
		Allowed:   false,
		Remaining: 0,
		Limit:     limit,
		ResetsAt:  resetsAt,
	}
}

// Reset removes rate limit state for a key.
func (tb *TokenBucket) Reset(key string) {
	tb.mu.Lock()
	delete(tb.buckets, key)
	tb.mu.Unlock()
}
