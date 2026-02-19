package cachegrid

import (
	"github.com/shohag/cachegrid/internal/pubsub"
)

// EventType re-exports the internal event type.
type EventType = pubsub.EventType

// Event type constants.
const (
	EventSet    = pubsub.EventSet
	EventDelete = pubsub.EventDelete
	EventExpire = pubsub.EventExpire
	EventEvict  = pubsub.EventEvict
)

// CacheEvent is a public cache event.
type CacheEvent = pubsub.Event

// Subscription wraps an internal subscription.
type Subscription struct {
	internal *pubsub.Subscription
}

// Events returns the channel to receive events on.
func (s *Subscription) Events() <-chan CacheEvent {
	return s.internal.Events()
}

// Close unsubscribes and closes the event channel.
func (s *Subscription) Close() {
	s.internal.Close()
}

// Subscribe returns a subscription for events matching the glob pattern.
func (c *Cache) Subscribe(pattern string) *Subscription {
	return &Subscription{internal: c.broker.Subscribe(pattern)}
}

// OnHit registers a callback for cache hits.
func (c *Cache) OnHit(fn func(key string)) {
	c.broker.OnHit(fn)
}

// OnMiss registers a callback for cache misses.
func (c *Cache) OnMiss(fn func(key string)) {
	c.broker.OnMiss(fn)
}

// OnEvict registers a callback for evictions.
func (c *Cache) OnEvict(fn func(key string, value interface{})) {
	c.broker.OnEvict(func(key string, value []byte) {
		fn(key, value)
	})
}

// OnSet registers a callback for set operations.
func (c *Cache) OnSet(fn func(key string, value interface{})) {
	c.broker.OnSet(func(key string, value []byte) {
		fn(key, value)
	})
}

// OnDelete registers a callback for delete operations.
func (c *Cache) OnDelete(fn func(key string)) {
	c.broker.OnDelete(fn)
}
