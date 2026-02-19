package cache

import (
	"container/list"
	"time"
)

// Entry represents a single cache item.
type Entry struct {
	Key       string
	Value     []byte
	ExpiresAt time.Time
	Size      int64
	Element   *list.Element
}

// IsExpired returns true if the entry has a TTL and it has passed.
func (e *Entry) IsExpired() bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.ExpiresAt)
}

// TTL returns the remaining time-to-live.
// Returns -1 if there is no expiry, 0 if expired.
func (e *Entry) TTL() time.Duration {
	if e.ExpiresAt.IsZero() {
		return -1
	}
	remaining := time.Until(e.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// EstimateSize returns an approximate memory footprint for a cache entry.
func EstimateSize(key string, value []byte) int64 {
	// key string header (16) + key bytes + value slice header (24) + value bytes
	// + entry struct overhead (~80 bytes) + list element (~64 bytes)
	return int64(len(key)) + int64(len(value)) + 184
}
