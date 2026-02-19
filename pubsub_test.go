package cachegrid

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscribeReceivesEvents(t *testing.T) {
	c := newTestCache(t)
	sub := c.Subscribe("user:*")
	defer sub.Close()

	c.Set("user:1", "alice", time.Minute)

	select {
	case evt := <-sub.Events():
		if evt.Key != "user:1" {
			t.Fatalf("expected key user:1, got %s", evt.Key)
		}
		if evt.Type != EventSet {
			t.Fatalf("expected EventSet, got %s", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSubscribeFiltersByPattern(t *testing.T) {
	c := newTestCache(t)
	sub := c.Subscribe("order:*")
	defer sub.Close()

	// This should NOT match the pattern
	c.Set("user:1", "alice", time.Minute)

	// This should match
	c.Set("order:1", "pizza", time.Minute)

	select {
	case evt := <-sub.Events():
		if evt.Key != "order:1" {
			t.Fatalf("expected order:1, got %s", evt.Key)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for matching event")
	}
}

func TestSubscribeDeleteEvent(t *testing.T) {
	c := newTestCache(t)
	sub := c.Subscribe("*")
	defer sub.Close()

	c.Set("temp", "val", time.Minute)
	// drain set event
	<-sub.Events()

	c.Delete("temp")

	select {
	case evt := <-sub.Events():
		if evt.Type != EventDelete {
			t.Fatalf("expected EventDelete, got %s", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delete event")
	}
}

func TestOnHitMissCallbacks(t *testing.T) {
	c := newTestCache(t)

	var hits, misses atomic.Int64
	c.OnHit(func(key string) { hits.Add(1) })
	c.OnMiss(func(key string) { misses.Add(1) })

	c.Set("exists", "val", time.Minute)

	var dest string
	c.Get("exists", &dest)  // hit
	c.Get("missing", &dest) // miss

	time.Sleep(10 * time.Millisecond)

	if hits.Load() != 1 {
		t.Fatalf("expected 1 hit, got %d", hits.Load())
	}
	if misses.Load() != 1 {
		t.Fatalf("expected 1 miss, got %d", misses.Load())
	}
}

func TestOnSetDeleteCallbacks(t *testing.T) {
	c := newTestCache(t)

	var sets, deletes atomic.Int64
	c.OnSet(func(key string, value interface{}) { sets.Add(1) })
	c.OnDelete(func(key string) { deletes.Add(1) })

	c.Set("x", "v", time.Minute)
	c.Delete("x")

	time.Sleep(10 * time.Millisecond)

	if sets.Load() != 1 {
		t.Fatalf("expected 1 set, got %d", sets.Load())
	}
	if deletes.Load() != 1 {
		t.Fatalf("expected 1 delete, got %d", deletes.Load())
	}
}

func TestSubscriptionClose(t *testing.T) {
	c := newTestCache(t)
	sub := c.Subscribe("*")
	sub.Close()

	// Events channel should be closed
	_, ok := <-sub.Events()
	if ok {
		t.Fatal("expected channel to be closed after Close()")
	}
}
