# CacheGrid

A high-performance distributed cache for Go with pluggable storage backends. Embed it as a library or run it as a standalone server.

[![Go Reference](https://pkg.go.dev/badge/github.com/skshohagmiah/cachegrid.svg)](https://pkg.go.dev/github.com/skshohagmiah/cachegrid)
[![Go Report Card](https://goreportcard.com/badge/github.com/skshohagmiah/cachegrid)](https://goreportcard.com/report/github.com/skshohagmiah/cachegrid)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Features

- **Sub-microsecond reads** — 76ns Get, 441ns Set (benchmarked)
- **Pluggable storage** — In-memory (default) or disk-based (PebbleDB)
- **Distributed** — Gossip-based cluster with consistent hashing
- **Three cache modes** — Partitioned, Replicated, NearCache
- **Distributed locks** — With fencing tokens and auto-release
- **Rate limiting** — Token bucket and sliding window algorithms
- **Pub/Sub** — Subscribe to cache events with glob patterns
- **Tag-based invalidation** — Group keys by tags, invalidate in bulk
- **Namespaces** — Multi-tenant key isolation
- **HTTP middleware** — Rate limiting and response caching for `net/http`
- **Standalone server** — REST API with Docker and Kubernetes support
- **Prometheus metrics** — `/metrics` endpoint out of the box

## Install

```bash
go get github.com/skshohagmiah/cachegrid
```

## Quick Start

### In-Memory Cache (Default)

```go
package main

import (
    "fmt"
    "time"

    "github.com/skshohagmiah/cachegrid"
)

func main() {
    // Simplest way — zero config, in-memory, sensible defaults
    cache, _ := cachegrid.NewMemory()
    defer cache.Shutdown()

    cache.Set("user:123", map[string]string{"name": "Alice"}, 5*time.Minute)

    var user map[string]string
    if cache.Get("user:123", &user) {
        fmt.Println(user["name"]) // Alice
    }
}
```

### Disk-Based Cache (PebbleDB)

```go
// Persistent storage — data survives restarts
cache, _ := cachegrid.NewDisk("/var/data/my-cache")
defer cache.Shutdown()

cache.Set("session:abc", sessionData, 24*time.Hour)
```

### Full Configuration

```go
cache, _ := cachegrid.New(cachegrid.Config{
    StorageMode: cachegrid.Memory,   // or cachegrid.Disk
    DiskPath:    "./cache-data",     // required for Disk mode
    NumShards:   256,                // memory mode only, must be power of 2
    MaxMemoryMB: 512,                // memory mode only, 0 = unlimited
    DefaultTTL:  5 * time.Minute,
})
defer cache.Shutdown()
```

### Distributed Cluster

```go
cache, _ := cachegrid.New(cachegrid.Config{
    ListenAddr:  ":7946",
    Peers:       []string{"10.0.0.2:7946", "10.0.0.3:7946"},
    Mode:        cachegrid.Partitioned,
    MaxMemoryMB: 256,
})
defer cache.Shutdown()
```

Nodes discover each other via gossip and automatically partition keys across the cluster.

### Standalone Server

```bash
docker run -d -p 6380:6380 -p 7946:7946 cachegrid

curl -X PUT localhost:6380/cache/user:123 \
  -H "X-TTL: 5m" -d '{"name": "Alice"}'

curl localhost:6380/cache/user:123
```

## Storage Modes

| Mode | Backend | Use Case |
|------|---------|----------|
| **Memory** (default) | Sharded in-memory maps + LRU eviction | Low-latency, ephemeral caching |
| **Disk** | CockroachDB PebbleDB | Persistent cache, large datasets, survives restarts |

Both modes support the full API — Set, Get, Delete, TTL, Incr, tags, locks, pub/sub, etc.

## API Reference

### Cache Operations

```go
// Basic CRUD
cache.Set("key", value, 5*time.Minute)
cache.Get("key", &dest)
cache.Delete("key")
cache.Exists("key")
cache.TTL("key")

// Cache-aside pattern
cache.GetOrSet("key", &dest, ttl, func() (interface{}, error) {
    return fetchFromDB()
})

// Bulk operations
cache.MSet(map[string]cachegrid.Item{
    "a": {Value: 1, TTL: time.Minute},
    "b": {Value: 2, TTL: time.Minute},
})
results := cache.MGet("a", "b")

// Atomic counters
cache.Incr("views", 1)
cache.Decr("stock", 1)
```

### Tags and Namespaces

```go
// Tag-based invalidation
cache.SetWithTags("user:1:profile", profile, ttl, []string{"user:1"})
cache.SetWithTags("user:1:settings", settings, ttl, []string{"user:1"})
cache.InvalidateTag("user:1") // deletes both keys

// Namespaces
tenant := cache.WithNamespace("acme")
tenant.Set("config", val, time.Hour)  // stored as "acme:config"
tenant.Get("config", &dest)
```

### Distributed Locks

```go
lock, err := cache.Lock("order:456", cachegrid.LockOptions{
    TTL:        30 * time.Second,
    RetryCount: 10,
    RetryDelay: 100 * time.Millisecond,
})
if err != nil {
    return err
}
defer lock.Release()

// Extend for long operations
lock.Extend(60 * time.Second)

// Fencing token for safe writes
token := lock.Token()

// Non-blocking attempt
lock, ok := cache.TryLock("resource", 30*time.Second)
```

### Rate Limiting

```go
// Token bucket
allowed, state := cache.RateLimit("api:user:1", cachegrid.RateLimitOptions{
    Limit:  100,
    Window: time.Minute,
})
// state.Remaining, state.Limit, state.ResetsAt

// Sliding window
allowed, state := cache.RateLimitSliding("api:user:1", cachegrid.SlidingWindowOptions{
    Limit:  100,
    Window: time.Minute,
})
```

### Pub/Sub

```go
sub := cache.Subscribe("user:*")
defer sub.Close()

go func() {
    for event := range sub.Events() {
        fmt.Printf("%s %s\n", event.Type, event.Key)
    }
}()

// Event hooks
cache.OnHit(func(key string) { /* metrics */ })
cache.OnMiss(func(key string) { /* metrics */ })
cache.OnEvict(func(key string, value interface{}) { /* cleanup */ })
```

### HTTP Middleware

```go
mux := http.NewServeMux()

// Rate limit by client IP (100 req/min)
handler := cachegrid.HTTPRateLimit(cache, 100, time.Minute)(mux)

// Cache GET responses
handler = cachegrid.HTTPCache(cache, 30*time.Second)(mux)
```

## REST API (Standalone Mode)

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/cache/{key}` | Get value |
| `PUT` | `/cache/{key}` | Set value (`X-TTL`, `X-Tags` headers) |
| `DELETE` | `/cache/{key}` | Delete key |
| `HEAD` | `/cache/{key}` | Check existence |
| `POST` | `/cache/_mget` | Bulk get (`{"keys": [...]}`) |
| `POST` | `/locks/{key}` | Acquire lock (`{"ttl": "30s"}`) |
| `DELETE` | `/locks/{key}` | Release lock (`X-Lock-Token` header) |
| `PUT` | `/locks/{key}` | Extend lock |
| `POST` | `/ratelimit/{key}` | Check rate limit |
| `GET` | `/subscribe/{pattern}` | SSE event stream |
| `GET` | `/health` | Health check |
| `GET` | `/metrics` | Prometheus metrics |
| `GET` | `/cluster/nodes` | Cluster state |

## Configuration

### Embedded

```go
cachegrid.Config{
    // Storage
    StorageMode:     cachegrid.Memory, // Memory (default) or Disk
    DiskPath:        "./data",         // required for Disk mode

    // Memory mode settings
    NumShards:       256,              // must be power of 2
    MaxMemoryMB:     256,              // 0 = unlimited

    // General
    DefaultTTL:      5*time.Minute,    // 0 = no expiry
    SweeperInterval: time.Second,      // expired key cleanup interval

    // Cluster (optional — omit for local-only mode)
    ListenAddr:   ":7946",
    Peers:        []string{"10.0.0.2:7946"},
    Mode:         cachegrid.Partitioned, // or Replicated, NearCache
    NodeName:     "node-1",              // defaults to hostname
    GRPCPort:     7947,                  // inter-node RPC
    HTTPPort:     6380,                  // standalone HTTP API
    VirtualNodes: 150,                   // hash ring granularity
}
```

### Standalone (Environment Variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `CACHEGRID_STORAGE_MODE` | `memory` | `memory` or `disk` |
| `CACHEGRID_DISK_PATH` | `./cachegrid-data` | Disk storage directory |
| `CACHEGRID_SHARDS` | `256` | Number of shards (memory mode) |
| `CACHEGRID_MAX_MEMORY_MB` | `0` | Memory limit (memory mode) |
| `CACHEGRID_DEFAULT_TTL` | `0` | Default TTL (e.g., `5m`) |
| `CACHEGRID_SWEEPER_INTERVAL` | `1s` | Expiry sweep interval |
| `CACHEGRID_NODE_NAME` | hostname | Node identifier |
| `CACHEGRID_LISTEN_ADDR` | `` | Gossip bind address |
| `CACHEGRID_SEEDS` | `` | Comma-separated seed nodes |
| `CACHEGRID_MODE` | `partitioned` | `partitioned`, `replicated`, `nearcache` |
| `CACHEGRID_RPC_PORT` | `7947` | Inter-node RPC port |
| `CACHEGRID_HTTP_PORT` | `6380` | HTTP API port |

## Deployment

### Using Make

```bash
make build          # Build the binary
make test           # Run all tests
make bench          # Run benchmarks
make lint           # Run linter
make docker-build   # Build Docker image
make run            # Run standalone server
make run-disk       # Run with disk storage
```

### Docker Compose

```bash
docker compose up -d   # starts 3-node cluster
```

### Kubernetes

```bash
kubectl apply -f kubernetes.yaml
```

Creates a 3-replica StatefulSet with headless service for gossip discovery.

## Cache Modes

| Mode | Read | Write | Use Case |
|------|------|-------|----------|
| **Partitioned** | From owner | To owner | Large datasets, even distribution |
| **Replicated** | Local (fast) | Fan-out to all | Read-heavy, small datasets |
| **NearCache** | Local first, then owner | Local + owner | Hot keys, read-heavy |

## Benchmarks

```
BenchmarkGetHit-10          15,733,113    75.85 ns/op     69 B/op    3 allocs/op
BenchmarkGetMiss-10         51,526,194    23.16 ns/op     16 B/op    1 allocs/op
BenchmarkSet-10              2,504,900   441.30 ns/op    353 B/op    6 allocs/op
BenchmarkExists-10          17,432,041    68.02 ns/op     15 B/op    1 allocs/op
BenchmarkIncr-10             7,588,982   156.50 ns/op    304 B/op    7 allocs/op
BenchmarkConcurrentRead-10  10,314,302   122.10 ns/op     87 B/op    4 allocs/op
BenchmarkConcurrentMixed-10  8,313,844   147.90 ns/op    138 B/op    5 allocs/op
```

Run locally: `make bench`

## Architecture

For a deep dive into internals, see [ARCHITECTURE.md](ARCHITECTURE.md).

```
┌─────────────────────────────────────────────────────────────┐
│                      Public API (cachegrid)                  │
│  Set · Get · Delete · Lock · RateLimit · Subscribe · ...     │
├─────────────────────────────────────────────────────────────┤
│                     Store Interface                          │
│              MemoryStore  ·  PebbleStore                     │
├──────────────┬──────────────┬───────────────┬───────────────┤
│  internal/   │  internal/   │  internal/    │  internal/    │
│  cache       │  cluster     │  transport    │  lock         │
│  (shards,    │  (hashring,  │  (TCP+msgpack │  (fencing,    │
│   LRU,       │   gossip,    │   RPC)        │   auto-       │
│   eviction)  │   state)     │               │   release)    │
├──────────────┼──────────────┼───────────────┼───────────────┤
│  internal/   │  internal/   │  internal/    │               │
│  ratelimit   │  pubsub      │  server       │               │
│  (token      │  (broker,    │  (HTTP REST,  │               │
│   bucket,    │   events,    │   SSE,        │               │
│   sliding    │   hooks)     │   metrics)    │               │
│   window)    │              │               │               │
└──────────────┴──────────────┴───────────────┴───────────────┘
```

## Project Structure

```
cachegrid/
├── cache.go, config.go, errors.go      # Core cache + configuration
├── store.go                            # Store interface + StorageMode
├── store_memory.go                     # In-memory backend (sharded maps + LRU)
├── store_pebble.go                     # Disk backend (PebbleDB)
├── distributed.go                      # Cluster routing logic
├── lock.go, ratelimit.go              # Distributed locks + rate limiting
├── pubsub.go, tags.go, namespace.go   # Events, tags, namespaces
├── middleware.go                       # HTTP middleware helpers
├── *_test.go                          # Tests across all features
├── internal/
│   ├── cache/       # Sharded store, LRU eviction, serialization
│   ├── cluster/     # Hash ring, gossip membership, cluster state
│   ├── transport/   # TCP+msgpack inter-node RPC
│   ├── lock/        # Lock engine with fencing tokens
│   ├── ratelimit/   # Token bucket + sliding window
│   ├── pubsub/      # Event broker + subscriptions
│   └── server/      # HTTP server, handlers, middleware
├── cmd/cachegrid/   # Standalone server binary
├── Makefile
├── Dockerfile
├── docker-compose.yml
└── kubernetes.yaml
```

## Examples

See the [examples/](examples/) directory for runnable apps:

| Example | Description |
|---------|-------------|
| [basic/](examples/basic/) | Core features: CRUD, TTL, counters, tags, namespaces, disk persistence |
| [webapi/](examples/webapi/) | Bookstore REST API with response caching, rate limiting, distributed locks |
| [sessions/](examples/sessions/) | Persistent session store using disk mode (survives restarts) |
| [pubsub/](examples/pubsub/) | Real-time cache event monitoring with pattern subscriptions |

```bash
go run ./examples/basic    # try it out
go run ./examples/webapi   # then curl localhost:8080/books
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License. See [LICENSE](LICENSE) for details.
