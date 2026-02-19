package pubsub

import "path"

// Subscription represents a client's subscription to cache events.
type Subscription struct {
	ID      uint64
	pattern string
	ch      chan Event
	done    chan struct{}
	closed  bool
}

func newSubscription(id uint64, pattern string, bufSize int) *Subscription {
	return &Subscription{
		ID:      id,
		pattern: pattern,
		ch:      make(chan Event, bufSize),
		done:    make(chan struct{}),
	}
}

// Events returns the channel to receive events on.
func (s *Subscription) Events() <-chan Event {
	return s.ch
}

// Close unsubscribes and closes the event channel.
func (s *Subscription) Close() {
	if !s.closed {
		s.closed = true
		close(s.done)
		close(s.ch)
	}
}

// Pattern returns the glob pattern for this subscription.
func (s *Subscription) Pattern() string {
	return s.pattern
}

// matches checks if a key matches this subscription's glob pattern.
func (s *Subscription) matches(key string) bool {
	matched, _ := path.Match(s.pattern, key)
	return matched
}
