package cachegrid

import (
	"time"

	"github.com/shohag/cachegrid/internal/ratelimit"
)

// RateLimitOptions configures token bucket rate limiting.
type RateLimitOptions struct {
	Limit  int64
	Window time.Duration
}

// SlidingWindowOptions configures sliding window rate limiting.
type SlidingWindowOptions struct {
	Limit  int64
	Window time.Duration
}

// RateLimitState is the public rate limit response.
type RateLimitState struct {
	Allowed   bool
	Remaining int64
	Limit     int64
	ResetsAt  time.Time
}

func stateFromInternal(s ratelimit.State) RateLimitState {
	return RateLimitState{
		Allowed:   s.Allowed,
		Remaining: s.Remaining,
		Limit:     s.Limit,
		ResetsAt:  s.ResetsAt,
	}
}

// RateLimit checks a token bucket rate limiter for the given key.
func (c *Cache) RateLimit(key string, opts RateLimitOptions) (bool, RateLimitState) {
	s := c.tokenBucket.Allow(key, opts.Limit, opts.Window)
	state := stateFromInternal(s)
	return state.Allowed, state
}

// RateLimitSliding checks a sliding window rate limiter for the given key.
func (c *Cache) RateLimitSliding(key string, opts SlidingWindowOptions) (bool, RateLimitState) {
	s := c.slidingWindow.Allow(key, opts.Limit, opts.Window)
	state := stateFromInternal(s)
	return state.Allowed, state
}
