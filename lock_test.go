package cachegrid

import (
	"testing"
	"time"
)

func TestLockAndRelease(t *testing.T) {
	c := newTestCache(t)

	handle, err := c.Lock("resource-1", LockOptions{TTL: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if handle == nil {
		t.Fatal("expected non-nil handle")
	}
	if handle.Token() == 0 {
		t.Fatal("expected non-zero fencing token")
	}

	if err := handle.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}
}

func TestLockConflict(t *testing.T) {
	c := newTestCache(t)

	h1, err := c.Lock("resource-2", LockOptions{TTL: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	// Second lock should fail with no retries
	_, err = c.Lock("resource-2", LockOptions{TTL: 5 * time.Second, RetryCount: 0})
	if err != ErrLockNotAcquired {
		t.Fatalf("expected ErrLockNotAcquired, got %v", err)
	}

	h1.Release()
}

func TestLockRetry(t *testing.T) {
	c := newTestCache(t)

	h1, err := c.Lock("resource-3", LockOptions{TTL: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_ = h1

	// Should succeed after TTL expires
	h2, err := c.Lock("resource-3", LockOptions{
		TTL:        5 * time.Second,
		RetryCount: 5,
		RetryDelay: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("expected lock to succeed after expiry, got: %v", err)
	}
	h2.Release()
}

func TestTryLock(t *testing.T) {
	c := newTestCache(t)

	handle, ok := c.TryLock("resource-4", 5*time.Second)
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}
	handle.Release()

	// Re-acquire should work
	handle2, ok := c.TryLock("resource-4", 5*time.Second)
	if !ok {
		t.Fatal("expected second TryLock to succeed after release")
	}
	handle2.Release()
}

func TestLockExtend(t *testing.T) {
	c := newTestCache(t)

	handle, err := c.Lock("resource-5", LockOptions{TTL: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	if err := handle.Extend(5 * time.Second); err != nil {
		t.Fatalf("extend failed: %v", err)
	}

	// Sleep past original TTL — should still be held
	time.Sleep(150 * time.Millisecond)

	// Release should work since we extended
	if err := handle.Release(); err != nil {
		t.Fatalf("release after extend failed: %v", err)
	}
}

func TestLockFencingTokenMonotonic(t *testing.T) {
	c := newTestCache(t)

	h1, _ := c.Lock("resource-6", LockOptions{TTL: 50 * time.Millisecond})
	t1 := h1.Token()
	h1.Release()

	h2, _ := c.Lock("resource-6", LockOptions{TTL: 50 * time.Millisecond})
	t2 := h2.Token()
	h2.Release()

	if t2 <= t1 {
		t.Fatalf("fencing tokens should be monotonically increasing: %d <= %d", t2, t1)
	}
}

func TestLockEmptyKey(t *testing.T) {
	c := newTestCache(t)
	_, err := c.Lock("", LockOptions{TTL: time.Second})
	if err != ErrKeyEmpty {
		t.Fatalf("expected ErrKeyEmpty, got %v", err)
	}
}

func TestLockAfterShutdown(t *testing.T) {
	c := newTestCache(t)
	c.Shutdown()

	_, err := c.Lock("key", LockOptions{TTL: time.Second})
	if err != ErrShutdown {
		t.Fatalf("expected ErrShutdown, got %v", err)
	}
}
