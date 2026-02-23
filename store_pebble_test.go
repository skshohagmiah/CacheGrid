package cachegrid

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func newDiskTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := New(Config{
		StorageMode:     Disk,
		DiskPath:        t.TempDir(),
		SweeperInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Shutdown() })
	return c
}

// --- Set / Get ---

func TestDiskSetAndGet(t *testing.T) {
	c := newDiskTestCache(t)

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

func TestDiskSetOverwrite(t *testing.T) {
	c := newDiskTestCache(t)

	c.Set("k", "v1", 0)
	c.Set("k", "v2", 0)

	var val string
	c.Get("k", &val)
	if val != "v2" {
		t.Fatalf("expected 'v2', got %q", val)
	}
}

func TestDiskGetMiss(t *testing.T) {
	c := newDiskTestCache(t)
	var val string
	if c.Get("nonexistent", &val) {
		t.Fatal("expected miss for nonexistent key")
	}
}

func TestDiskSetStruct(t *testing.T) {
	type User struct {
		Name string
		Age  int
	}
	c := newDiskTestCache(t)

	c.Set("user:1", User{Name: "Alice", Age: 30}, 0)

	var u User
	if !c.Get("user:1", &u) {
		t.Fatal("expected key to exist")
	}
	if u.Name != "Alice" || u.Age != 30 {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestDiskSetBytes(t *testing.T) {
	c := newDiskTestCache(t)
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

func TestDiskDelete(t *testing.T) {
	c := newDiskTestCache(t)
	c.Set("k", "v", 0)
	c.Delete("k")

	if c.Exists("k") {
		t.Fatal("expected key to be deleted")
	}
}

func TestDiskDeleteNonexistent(t *testing.T) {
	c := newDiskTestCache(t)
	c.Delete("nope") // should not panic
}

// --- Exists ---

func TestDiskExists(t *testing.T) {
	c := newDiskTestCache(t)
	c.Set("k", "v", 0)

	if !c.Exists("k") {
		t.Fatal("expected key to exist")
	}
	if c.Exists("nope") {
		t.Fatal("expected key not to exist")
	}
}

// --- TTL ---

func TestDiskTTLNoExpiry(t *testing.T) {
	c := newDiskTestCache(t)
	c.Set("k", "v", 0)

	ttl := c.TTL("k")
	if ttl != -1 {
		t.Fatalf("expected TTL -1 for no expiry, got %v", ttl)
	}
}

func TestDiskTTLWithExpiry(t *testing.T) {
	c := newDiskTestCache(t)
	c.Set("k", "v", 10*time.Second)

	ttl := c.TTL("k")
	if ttl <= 0 || ttl > 10*time.Second {
		t.Fatalf("expected TTL between 0 and 10s, got %v", ttl)
	}
}

func TestDiskTTLMiss(t *testing.T) {
	c := newDiskTestCache(t)
	ttl := c.TTL("nope")
	if ttl != 0 {
		t.Fatalf("expected TTL 0 for missing key, got %v", ttl)
	}
}

// --- TTL Expiry ---

func TestDiskLazyExpiry(t *testing.T) {
	c := newDiskTestCache(t)
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

func TestDiskSweeperExpiry(t *testing.T) {
	c, _ := New(Config{
		StorageMode:     Disk,
		DiskPath:        t.TempDir(),
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

func TestDiskDefaultTTL(t *testing.T) {
	c, _ := New(Config{
		StorageMode:     Disk,
		DiskPath:        t.TempDir(),
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

func TestDiskGetOrSetMiss(t *testing.T) {
	c := newDiskTestCache(t)

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
}

func TestDiskGetOrSetHit(t *testing.T) {
	c := newDiskTestCache(t)
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

// --- MGet / MSet ---

func TestDiskMSetAndMGet(t *testing.T) {
	c := newDiskTestCache(t)

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

func TestDiskIncr(t *testing.T) {
	c := newDiskTestCache(t)

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

func TestDiskDecr(t *testing.T) {
	c := newDiskTestCache(t)

	c.Incr("counter", 10)
	val, err := c.Decr("counter", 3)
	if err != nil {
		t.Fatal(err)
	}
	if val != 7 {
		t.Fatalf("expected 7, got %d", val)
	}
}

// --- Empty Key ---

func TestDiskEmptyKeyErrors(t *testing.T) {
	c := newDiskTestCache(t)

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

func TestDiskShutdown(t *testing.T) {
	c := newDiskTestCache(t)
	c.Shutdown()

	if err := c.Set("k", "v", 0); err != ErrShutdown {
		t.Fatalf("expected ErrShutdown, got %v", err)
	}
	if c.Get("k", nil) {
		t.Fatal("expected false after shutdown")
	}
}

// --- Concurrency ---

func TestDiskConcurrentAccess(t *testing.T) {
	c := newDiskTestCache(t)
	var wg sync.WaitGroup
	n := 500

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
}

// --- Len ---

func TestDiskLen(t *testing.T) {
	c := newDiskTestCache(t)

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

// --- Config Validation ---

func TestDiskConfigValidation(t *testing.T) {
	_, err := New(Config{StorageMode: Disk})
	if err == nil {
		t.Fatal("expected error for Disk mode without DiskPath")
	}

	// Valid disk config
	c, err := New(Config{StorageMode: Disk, DiskPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c.Shutdown()
}

// --- Convenience Constructors ---

func TestNewMemoryConstructor(t *testing.T) {
	c, err := NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()

	c.Set("k", "v", 0)
	var val string
	if !c.Get("k", &val) || val != "v" {
		t.Fatal("expected to get value from NewMemory cache")
	}
}

func TestNewDiskConstructor(t *testing.T) {
	c, err := NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()

	c.Set("k", "v", 0)
	var val string
	if !c.Get("k", &val) || val != "v" {
		t.Fatal("expected to get value from NewDisk cache")
	}
}
