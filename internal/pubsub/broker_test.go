package pubsub

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscribeAndPublish(t *testing.T) {
	b := NewBroker()
	defer b.Shutdown()

	sub := b.Subscribe("user:*")
	defer sub.Close()

	b.Publish(Event{Type: EventSet, Key: "user:123", Value: []byte("data")})

	select {
	case ev := <-sub.Events():
		if ev.Key != "user:123" {
			t.Fatalf("expected key 'user:123', got %q", ev.Key)
		}
		if ev.Type != EventSet {
			t.Fatalf("expected EventSet, got %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestPatternMatching(t *testing.T) {
	b := NewBroker()
	defer b.Shutdown()

	sub := b.Subscribe("user:*")
	defer sub.Close()

	// Should match
	b.Publish(Event{Type: EventSet, Key: "user:123"})
	// Should NOT match
	b.Publish(Event{Type: EventSet, Key: "order:456"})

	select {
	case ev := <-sub.Events():
		if ev.Key != "user:123" {
			t.Fatalf("expected 'user:123', got %q", ev.Key)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	// Should not receive the order event
	select {
	case ev := <-sub.Events():
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// OK
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := NewBroker()
	defer b.Shutdown()

	sub1 := b.Subscribe("*")
	sub2 := b.Subscribe("user:*")
	defer sub1.Close()
	defer sub2.Close()

	b.Publish(Event{Type: EventSet, Key: "user:123"})

	// Both should receive
	select {
	case <-sub1.Events():
	case <-time.After(time.Second):
		t.Fatal("sub1 timeout")
	}
	select {
	case <-sub2.Events():
	case <-time.After(time.Second):
		t.Fatal("sub2 timeout")
	}
}

func TestUnsubscribe(t *testing.T) {
	b := NewBroker()
	defer b.Shutdown()

	sub := b.Subscribe("*")
	b.Unsubscribe(sub.ID)

	b.Publish(Event{Type: EventSet, Key: "test"})

	// Channel should be closed
	_, ok := <-sub.Events()
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}

func TestHooks(t *testing.T) {
	b := NewBroker()
	defer b.Shutdown()

	var hitCount, missCount, setCount, deleteCount atomic.Int64

	b.OnHit(func(key string) { hitCount.Add(1) })
	b.OnMiss(func(key string) { missCount.Add(1) })
	b.OnSet(func(key string, value []byte) { setCount.Add(1) })
	b.OnDelete(func(key string) { deleteCount.Add(1) })

	b.FireHit("key1")
	b.FireHit("key2")
	b.FireMiss("key3")
	b.FireSet("key4", nil)
	b.FireDelete("key5")

	if hitCount.Load() != 2 {
		t.Fatalf("expected 2 hits, got %d", hitCount.Load())
	}
	if missCount.Load() != 1 {
		t.Fatalf("expected 1 miss, got %d", missCount.Load())
	}
	if setCount.Load() != 1 {
		t.Fatalf("expected 1 set, got %d", setCount.Load())
	}
	if deleteCount.Load() != 1 {
		t.Fatalf("expected 1 delete, got %d", deleteCount.Load())
	}
}

func TestEvictHook(t *testing.T) {
	b := NewBroker()
	defer b.Shutdown()

	var evictCount atomic.Int64
	b.OnEvict(func(key string, value []byte) { evictCount.Add(1) })

	b.FireEvict("key1", []byte("val"))
	b.FireEvict("key2", []byte("val"))

	if evictCount.Load() != 2 {
		t.Fatalf("expected 2 evictions, got %d", evictCount.Load())
	}
}

func TestPublishSetsTimestamp(t *testing.T) {
	b := NewBroker()
	defer b.Shutdown()

	sub := b.Subscribe("*")
	defer sub.Close()

	b.Publish(Event{Type: EventSet, Key: "test"})

	ev := <-sub.Events()
	if ev.Timestamp.IsZero() {
		t.Fatal("expected timestamp to be set")
	}
}
