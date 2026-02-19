package cachegrid

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := New(Config{
		NumShards:       16,
		SweeperInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Shutdown() })
	return c
}

// --- Set / Get ---

func TestSetAndGet(t *testing.T) {
	c := newTestCache(t)

	if err := c.Set("key1", "hello", 0); err != nil {
		t.Fatal(err)
	}

	var val string
	if !c.Get("key1", &val) {
		t.Fatal("expected key1 to exist")
	}
	if val != "hello" {
		t.Fatalf("expected 'hello', got %q", val)
	}
}

func TestSetOverwrite(t *testing.T) {
	c := newTestCache(t)

	c.Set("k", "v1", 0)
	c.Set("k", "v2", 0)

	var val string
	c.Get("k", &val)
	if val != "v2" {
		t.Fatalf("expected 'v2', got %q", val)
	}
}

func TestGetMiss(t *testing.T) {
	c := newTestCache(t)
	var val string
	if c.Get("nonexistent", &val) {
		t.Fatal("expected miss for nonexistent key")
	}
}

func TestSetStruct(t *testing.T) {
	type User struct {
		Name string
		Age  int
	}
	c := newTestCache(t)

	c.Set("user:1", User{Name: "Alice", Age: 30}, 0)

	var u User
	if !c.Get("user:1", &u) {
		t.Fatal("expected key to exist")
	}
	if u.Name != "Alice" || u.Age != 30 {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestSetBytes(t *testing.T) {
	c := newTestCache(t)
	data := []byte{0x01, 0x02, 0x03}
	c.Set("bin", data, 0)

	var out []byte
	if !c.Get("bin", &out) {
		t.Fatal("expected key to exist")
	}
	if len(out) != 3 || out[0] != 0x01 {
		t.Fatalf("unexpected bytes: %v", out)
	}
}

// --- Delete ---

func TestDelete(t *testing.T) {
	c := newTestCache(t)
	c.Set("k", "v", 0)
	c.Delete("k")

	if c.Exists("k") {
		t.Fatal("expected key to be deleted")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	c := newTestCache(t)
	c.Delete("nope") // should not panic
}

// --- Exists ---

func TestExists(t *testing.T) {
	c := newTestCache(t)
	c.Set("k", "v", 0)

	if !c.Exists("k") {
		t.Fatal("expected key to exist")
	}
	if c.Exists("nope") {
		t.Fatal("expected key not to exist")
	}
}

// --- TTL ---

func TestTTLNoExpiry(t *testing.T) {
	c := newTestCache(t)
	c.Set("k", "v", 0)

	ttl := c.TTL("k")
	if ttl != -1 {
		t.Fatalf("expected TTL -1 for no expiry, got %v", ttl)
	}
}

func TestTTLWithExpiry(t *testing.T) {
	c := newTestCache(t)
	c.Set("k", "v", 10*time.Second)

	ttl := c.TTL("k")
	if ttl <= 0 || ttl > 10*time.Second {
		t.Fatalf("expected TTL between 0 and 10s, got %v", ttl)
	}
}

func TestTTLMiss(t *testing.T) {
	c := newTestCache(t)
	ttl := c.TTL("nope")
	if ttl != 0 {
		t.Fatalf("expected TTL 0 for missing key, got %v", ttl)
	}
}

// --- TTL Expiry ---

func TestLazyExpiry(t *testing.T) {
	c := newTestCache(t)
	c.Set("k", "v", 50*time.Millisecond)

	time.Sleep(60 * time.Millisecond)

	var val string
	if c.Get("k", &val) {
		t.Fatal("expected key to be expired")
	}
	if c.Exists("k") {
		t.Fatal("expected key to not exist after expiry")
	}
}

func TestSweeperExpiry(t *testing.T) {
	c, _ := New(Config{
		NumShards:       4,
		SweeperInterval: 50 * time.Millisecond,
	})
	defer c.Shutdown()

	for i := 0; i < 100; i++ {
		c.Set(fmt.Sprintf("k%d", i), "v", 50*time.Millisecond)
	}

	if c.Len() != 100 {
		t.Fatalf("expected 100 items, got %d", c.Len())
	}

	// Wait for entries to expire and sweeper to run
	time.Sleep(300 * time.Millisecond)

	if c.Len() != 0 {
		t.Fatalf("expected 0 items after sweep, got %d", c.Len())
	}
}

func TestDefaultTTL(t *testing.T) {
	c, _ := New(Config{
		NumShards:       4,
		DefaultTTL:      50 * time.Millisecond,
		SweeperInterval: time.Second,
	})
	defer c.Shutdown()

	c.Set("k", "v", 0) // should use DefaultTTL

	time.Sleep(60 * time.Millisecond)

	var val string
	if c.Get("k", &val) {
		t.Fatal("expected key to be expired by default TTL")
	}
}

// --- GetOrSet ---

func TestGetOrSetMiss(t *testing.T) {
	c := newTestCache(t)

	var val string
	called := false
	err := c.GetOrSet("k", &val, time.Minute, func() (interface{}, error) {
		called = true
		return "computed", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected fn to be called on miss")
	}
	if val != "computed" {
		t.Fatalf("expected 'computed', got %q", val)
	}

	// Verify it's cached now
	var val2 string
	c.Get("k", &val2)
	if val2 != "computed" {
		t.Fatalf("expected 'computed' in cache, got %q", val2)
	}
}

func TestGetOrSetHit(t *testing.T) {
	c := newTestCache(t)
	c.Set("k", "existing", 0)

	var val string
	called := false
	err := c.GetOrSet("k", &val, time.Minute, func() (interface{}, error) {
		called = true
		return "new", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("expected fn NOT to be called on hit")
	}
	if val != "existing" {
		t.Fatalf("expected 'existing', got %q", val)
	}
}

func TestGetOrSetFnError(t *testing.T) {
	c := newTestCache(t)

	var val string
	err := c.GetOrSet("k", &val, time.Minute, func() (interface{}, error) {
		return nil, fmt.Errorf("db error")
	})
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected 'db error', got %v", err)
	}
}

// --- MGet / MSet ---

func TestMSetAndMGet(t *testing.T) {
	c := newTestCache(t)

	err := c.MSet(map[string]Item{
		"a": {Value: "alpha", TTL: time.Minute},
		"b": {Value: "beta", TTL: time.Minute},
		"c": {Value: "gamma", TTL: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}

	results := c.MGet("a", "b", "c", "missing")
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results["missing"] != nil {
		t.Fatal("expected nil for missing key")
	}
}

// --- Incr / Decr ---

func TestIncr(t *testing.T) {
	c := newTestCache(t)

	val, err := c.Incr("counter", 1)
	if err != nil {
		t.Fatal(err)
	}
	if val != 1 {
		t.Fatalf("expected 1, got %d", val)
	}

	val, err = c.Incr("counter", 5)
	if err != nil {
		t.Fatal(err)
	}
	if val != 6 {
		t.Fatalf("expected 6, got %d", val)
	}
}

func TestDecr(t *testing.T) {
	c := newTestCache(t)

	c.Incr("counter", 10)
	val, err := c.Decr("counter", 3)
	if err != nil {
		t.Fatal(err)
	}
	if val != 7 {
		t.Fatalf("expected 7, got %d", val)
	}
}

// --- LRU Eviction ---

func TestLRUEviction(t *testing.T) {
	c, _ := New(Config{
		NumShards:       1,
		MaxMemoryMB:     1, // 1MB limit
		SweeperInterval: time.Hour,
	})
	defer c.Shutdown()

	// Fill the cache beyond 1MB
	bigVal := make([]byte, 1024) // 1KB per entry
	for i := 0; i < 2000; i++ {
		c.Set(fmt.Sprintf("key:%d", i), bigVal, 0)
	}

	// Should have evicted some entries
	if c.Len() >= 2000 {
		t.Fatalf("expected eviction to reduce item count, got %d", c.Len())
	}
}

// --- Empty Key ---

func TestEmptyKeyErrors(t *testing.T) {
	c := newTestCache(t)

	if err := c.Set("", "val", 0); err != ErrKeyEmpty {
		t.Fatalf("expected ErrKeyEmpty, got %v", err)
	}
	if c.Get("", nil) {
		t.Fatal("expected false for empty key")
	}
	if c.Exists("") {
		t.Fatal("expected false for empty key")
	}
}

// --- Shutdown ---

func TestShutdown(t *testing.T) {
	c := newTestCache(t)
	c.Shutdown()

	if err := c.Set("k", "v", 0); err != ErrShutdown {
		t.Fatalf("expected ErrShutdown, got %v", err)
	}
	if c.Get("k", nil) {
		t.Fatal("expected false after shutdown")
	}
}

// --- Concurrency ---

func TestConcurrentAccess(t *testing.T) {
	c := newTestCache(t)
	var wg sync.WaitGroup
	n := 1000

	// Concurrent writes
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			c.Set(fmt.Sprintf("key:%d", i), i, time.Minute)
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			var val int
			c.Get(fmt.Sprintf("key:%d", i), &val)
		}(i)
	}
	wg.Wait()

	// Concurrent mixed
	wg.Add(n * 3)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			c.Set(fmt.Sprintf("key:%d", i), i*2, time.Minute)
		}(i)
		go func(i int) {
			defer wg.Done()
			var val int
			c.Get(fmt.Sprintf("key:%d", i), &val)
		}(i)
		go func(i int) {
			defer wg.Done()
			c.Delete(fmt.Sprintf("key:%d", i))
		}(i)
	}
	wg.Wait()
}

// --- Config Validation ---

func TestConfigValidation(t *testing.T) {
	_, err := New(Config{NumShards: 3})
	if err == nil {
		t.Fatal("expected error for non-power-of-2 NumShards")
	}

	_, err = New(Config{NumShards: 4, MaxMemoryMB: -1})
	if err == nil {
		t.Fatal("expected error for negative MaxMemoryMB")
	}

	// Valid config
	c, err := New(Config{NumShards: 4})
	if err != nil {
		t.Fatal(err)
	}
	c.Shutdown()
}

// --- Len ---

func TestLen(t *testing.T) {
	c := newTestCache(t)

	for i := 0; i < 50; i++ {
		c.Set(fmt.Sprintf("k%d", i), i, 0)
	}
	if c.Len() != 50 {
		t.Fatalf("expected 50, got %d", c.Len())
	}

	for i := 0; i < 10; i++ {
		c.Delete(fmt.Sprintf("k%d", i))
	}
	if c.Len() != 40 {
		t.Fatalf("expected 40, got %d", c.Len())
	}
}
