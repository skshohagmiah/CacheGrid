package cachegrid

import (
	"testing"
	"time"
)

func TestDistributedLocalOnlyMode(t *testing.T) {
	c := newTestCache(t)

	// In local-only mode, ownerAddr should always return ""
	addr := c.ownerAddr("any-key")
	if addr != "" {
		t.Fatalf("expected empty ownerAddr in local mode, got %s", addr)
	}
}

func TestDistributedSetGetDelete(t *testing.T) {
	c := newTestCache(t)

	// These all go through distributed routing but should work locally
	if err := c.distributedSet("key1", []byte("value1"), time.Minute, nil); err != nil {
		t.Fatal(err)
	}

	data, ok := c.distributedGet("key1")
	if !ok {
		t.Fatal("expected to find key1")
	}
	if string(data) != "value1" {
		t.Fatalf("expected value1, got %s", string(data))
	}

	c.distributedDelete("key1")

	_, ok = c.distributedGet("key1")
	if ok {
		t.Fatal("key1 should be deleted")
	}
}

func TestLocalHandlerImplementation(t *testing.T) {
	c := newTestCache(t)

	// Test HandleSet + HandleGet
	c.HandleSet("test-key", []byte("test-value"), time.Minute, nil)
	data, ok := c.HandleGet("test-key")
	if !ok {
		t.Fatal("expected HandleGet to find test-key")
	}
	if string(data) != "test-value" {
		t.Fatalf("expected test-value, got %s", string(data))
	}

	// Test HandleExists
	if !c.HandleExists("test-key") {
		t.Fatal("expected HandleExists to return true")
	}

	// Test HandleDelete
	if !c.HandleDelete("test-key") {
		t.Fatal("expected HandleDelete to return true")
	}
	if c.HandleExists("test-key") {
		t.Fatal("expected HandleExists to return false after delete")
	}
}

func TestHandleMGet(t *testing.T) {
	c := newTestCache(t)

	c.HandleSet("a", []byte("va"), time.Minute, nil)
	c.HandleSet("b", []byte("vb"), time.Minute, nil)

	results := c.HandleMGet([]string{"a", "b", "missing"})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if string(results["a"]) != "va" {
		t.Fatalf("expected va, got %s", string(results["a"]))
	}
}

func TestHandleIncr(t *testing.T) {
	c := newTestCache(t)

	val, err := c.HandleIncr("counter", 5)
	if err != nil {
		t.Fatal(err)
	}
	if val != 5 {
		t.Fatalf("expected 5, got %d", val)
	}

	val, err = c.HandleIncr("counter", 3)
	if err != nil {
		t.Fatal(err)
	}
	if val != 8 {
		t.Fatalf("expected 8, got %d", val)
	}
}

func TestHandleLockAcquireRelease(t *testing.T) {
	c := newTestCache(t)

	token, fencing, acquired := c.HandleLockAcquire("mylock", 5*time.Second)
	if !acquired {
		t.Fatal("expected lock to be acquired")
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if fencing == 0 {
		t.Fatal("expected non-zero fencing token")
	}

	// Release
	if !c.HandleLockRelease("mylock", token) {
		t.Fatal("expected release to succeed")
	}

	// Release again should fail
	if c.HandleLockRelease("mylock", token) {
		t.Fatal("expected second release to fail")
	}
}

func TestHandleLockExtend(t *testing.T) {
	c := newTestCache(t)

	token, _, acquired := c.HandleLockAcquire("ext-lock", time.Second)
	if !acquired {
		t.Fatal("expected lock to be acquired")
	}

	if !c.HandleLockExtend("ext-lock", token, 5*time.Second) {
		t.Fatal("expected extend to succeed")
	}

	// Wrong token should fail
	if c.HandleLockExtend("ext-lock", "wrong-token", 5*time.Second) {
		t.Fatal("expected extend with wrong token to fail")
	}
}

func TestHandleSetWithTags(t *testing.T) {
	c := newTestCache(t)

	c.HandleSet("tagged-key", []byte("tagged-val"), time.Minute, []string{"tag1"})

	data, ok := c.HandleGet("tagged-key")
	if !ok {
		t.Fatal("expected to find tagged-key")
	}
	if string(data) != "tagged-val" {
		t.Fatalf("expected tagged-val, got %s", string(data))
	}
}
