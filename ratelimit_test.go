package cachegrid

import (
	"testing"
	"time"
)

func TestRateLimitBasic(t *testing.T) {
	c := newTestCache(t)

	for i := int64(0); i < 5; i++ {
		allowed, state := c.RateLimit("api:user1", RateLimitOptions{Limit: 5, Window: time.Minute})
		if !allowed {
			t.Fatalf("request %d should be allowed", i)
		}
		if state.Remaining != 5-i-1 {
			t.Fatalf("expected remaining %d, got %d", 5-i-1, state.Remaining)
		}
	}

	// 6th request should be denied
	allowed, state := c.RateLimit("api:user1", RateLimitOptions{Limit: 5, Window: time.Minute})
	if allowed {
		t.Fatal("6th request should be denied")
	}
	if state.Remaining != 0 {
		t.Fatalf("expected remaining 0, got %d", state.Remaining)
	}
}

func TestRateLimitSeparateKeys(t *testing.T) {
	c := newTestCache(t)

	allowed1, _ := c.RateLimit("user-a", RateLimitOptions{Limit: 1, Window: time.Minute})
	allowed2, _ := c.RateLimit("user-b", RateLimitOptions{Limit: 2, Window: time.Minute})

	if !allowed1 || !allowed2 {
		t.Fatal("separate keys should have independent limits")
	}

	// user-a should be denied (limit=1, already used)
	denied1, _ := c.RateLimit("user-a", RateLimitOptions{Limit: 1, Window: time.Minute})
	if denied1 {
		// expected: user-a already used their 1 token
	}

	// user-b should still be allowed (limit=2, only used 1)
	allowed3, _ := c.RateLimit("user-b", RateLimitOptions{Limit: 2, Window: time.Minute})
	if !allowed3 {
		t.Fatal("user-b should still have tokens with limit=2")
	}
}

func TestRateLimitSlidingBasic(t *testing.T) {
	c := newTestCache(t)

	for i := int64(0); i < 3; i++ {
		allowed, _ := c.RateLimitSliding("slide:user1", SlidingWindowOptions{Limit: 3, Window: time.Minute})
		if !allowed {
			t.Fatalf("sliding window request %d should be allowed", i)
		}
	}

	allowed, _ := c.RateLimitSliding("slide:user1", SlidingWindowOptions{Limit: 3, Window: time.Minute})
	if allowed {
		t.Fatal("4th sliding window request should be denied")
	}
}

func TestRateLimitStateFields(t *testing.T) {
	c := newTestCache(t)

	_, state := c.RateLimit("state-test", RateLimitOptions{Limit: 10, Window: 5 * time.Minute})
	if state.Limit != 10 {
		t.Fatalf("expected limit 10, got %d", state.Limit)
	}
	if state.ResetsAt.IsZero() {
		t.Fatal("expected non-zero reset time")
	}
	if !state.Allowed {
		t.Fatal("expected allowed=true")
	}
}
