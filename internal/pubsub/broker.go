package pubsub

import (
	"sync"
	"sync/atomic"
	"time"
)

const defaultBufSize = 256

// Broker manages pub/sub subscriptions and event dispatch.
type Broker struct {
	mu            sync.RWMutex
	subscriptions map[uint64]*Subscription
	nextID        atomic.Uint64

	hookMu   sync.RWMutex
	onHit    []func(key string)
	onMiss   []func(key string)
	onEvict  []func(key string, value []byte)
	onSet    []func(key string, value []byte)
	onDelete []func(key string)
}

// NewBroker creates a new event broker.
func NewBroker() *Broker {
	return &Broker{
		subscriptions: make(map[uint64]*Subscription),
	}
}

// Subscribe creates a new subscription matching the given glob pattern.
func (b *Broker) Subscribe(pattern string) *Subscription {
	id := b.nextID.Add(1)
	sub := newSubscription(id, pattern, defaultBufSize)

	b.mu.Lock()
	b.subscriptions[id] = sub
	b.mu.Unlock()

	return sub
}

// Unsubscribe removes a subscription.
func (b *Broker) Unsubscribe(id uint64) {
	b.mu.Lock()
	if sub, ok := b.subscriptions[id]; ok {
		sub.Close()
		delete(b.subscriptions, id)
	}
	b.mu.Unlock()
}

// Publish dispatches an event to all matching subscriptions.
// Non-blocking: if a subscriber's channel is full, the event is dropped.
func (b *Broker) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.RLock()
	for _, sub := range b.subscriptions {
		if sub.closed {
			continue
		}
		if sub.matches(event.Key) {
			select {
			case sub.ch <- event:
			default:
				// drop if buffer full
			}
		}
	}
	b.mu.RUnlock()
}

// OnHit registers a callback for cache hits.
func (b *Broker) OnHit(fn func(key string)) {
	b.hookMu.Lock()
	b.onHit = append(b.onHit, fn)
	b.hookMu.Unlock()
}

// OnMiss registers a callback for cache misses.
func (b *Broker) OnMiss(fn func(key string)) {
	b.hookMu.Lock()
	b.onMiss = append(b.onMiss, fn)
	b.hookMu.Unlock()
}

// OnEvict registers a callback for evictions.
func (b *Broker) OnEvict(fn func(key string, value []byte)) {
	b.hookMu.Lock()
	b.onEvict = append(b.onEvict, fn)
	b.hookMu.Unlock()
}

// OnSet registers a callback for set operations.
func (b *Broker) OnSet(fn func(key string, value []byte)) {
	b.hookMu.Lock()
	b.onSet = append(b.onSet, fn)
	b.hookMu.Unlock()
}

// OnDelete registers a callback for delete operations.
func (b *Broker) OnDelete(fn func(key string)) {
	b.hookMu.Lock()
	b.onDelete = append(b.onDelete, fn)
	b.hookMu.Unlock()
}

// FireHit fires all OnHit callbacks.
func (b *Broker) FireHit(key string) {
	b.hookMu.RLock()
	for _, fn := range b.onHit {
		fn(key)
	}
	b.hookMu.RUnlock()
}

// FireMiss fires all OnMiss callbacks.
func (b *Broker) FireMiss(key string) {
	b.hookMu.RLock()
	for _, fn := range b.onMiss {
		fn(key)
	}
	b.hookMu.RUnlock()
}

// FireEvict fires all OnEvict callbacks.
func (b *Broker) FireEvict(key string, value []byte) {
	b.hookMu.RLock()
	for _, fn := range b.onEvict {
		fn(key, value)
	}
	b.hookMu.RUnlock()
}

// FireSet fires all OnSet callbacks.
func (b *Broker) FireSet(key string, value []byte) {
	b.hookMu.RLock()
	for _, fn := range b.onSet {
		fn(key, value)
	}
	b.hookMu.RUnlock()
}

// FireDelete fires all OnDelete callbacks.
func (b *Broker) FireDelete(key string) {
	b.hookMu.RLock()
	for _, fn := range b.onDelete {
		fn(key)
	}
	b.hookMu.RUnlock()
}

// Shutdown cleans up all subscriptions.
func (b *Broker) Shutdown() {
	b.mu.Lock()
	for id, sub := range b.subscriptions {
		sub.Close()
		delete(b.subscriptions, id)
	}
	b.mu.Unlock()
}
