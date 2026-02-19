package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucketAllow(t *testing.T) {
	tb := NewTokenBucket()

	// Should allow first 10 requests
	for i := 0; i < 10; i++ {
		s := tb.Allow("api:user:1", 10, time.Second)
		if !s.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	// 11th should be denied
	s := tb.Allow("api:user:1", 10, time.Second)
	if s.Allowed {
		t.Fatal("11th request should be denied")
	}
	if s.Remaining != 0 {
		t.Fatalf("expected 0 remaining, got %d", s.Remaining)
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := NewTokenBucket()

	// Exhaust all tokens
	for i := 0; i < 10; i++ {
		tb.Allow("key", 10, 100*time.Millisecond)
	}

	// Wait for refill
	time.Sleep(110 * time.Millisecond)

	s := tb.Allow("key", 10, 100*time.Millisecond)
	if !s.Allowed {
		t.Fatal("should be allowed after refill")
	}
}

func TestTokenBucketReset(t *testing.T) {
	tb := NewTokenBucket()

	for i := 0; i < 10; i++ {
		tb.Allow("key", 10, time.Second)
	}

	tb.Reset("key")

	s := tb.Allow("key", 10, time.Second)
	if !s.Allowed {
		t.Fatal("should be allowed after reset")
	}
}

func TestTokenBucketSeparateKeys(t *testing.T) {
	tb := NewTokenBucket()

	for i := 0; i < 5; i++ {
		tb.Allow("user:1", 5, time.Second)
	}

	// user:2 should still have tokens
	s := tb.Allow("user:2", 5, time.Second)
	if !s.Allowed {
		t.Fatal("different key should have independent limit")
	}
}

func TestSlidingWindowAllow(t *testing.T) {
	sw := NewSlidingWindow()

	for i := 0; i < 10; i++ {
		s := sw.Allow("key", 10, time.Second)
		if !s.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	s := sw.Allow("key", 10, time.Second)
	if s.Allowed {
		t.Fatal("11th request should be denied")
	}
}

func TestSlidingWindowReset(t *testing.T) {
	sw := NewSlidingWindow()

	for i := 0; i < 10; i++ {
		sw.Allow("key", 10, time.Second)
	}

	sw.Reset("key")

	s := sw.Allow("key", 10, time.Second)
	if !s.Allowed {
		t.Fatal("should be allowed after reset")
	}
}

func TestSlidingWindowSlide(t *testing.T) {
	sw := NewSlidingWindow()

	for i := 0; i < 10; i++ {
		sw.Allow("key", 10, 100*time.Millisecond)
	}

	// Wait for window to fully pass
	time.Sleep(210 * time.Millisecond)

	s := sw.Allow("key", 10, 100*time.Millisecond)
	if !s.Allowed {
		t.Fatal("should be allowed after window passes")
	}
}

func TestSlidingWindowState(t *testing.T) {
	sw := NewSlidingWindow()

	s := sw.Allow("key", 100, time.Minute)
	if !s.Allowed {
		t.Fatal("should be allowed")
	}
	if s.Limit != 100 {
		t.Fatalf("expected limit 100, got %d", s.Limit)
	}
	if s.Remaining < 90 {
		t.Fatalf("expected remaining ~99, got %d", s.Remaining)
	}
}
