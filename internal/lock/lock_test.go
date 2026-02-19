package lock

import (
	"sync"
	"testing"
	"time"
)

func TestAcquireRelease(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	token, fencing, acquired := e.Acquire("resource", 5*time.Second, "node-1")
	if !acquired {
		t.Fatal("expected lock to be acquired")
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if fencing == 0 {
		t.Fatal("expected non-zero fencing token")
	}

	if !e.Release("resource", token) {
		t.Fatal("expected release to succeed")
	}

	if e.IsHeld("resource") {
		t.Fatal("expected lock to be released")
	}
}

func TestDoubleAcquire(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	_, _, acquired := e.Acquire("resource", 5*time.Second, "node-1")
	if !acquired {
		t.Fatal("first acquire should succeed")
	}

	_, _, acquired = e.Acquire("resource", 5*time.Second, "node-2")
	if acquired {
		t.Fatal("second acquire should fail")
	}
}

func TestTokenMismatchRelease(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	e.Acquire("resource", 5*time.Second, "node-1")

	if e.Release("resource", "wrong-token") {
		t.Fatal("release with wrong token should fail")
	}
}

func TestLockExpiry(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	e.Acquire("resource", 50*time.Millisecond, "node-1")
	time.Sleep(60 * time.Millisecond)

	_, _, acquired := e.Acquire("resource", 5*time.Second, "node-2")
	if !acquired {
		t.Fatal("should acquire after expiry")
	}
}

func TestExtend(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	token, _, _ := e.Acquire("resource", 100*time.Millisecond, "node-1")

	if !e.Extend("resource", token, 5*time.Second) {
		t.Fatal("extend should succeed")
	}

	time.Sleep(150 * time.Millisecond)

	if !e.IsHeld("resource") {
		t.Fatal("lock should still be held after extend")
	}
}

func TestExtendWrongToken(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	e.Acquire("resource", 5*time.Second, "node-1")

	if e.Extend("resource", "wrong-token", 10*time.Second) {
		t.Fatal("extend with wrong token should fail")
	}
}

func TestFencingTokensIncrement(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	_, f1, _ := e.Acquire("r1", 5*time.Second, "node-1")
	_, f2, _ := e.Acquire("r2", 5*time.Second, "node-1")

	if f2 <= f1 {
		t.Fatalf("fencing tokens should be monotonically increasing: %d <= %d", f2, f1)
	}
}

func TestConcurrentAcquire(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	acquired := make(chan bool, 100)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, ok := e.Acquire("resource", 5*time.Second, "node")
			acquired <- ok
		}()
	}
	wg.Wait()
	close(acquired)

	count := 0
	for ok := range acquired {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 successful acquire, got %d", count)
	}
}

func TestActiveLocks(t *testing.T) {
	e := NewEngine()
	defer e.Shutdown()

	e.Acquire("r1", 5*time.Second, "node-1")
	e.Acquire("r2", 5*time.Second, "node-1")

	locks := e.ActiveLocks()
	if len(locks) != 2 {
		t.Fatalf("expected 2 active locks, got %d", len(locks))
	}
}
