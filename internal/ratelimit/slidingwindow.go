package ratelimit

import (
	"sync"
	"time"
)

type window struct {
	prevCount int64
	currCount int64
	limit     int64
	windowLen time.Duration
	currStart time.Time
}

// SlidingWindow implements a sliding window rate limiter.
type SlidingWindow struct {
	mu      sync.Mutex
	windows map[string]*window
}

// NewSlidingWindow creates a new sliding window rate limiter.
func NewSlidingWindow() *SlidingWindow {
	return &SlidingWindow{
		windows: make(map[string]*window),
	}
}

// Allow checks if a request is allowed under the sliding window rate limit.
func (sw *SlidingWindow) Allow(key string, limit int64, windowLen time.Duration) State {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	w, ok := sw.windows[key]
	if !ok {
		w = &window{
			limit:     limit,
			windowLen: windowLen,
			currStart: now,
		}
		sw.windows[key] = w
	}

	// Update parameters
	w.limit = limit
	w.windowLen = windowLen

	// Advance windows
	elapsed := now.Sub(w.currStart)
	if elapsed >= 2*windowLen {
		// Both windows have passed
		w.prevCount = 0
		w.currCount = 0
		w.currStart = now
	} else if elapsed >= windowLen {
		// Current window has ended, shift
		w.prevCount = w.currCount
		w.currCount = 0
		w.currStart = w.currStart.Add(windowLen)
	}

	// Calculate weighted count using sliding window
	windowProgress := now.Sub(w.currStart).Seconds() / windowLen.Seconds()
	if windowProgress > 1 {
		windowProgress = 1
	}
	prevWeight := 1.0 - windowProgress
	estimatedCount := float64(w.prevCount)*prevWeight + float64(w.currCount)

	resetsAt := w.currStart.Add(windowLen)

	if int64(estimatedCount) < limit {
		w.currCount++
		remaining := limit - int64(estimatedCount) - 1
		if remaining < 0 {
			remaining = 0
		}
		return State{
			Allowed:   true,
			Remaining: remaining,
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
func (sw *SlidingWindow) Reset(key string) {
	sw.mu.Lock()
	delete(sw.windows, key)
	sw.mu.Unlock()
}
