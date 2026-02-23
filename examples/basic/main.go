// Example: basic
//
// Demonstrates core CacheGrid features: in-memory and disk storage,
// CRUD operations, TTL, atomic counters, tags, and namespaces.
//
// Run: go run ./examples/basic
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/skshohagmiah/cachegrid"
)

func main() {
	// ── 1. In-Memory Cache (default) ─────────────────────
	fmt.Println("=== In-Memory Cache ===")

	cache, err := cachegrid.NewMemory()
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Shutdown()

	// Basic Set / Get
	cache.Set("greeting", "Hello, CacheGrid!", 5*time.Minute)

	var greeting string
	if cache.Get("greeting", &greeting) {
		fmt.Println("Get:", greeting)
	}

	// Store structs
	type User struct {
		Name  string
		Email string
		Age   int
	}

	cache.Set("user:1", User{
		Name:  "Alice",
		Email: "alice@example.com",
		Age:   30,
	}, 10*time.Minute)

	var user User
	if cache.Get("user:1", &user) {
		fmt.Printf("User: %s (%s), age %d\n", user.Name, user.Email, user.Age)
	}

	// Check existence and TTL
	fmt.Println("Exists:", cache.Exists("user:1"))
	fmt.Println("TTL:", cache.TTL("user:1").Round(time.Second))

	// Delete
	cache.Delete("greeting")
	fmt.Println("After delete, exists:", cache.Exists("greeting"))

	// ── 2. Atomic Counters ───────────────────────────────
	fmt.Println("\n=== Atomic Counters ===")

	cache.Incr("page:views", 1)
	cache.Incr("page:views", 1)
	cache.Incr("page:views", 1)
	views, _ := cache.Incr("page:views", 0) // read without incrementing
	fmt.Println("Page views:", views)

	cache.Incr("stock:item-42", 100)
	cache.Decr("stock:item-42", 3)
	stock, _ := cache.Incr("stock:item-42", 0)
	fmt.Println("Stock remaining:", stock)

	// ── 3. Cache-Aside Pattern ───────────────────────────
	fmt.Println("\n=== Cache-Aside (GetOrSet) ===")

	var result string
	err = cache.GetOrSet("expensive:query", &result, time.Minute, func() (interface{}, error) {
		fmt.Println("  Computing... (this only runs on miss)")
		time.Sleep(10 * time.Millisecond) // simulate expensive work
		return "computed-result", nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("First call:", result)

	// Second call hits the cache
	err = cache.GetOrSet("expensive:query", &result, time.Minute, func() (interface{}, error) {
		fmt.Println("  This should NOT print")
		return "new-result", nil
	})
	fmt.Println("Second call (cached):", result)

	// ── 4. Bulk Operations ───────────────────────────────
	fmt.Println("\n=== Bulk Operations ===")

	cache.MSet(map[string]cachegrid.Item{
		"color:red":   {Value: "#FF0000", TTL: time.Hour},
		"color:green": {Value: "#00FF00", TTL: time.Hour},
		"color:blue":  {Value: "#0000FF", TTL: time.Hour},
	})

	results := cache.MGet("color:red", "color:green", "color:blue", "color:missing")
	fmt.Printf("MGet returned %d keys (missing keys excluded)\n", len(results))

	// ── 5. Tag-Based Invalidation ────────────────────────
	fmt.Println("\n=== Tag-Based Invalidation ===")

	cache.SetWithTags("user:1:profile", "profile-data", time.Hour, []string{"user:1"})
	cache.SetWithTags("user:1:settings", "settings-data", time.Hour, []string{"user:1"})
	cache.SetWithTags("user:1:avatar", "avatar-url", time.Hour, []string{"user:1"})

	fmt.Println("Before invalidation:", cache.Exists("user:1:profile"), cache.Exists("user:1:settings"))

	cache.InvalidateTag("user:1") // deletes all 3 keys at once

	fmt.Println("After invalidation:", cache.Exists("user:1:profile"), cache.Exists("user:1:settings"))

	// ── 6. Namespaces ────────────────────────────────────
	fmt.Println("\n=== Namespaces ===")

	tenantA := cache.WithNamespace("acme")
	tenantB := cache.WithNamespace("globex")

	tenantA.Set("config", "acme-config", time.Hour)
	tenantB.Set("config", "globex-config", time.Hour)

	var configA, configB string
	tenantA.Get("config", &configA)
	tenantB.Get("config", &configB)
	fmt.Println("Tenant A:", configA)
	fmt.Println("Tenant B:", configB) // isolated from tenant A

	// ── 7. TTL Expiry ────────────────────────────────────
	fmt.Println("\n=== TTL Expiry ===")

	cache.Set("temp", "i-will-expire", 200*time.Millisecond)
	fmt.Println("Before expiry:", cache.Exists("temp"))
	time.Sleep(250 * time.Millisecond)
	fmt.Println("After expiry:", cache.Exists("temp"))

	// ── 8. Disk-Based Cache ──────────────────────────────
	fmt.Println("\n=== Disk-Based Cache (PebbleDB) ===")

	tmpDir, _ := os.MkdirTemp("", "cachegrid-example-*")
	defer os.RemoveAll(tmpDir)

	diskCache, err := cachegrid.NewDisk(tmpDir)
	if err != nil {
		log.Fatal(err)
	}

	diskCache.Set("persistent:key", "this survives restarts", time.Hour)

	var diskVal string
	if diskCache.Get("persistent:key", &diskVal) {
		fmt.Println("Disk Get:", diskVal)
	}

	fmt.Printf("Disk items: %d\n", diskCache.Len())
	diskCache.Shutdown()

	// Re-open the same path to show persistence
	diskCache2, err := cachegrid.NewDisk(tmpDir)
	if err != nil {
		log.Fatal(err)
	}
	defer diskCache2.Shutdown()

	var restored string
	if diskCache2.Get("persistent:key", &restored) {
		fmt.Println("Restored after reopen:", restored)
	} else {
		fmt.Println("Key not found after reopen (TTL may have expired)")
	}

	fmt.Println("\nDone!")
}
