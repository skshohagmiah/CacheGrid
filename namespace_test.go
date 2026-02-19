package cachegrid

import (
	"testing"
	"time"
)

func TestNamespacedSetGet(t *testing.T) {
	c := newTestCache(t)
	ns := c.WithNamespace("users")

	ns.Set("alice", "data-alice", time.Minute)

	var dest string
	if !ns.Get("alice", &dest) {
		t.Fatal("expected namespaced Get to find key")
	}
	if dest != "data-alice" {
		t.Fatalf("expected data-alice, got %s", dest)
	}
}

func TestNamespacedIsolation(t *testing.T) {
	c := newTestCache(t)
	ns1 := c.WithNamespace("ns1")
	ns2 := c.WithNamespace("ns2")

	ns1.Set("key", "from-ns1", time.Minute)
	ns2.Set("key", "from-ns2", time.Minute)

	var v1, v2 string
	ns1.Get("key", &v1)
	ns2.Get("key", &v2)

	if v1 != "from-ns1" {
		t.Fatalf("expected from-ns1, got %s", v1)
	}
	if v2 != "from-ns2" {
		t.Fatalf("expected from-ns2, got %s", v2)
	}
}

func TestNamespacedDelete(t *testing.T) {
	c := newTestCache(t)
	ns := c.WithNamespace("tmp")

	ns.Set("key", "val", time.Minute)
	ns.Delete("key")

	if ns.Exists("key") {
		t.Fatal("key should not exist after delete")
	}
}

func TestNamespacedExists(t *testing.T) {
	c := newTestCache(t)
	ns := c.WithNamespace("app")

	if ns.Exists("missing") {
		t.Fatal("should not exist")
	}

	ns.Set("present", 42, time.Minute)
	if !ns.Exists("present") {
		t.Fatal("should exist")
	}
}

func TestNamespacedTTL(t *testing.T) {
	c := newTestCache(t)
	ns := c.WithNamespace("ttl")

	ns.Set("key", "val", 5*time.Minute)
	ttl := ns.TTL("key")
	if ttl <= 0 || ttl > 5*time.Minute {
		t.Fatalf("unexpected TTL: %v", ttl)
	}
}

func TestNamespacedIncr(t *testing.T) {
	c := newTestCache(t)
	ns := c.WithNamespace("counters")

	val, err := ns.Incr("hits", 1)
	if err != nil {
		t.Fatal(err)
	}
	if val != 1 {
		t.Fatalf("expected 1, got %d", val)
	}

	val, err = ns.Incr("hits", 5)
	if err != nil {
		t.Fatal(err)
	}
	if val != 6 {
		t.Fatalf("expected 6, got %d", val)
	}
}

func TestNamespacedDecr(t *testing.T) {
	c := newTestCache(t)
	ns := c.WithNamespace("counters")

	ns.Incr("stock", 10)
	val, err := ns.Decr("stock", 3)
	if err != nil {
		t.Fatal(err)
	}
	if val != 7 {
		t.Fatalf("expected 7, got %d", val)
	}
}

func TestNamespacedGetOrSet(t *testing.T) {
	c := newTestCache(t)
	ns := c.WithNamespace("computed")

	var result string
	err := ns.GetOrSet("expensive", &result, time.Minute, func() (interface{}, error) {
		return "computed-value", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "computed-value" {
		t.Fatalf("expected computed-value, got %s", result)
	}

	// Second call should return cached value
	var cached string
	err = ns.GetOrSet("expensive", &cached, time.Minute, func() (interface{}, error) {
		t.Fatal("fn should not be called on cache hit")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if cached != "computed-value" {
		t.Fatalf("expected cached computed-value, got %s", cached)
	}
}

func TestNamespacedSetWithTags(t *testing.T) {
	c := newTestCache(t)
	ns := c.WithNamespace("tagged")

	ns.SetWithTags("item1", "val1", time.Minute, []string{"group-a"})
	ns.SetWithTags("item2", "val2", time.Minute, []string{"group-a"})

	ns.InvalidateTag("group-a")

	if ns.Exists("item1") || ns.Exists("item2") {
		t.Fatal("tagged items should be invalidated")
	}
}
