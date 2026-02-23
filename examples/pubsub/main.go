// Example: pubsub
//
// Demonstrates CacheGrid's pub/sub system for real-time cache event monitoring.
// Starts a cache, subscribes to events, and performs operations to trigger them.
//
// Run: go run ./examples/pubsub
package main

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/skshohagmiah/cachegrid"
)

func main() {
	cache, err := cachegrid.New(cachegrid.Config{
		NumShards:       16,
		MaxMemoryMB:     1, // small limit to trigger evictions
		SweeperInterval: 200 * time.Millisecond,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Shutdown()

	// ── 1. Pattern-Based Subscriptions ───────────────────
	fmt.Println("=== Pattern-Based Subscriptions ===")
	fmt.Println("Subscribing to user:* events...\n")

	sub := cache.Subscribe("user:*")
	defer sub.Close()

	// Consume events in background
	done := make(chan struct{})
	go func() {
		for event := range sub.Events() {
			fmt.Printf("  [EVENT] type=%-8s key=%s\n", event.Type, event.Key)
		}
		close(done)
	}()

	// Trigger events
	cache.Set("user:1", "Alice", time.Minute)
	cache.Set("user:2", "Bob", time.Minute)
	cache.Set("user:3", "Charlie", time.Minute)
	cache.Delete("user:2")

	// This should NOT appear in subscription (different pattern)
	cache.Set("order:100", "some order", time.Minute)

	time.Sleep(50 * time.Millisecond) // let events propagate

	// ── 2. Event Hooks (Metrics) ─────────────────────────
	fmt.Println("\n=== Event Hooks for Metrics ===")

	var hits, misses, sets, deletes atomic.Int64

	cache.OnHit(func(key string) { hits.Add(1) })
	cache.OnMiss(func(key string) { misses.Add(1) })
	cache.OnSet(func(key string, value interface{}) { sets.Add(1) })
	cache.OnDelete(func(key string) { deletes.Add(1) })

	// Generate some traffic
	for i := 0; i < 20; i++ {
		cache.Set(fmt.Sprintf("metric:%d", i), i, time.Minute)
	}
	for i := 0; i < 20; i++ {
		var v int
		cache.Get(fmt.Sprintf("metric:%d", i), &v)
	}
	for i := 0; i < 5; i++ {
		var v int
		cache.Get(fmt.Sprintf("missing:%d", i), &v)
	}

	time.Sleep(50 * time.Millisecond)
	fmt.Printf("  Hits:    %d\n", hits.Load())
	fmt.Printf("  Misses:  %d\n", misses.Load())
	fmt.Printf("  Sets:    %d\n", sets.Load())
	fmt.Printf("  Deletes: %d\n", deletes.Load())

	// ── 3. Eviction Events ───────────────────────────────
	fmt.Println("\n=== Eviction Monitoring ===")
	fmt.Println("Filling cache beyond 1MB to trigger LRU evictions...")

	var evictions atomic.Int64
	cache.OnEvict(func(key string, value interface{}) {
		evictions.Add(1)
	})

	// Fill beyond the 1MB limit
	bigVal := make([]byte, 1024) // 1KB each
	for i := 0; i < 2000; i++ {
		cache.Set(fmt.Sprintf("fill:%d", i), bigVal, time.Hour)
	}

	time.Sleep(100 * time.Millisecond)
	fmt.Printf("  Items in cache: %d (some evicted)\n", cache.Len())
	fmt.Printf("  Evictions:      %d\n", evictions.Load())

	// ── 4. Expiry Events ─────────────────────────────────
	fmt.Println("\n=== Expiry Monitoring ===")

	expireSub := cache.Subscribe("expiring:*")
	defer expireSub.Close()

	go func() {
		for event := range expireSub.Events() {
			if event.Type == cachegrid.EventExpire {
				fmt.Printf("  [EXPIRED] key=%s\n", event.Key)
			}
		}
	}()

	// Set keys with short TTL
	for i := 0; i < 5; i++ {
		cache.Set(fmt.Sprintf("expiring:%d", i), "temp", 200*time.Millisecond)
	}

	fmt.Println("Waiting for keys to expire...")
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\nDone!")
}
