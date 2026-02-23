// Example: webapi
//
// A REST API for a bookstore that uses CacheGrid for:
// - Response caching (GET requests cached for 30s)
// - Rate limiting (100 requests/minute per IP)
// - Manual cache management (invalidate on writes)
// - Distributed locks (prevent double-purchases)
//
// Run: go run ./examples/webapi
//
// Try it:
//   curl localhost:8080/books
//   curl localhost:8080/books/1
//   curl -X POST localhost:8080/books -d '{"title":"New Book","author":"Author","price":19.99}'
//   curl -X POST localhost:8080/books/1/purchase
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/skshohagmiah/cachegrid"
)

// Book represents a book in our store.
type Book struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Author string  `json:"author"`
	Price  float64 `json:"price"`
	Stock  int     `json:"stock"`
}

// In-memory "database" for the example.
var (
	bookDB = map[string]Book{
		"1": {ID: "1", Title: "The Go Programming Language", Author: "Donovan & Kernighan", Price: 34.99, Stock: 10},
		"2": {ID: "2", Title: "Designing Data-Intensive Applications", Author: "Martin Kleppmann", Price: 42.99, Stock: 5},
		"3": {ID: "3", Title: "Clean Code", Author: "Robert C. Martin", Price: 29.99, Stock: 8},
	}
	dbMu   sync.RWMutex
	nextID = 4
)

var cache *cachegrid.Cache

func main() {
	var err error
	cache, err = cachegrid.New(cachegrid.Config{
		NumShards:       64,
		MaxMemoryMB:     128,
		DefaultTTL:      5 * time.Minute,
		SweeperInterval: time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Shutdown()

	// Log cache events
	cache.OnHit(func(key string) {
		log.Printf("CACHE HIT: %s", key)
	})
	cache.OnMiss(func(key string) {
		log.Printf("CACHE MISS: %s", key)
	})

	mux := http.NewServeMux()

	// Routes
	mux.HandleFunc("GET /books", handleListBooks)
	mux.HandleFunc("GET /books/{id}", handleGetBook)
	mux.HandleFunc("POST /books", handleCreateBook)
	mux.HandleFunc("POST /books/{id}/purchase", handlePurchase)
	mux.HandleFunc("GET /stats", handleStats)

	// Wrap with CacheGrid middleware
	// 1. Rate limit: 100 requests per minute per IP
	handler := cachegrid.HTTPRateLimit(cache, 100, time.Minute)(mux)

	// 2. Response cache: cache GET responses for 30 seconds
	handler = cachegrid.HTTPCache(cache, 30*time.Second)(handler)

	addr := ":8080"
	log.Printf("Bookstore API running at http://localhost%s", addr)
	log.Printf("  GET  /books           - List all books")
	log.Printf("  GET  /books/{id}      - Get a book")
	log.Printf("  POST /books           - Create a book")
	log.Printf("  POST /books/{id}/purchase - Purchase a book (uses distributed lock)")
	log.Printf("  GET  /stats           - Cache statistics")
	log.Fatal(http.ListenAndServe(addr, handler))
}

func handleListBooks(w http.ResponseWriter, r *http.Request) {
	// Try cache first
	var books []Book
	if cache.Get("books:list", &books) {
		writeJSON(w, books)
		return
	}

	// Cache miss — load from "DB"
	dbMu.RLock()
	books = make([]Book, 0, len(bookDB))
	for _, b := range bookDB {
		books = append(books, b)
	}
	dbMu.RUnlock()

	// Cache for next time
	cache.SetWithTags("books:list", books, 30*time.Second, []string{"books"})

	writeJSON(w, books)
}

func handleGetBook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cacheKey := "book:" + id

	// Try cache
	var book Book
	if cache.Get(cacheKey, &book) {
		writeJSON(w, book)
		return
	}

	// Load from DB
	dbMu.RLock()
	book, ok := bookDB[id]
	dbMu.RUnlock()
	if !ok {
		http.Error(w, `{"error":"book not found"}`, http.StatusNotFound)
		return
	}

	cache.SetWithTags(cacheKey, book, time.Minute, []string{"books", "book:" + id})
	writeJSON(w, book)
}

func handleCreateBook(w http.ResponseWriter, r *http.Request) {
	var book Book
	if err := json.NewDecoder(r.Body).Decode(&book); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	dbMu.Lock()
	book.ID = fmt.Sprintf("%d", nextID)
	nextID++
	book.Stock = 10
	bookDB[book.ID] = book
	dbMu.Unlock()

	// Invalidate list cache since we added a book
	cache.InvalidateTag("books")

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, book)
}

func handlePurchase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Use a distributed lock to prevent double-purchases / race conditions
	lock, err := cache.Lock("purchase:"+id, cachegrid.LockOptions{
		TTL:        5 * time.Second,
		RetryCount: 3,
		RetryDelay: 100 * time.Millisecond,
	})
	if err != nil {
		http.Error(w, `{"error":"could not acquire lock, try again"}`, http.StatusConflict)
		return
	}
	defer lock.Release()

	// Check stock inside the lock
	dbMu.Lock()
	book, ok := bookDB[id]
	if !ok {
		dbMu.Unlock()
		http.Error(w, `{"error":"book not found"}`, http.StatusNotFound)
		return
	}
	if book.Stock <= 0 {
		dbMu.Unlock()
		http.Error(w, `{"error":"out of stock"}`, http.StatusConflict)
		return
	}
	book.Stock--
	bookDB[id] = book
	dbMu.Unlock()

	// Invalidate caches for this book
	cache.InvalidateTag("book:" + id)
	cache.InvalidateTag("books")

	writeJSON(w, map[string]interface{}{
		"message":        "Purchase successful!",
		"book":           book.Title,
		"stock_remaining": book.Stock,
		"fencing_token":  lock.Token(),
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"cached_items": cache.Len(),
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
