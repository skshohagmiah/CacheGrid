# CacheGrid — Distributed Application Cache for Go

### Embedded + Standalone | Zero Dependencies | Gossip-Based | Open Source

---

## What is CacheGrid?

CacheGrid is a **distributed cache** that works two ways:

1. **Embedded** — import as a Go library, runs inside your app process (~200ns reads)
2. **Standalone** — run as a Docker container with HTTP/gRPC API, works with any language

Your app instances automatically discover each other and keep caches synchronized using gossip protocol. No Redis. No Memcached. No external infrastructure.

```go
// Mode 1: Embedded (Go apps — fastest)
cache := cachegrid.New(cachegrid.Config{
    ListenAddr: ":7946",
    Peers:      []string{"10.0.0.2:7946", "10.0.0.3:7946"},
})
cache.Set("user:123", userData, 5*time.Minute)
val, ok := cache.Get("user:123")
```

```bash
# Mode 2: Standalone (any language — Docker)
docker run -d \
  -p 6380:6380 \
  -p 7946:7946 \
  -e PEERS=10.0.0.2:7946,10.0.0.3:7946 \
  cachegrid/cachegrid

# Any language can use it via HTTP
curl -X PUT localhost:6380/cache/user:123 \
  -d '{"name": "Shohag"}' -H "X-TTL: 300"

curl localhost:6380/cache/user:123
```

---

## Two Modes, One Codebase

```
Mode 1: Embedded (Go apps)              Mode 2: Standalone (any language)
┌──────────────────────┐                ┌──────────────────────┐
│  Your Go App         │                │  CacheGrid Server    │
│  ┌────────────────┐  │                │  ┌────────────────┐  │
│  │  CacheGrid lib │  │                │  │  CacheGrid lib │  │
│  │  (in-process)  │  │                │  │  + HTTP API    │  │
│  └────────────────┘  │                │  └────────────────┘  │
└──────────────────────┘                └──────────────────────┘
  cache.Get("key")                       GET /cache/key
  ~200ns                                 ~1-2ms
```

---

## Feature Overview

### Core Cache
- Get / Set / Delete / Exists / TTL
- GetOrSet (cache-aside pattern)
- MGet / MSet (bulk operations)
- TTL with background expiry
- LRU / LFU / Random eviction
- Memory limits per node
- Tag-based group invalidation
- Namespace support (multi-tenant)

### Distributed
- Consistent hashing (virtual nodes)
- Gossip-based membership (SWIM)
- Automatic node discovery
- Key rebalancing on topology change
- Three cache modes: Partitioned, Replicated, NearCache

### Pub/Sub (Cache Events)
- Subscribe to key changes across the cluster
- Cache invalidation broadcasts
- Event hooks (OnHit, OnMiss, OnEvict, OnSet, OnDelete)

### Distributed Locks
- Acquire / Release / Extend
- Auto-release on timeout (no deadlocks)
- Fencing tokens for safety

### Rate Limiting
- Token bucket / Sliding window
- Distributed counters (consistent across nodes)
- Per-key rate limits

### Standalone Mode
- HTTP REST API
- Redis-compatible protocol (RESP — future)
- Docker image + Docker Compose + Kubernetes
- Web dashboard
- Health checks + Prometheus metrics

---

## Architecture

### Embedded Mode (Go Apps)

```
┌─────────────────────┐     ┌─────────────────────┐     ┌─────────────────────┐
│   API Server #1     │     │   API Server #2     │     │   API Server #3     │
│  ┌───────────────┐  │     │  ┌───────────────┐  │     │  ┌───────────────┐  │
│  │  CacheGrid    │◄─┼─────┼──│  CacheGrid    │◄─┼─────┼──│  CacheGrid    │  │
│  │  (in-process) │──┼─────┼─▶│  (in-process) │──┼─────┼─▶│  (in-process) │  │
│  │  Keys: A,D,G  │  │     │  │  Keys: B,E,H  │  │     │  │  Keys: C,F,I  │  │
│  └───────────────┘  │     │  └───────────────┘  │     │  └───────────────┘  │
│  Your app code      │     │  Your app code      │     │  Your app code      │
└─────────────────────┘     └─────────────────────┘     └─────────────────────┘
         ▲                           ▲                           ▲
         └───────────── Gossip (membership + events) ────────────┘
```

### Standalone Mode (Any Language)

```
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│  Next.js App │    │  Python API  │    │  Go Service  │
└──────┬───────┘    └──────┬───────┘    └──────┬───────┘
       │ HTTP              │ HTTP              │ HTTP
       ▼                   ▼                   ▼
┌─────────────────────────────────────────────────────────┐
│              CacheGrid Cluster (Docker)                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Node 1      │  │  Node 2      │  │  Node 3      │  │
│  │  :6380 HTTP  │◄─│  :6380 HTTP  │◄─│  :6380 HTTP  │  │
│  │  :7946 gossip│─▶│  :7946 gossip│─▶│  :7946 gossip│  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### Mixed Mode (Go embedded + Docker standalone in same cluster)

```
┌─────────────────────┐    ┌─────────────────────┐    ┌──────────────────┐
│  Go API (embedded)  │    │  Go API (embedded)  │    │ CacheGrid Docker │
│  ┌───────────────┐  │    │  ┌───────────────┐  │    │ (standalone)     │
│  │  CacheGrid    │◄─┼────┼──│  CacheGrid    │◄─┼────│ HTTP API :6380  │
│  │  node         │──┼────┼─▶│  node         │──┼───▶│                  │
│  └───────────────┘  │    │  └───────────────┘  │    └──────────────────┘
└─────────────────────┘    └─────────────────────┘             ▲
                                                               │ HTTP
                                                    ┌──────────┴───────┐
                                                    │  Next.js / Python │
                                                    └──────────────────┘
```

---

## Embedded Go API

### Basic Cache

```go
cache, _ := cachegrid.New(cachegrid.Config{
    ListenAddr:  ":7946",
    Peers:       []string{"10.0.0.2:7946"},
    MaxMemoryMB: 256,
    Mode:        cachegrid.Partitioned,
})
defer cache.Shutdown()

// Set / Get / Delete
cache.Set("user:123", User{Name: "Shohag"}, 5*time.Minute)
var user User
cache.Get("user:123", &user)
cache.Delete("user:123")

// Cache-aside (most common pattern)
cache.GetOrSet("user:123", &user, 5*time.Minute, func() (interface{}, error) {
    return db.GetUser(123) // only on cache miss
})

// Bulk
cache.MSet(map[string]cachegrid.Item{
    "user:1": {Value: u1, TTL: 5 * time.Minute},
    "user:2": {Value: u2, TTL: 5 * time.Minute},
})
results := cache.MGet("user:1", "user:2")

// Tags
cache.SetWithTags("user:123:profile", profile, 10*time.Minute, []string{"user:123"})
cache.SetWithTags("user:123:settings", settings, 10*time.Minute, []string{"user:123"})
cache.InvalidateTag("user:123") // deletes both

// Namespace
tenant := cache.WithNamespace("tenant:acme")
tenant.Set("config", config, 1*time.Hour)

// Counters
cache.Incr("views:homepage", 1)
cache.Decr("stock:sku-789", 1)
```

### Distributed Locks

```go
// Basic lock
lock, err := cache.Lock("resource:order:456", cachegrid.LockOptions{
    TTL:        30 * time.Second,
    RetryDelay: 100 * time.Millisecond,
    RetryCount: 50,
})
if err != nil {
    return // could not acquire
}
defer lock.Release()
processOrder(456) // only one node runs this

// Extend for long operations
lock.Extend(60 * time.Second)

// Non-blocking try
lock, acquired := cache.TryLock("report:gen", 30*time.Second)
if !acquired {
    return getCachedReport()
}
defer lock.Release()

// Fencing token (prevents stale writes)
token := lock.Token()
db.Update("row", newData, token) // DB rejects if token is old
```

### Rate Limiting

```go
// Token bucket
allowed, state := cache.RateLimit("api:user:123", cachegrid.RateLimitOptions{
    Limit:  100,
    Window: 1 * time.Minute,
})
if !allowed {
    // state.Remaining = 0, state.ResetsAt = ...
    http.Error(w, "rate limited", 429)
    return
}

// Sliding window
allowed, state := cache.RateLimitSliding("api:user:123", cachegrid.SlidingWindowOptions{
    Limit:  100,
    Window: 1 * time.Minute,
})

// HTTP middleware
r := chi.NewRouter()
r.Use(cachegrid.HTTPRateLimit(cache, 100, time.Minute)) // 100 req/min per IP
```

### Pub/Sub

```go
// Subscribe to key changes
sub := cache.Subscribe("user:*")
defer sub.Close()

go func() {
    for event := range sub.Events() {
        switch event.Type {
        case cachegrid.EventSet:
            log.Info().Str("key", event.Key).Msg("set")
        case cachegrid.EventDelete:
            log.Info().Str("key", event.Key).Msg("deleted")
        case cachegrid.EventExpire:
            log.Info().Str("key", event.Key).Msg("expired")
        }
    }
}()

// Event hooks (simpler)
cache.OnHit(func(key string) { metrics.CacheHits.Inc() })
cache.OnMiss(func(key string) { metrics.CacheMisses.Inc() })
cache.OnEvict(func(key string, value interface{}) { log.Debug().Msg("evicted") })
```

### HTTP Middleware Helpers

```go
// Cache HTTP responses
r.With(cachegrid.HTTPCache(cache, 30*time.Second)).Get("/api/dashboard", handler)

// Rate limiting
r.Use(cachegrid.HTTPRateLimit(cache, 100, time.Minute))

// Session storage
sessions := cachegrid.NewSessionStore(cache, cachegrid.SessionConfig{
    TTL: 24 * time.Hour, CookieName: "session_id",
})
r.Use(sessions.Middleware())
```

---

## Standalone HTTP API

Base URL: `http://localhost:6380`

### Cache

```
GET    /cache/:key                    Get value
PUT    /cache/:key                    Set value (X-TTL header, X-Tags header)
DELETE /cache/:key                    Delete key
HEAD   /cache/:key                    Exists check (204/404)
GET    /cache/:key/ttl               TTL remaining
POST   /cache/_mget                   Bulk get
POST   /cache/_mset                   Bulk set
POST   /cache/_invalidate-tag         Invalidate by tag
```

### Locks

```
POST   /locks/:key                    Acquire (body: {"ttl": 30})
DELETE /locks/:key/:token             Release
PUT    /locks/:key/:token/extend      Extend (body: {"ttl": 30})
GET    /locks                         List active locks
```

### Rate Limiting

```
POST   /ratelimit/:key                Check (body: {"limit": 100, "window": 60})
```

### Pub/Sub

```
GET    /subscribe/:pattern            SSE event stream
POST   /publish/:channel              Publish event
```

### Cluster

```
GET    /cluster/nodes                  List nodes
GET    /cluster/ring                   Hash ring state
GET    /cluster/stats                  Cluster stats
GET    /health                         Health check
GET    /metrics                        Prometheus metrics
```

### Examples

```bash
# Set
curl -X PUT localhost:6380/cache/user:123 \
  -H "X-TTL: 300" -H "X-Tags: user:123" \
  -d '{"name": "Shohag"}'

# Get
curl localhost:6380/cache/user:123

# Lock
curl -X POST localhost:6380/locks/order:456 -d '{"ttl": 30}'
# → {"token": "abc123", "acquired": true}

# Rate limit
curl -X POST localhost:6380/ratelimit/api:user:789 -d '{"limit":100,"window":60}'
# → {"allowed": true, "remaining": 99}

# Subscribe (SSE)
curl localhost:6380/subscribe/user:*

# Cluster
curl localhost:6380/cluster/nodes
```

---

## Docker & Kubernetes

### Docker Compose (3-Node Cluster)

```yaml
version: '3.8'
services:
  cachegrid-1:
    image: cachegrid/cachegrid:latest
    environment:
      - CACHEGRID_NODE_NAME=node-1
      - CACHEGRID_PEERS=cachegrid-2:7946,cachegrid-3:7946
      - CACHEGRID_MAX_MEMORY_MB=256
    ports:
      - "6380:6380"
      - "7946:7946"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:6380/health"]
      interval: 10s

  cachegrid-2:
    image: cachegrid/cachegrid:latest
    environment:
      - CACHEGRID_NODE_NAME=node-2
      - CACHEGRID_PEERS=cachegrid-1:7946,cachegrid-3:7946
      - CACHEGRID_MAX_MEMORY_MB=256
    ports:
      - "6381:6380"

  cachegrid-3:
    image: cachegrid/cachegrid:latest
    environment:
      - CACHEGRID_NODE_NAME=node-3
      - CACHEGRID_PEERS=cachegrid-1:7946,cachegrid-2:7946
      - CACHEGRID_MAX_MEMORY_MB=256
    ports:
      - "6382:6380"
```

### Kubernetes StatefulSet

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: cachegrid
spec:
  serviceName: cachegrid
  replicas: 3
  selector:
    matchLabels:
      app: cachegrid
  template:
    metadata:
      labels:
        app: cachegrid
    spec:
      containers:
        - name: cachegrid
          image: cachegrid/cachegrid:latest
          ports:
            - containerPort: 6380
            - containerPort: 7946
            - containerPort: 7947
          env:
            - name: CACHEGRID_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: CACHEGRID_PEERS
              value: "cachegrid-0.cachegrid:7946,cachegrid-1.cachegrid:7946,cachegrid-2.cachegrid:7946"
            - name: CACHEGRID_MAX_MEMORY_MB
              value: "512"
          readinessProbe:
            httpGet:
              path: /health
              port: 6380
---
apiVersion: v1
kind: Service
metadata:
  name: cachegrid
spec:
  clusterIP: None
  selector:
    app: cachegrid
  ports:
    - name: http
      port: 6380
    - name: gossip
      port: 7946
```

---

## Performance Targets

```
┌────────────────────────┬───────────┬────────────┬───────────────┐
│ Operation              │ CacheGrid │ Redis      │ Improvement   │
├────────────────────────┼───────────┼────────────┼───────────────┤
│ Local Get              │ ~200ns    │ ~100μs     │ 500x faster   │
│ Local Set              │ ~500ns    │ ~100μs     │ 200x faster   │
│ Remote Get             │ ~1-2ms    │ ~1-5ms     │ comparable    │
│ Throughput (per node)  │ >500K/s   │ ~100K/s    │ 5x            │
│ Memory overhead/key    │ ~100B     │ ~100B      │ same          │
│ Cluster join           │ <3s       │ N/A        │ automatic     │
│ External dependencies  │ 0         │ 1 (Redis)  │ simpler       │
└────────────────────────┴───────────┴────────────┴───────────────┘
```

---

## Development Phases

### Phase 1 — Local Cache (Week 1)
- [ ] Sharded in-memory store with LRU eviction
- [ ] Get / Set / Delete / Exists / TTL / GetOrSet / MGet / MSet
- [ ] Msgpack serialization + benchmarks

### Phase 2 — Hash Ring + Gossip (Week 2-3)
- [ ] Consistent hash ring with virtual nodes
- [ ] memberlist gossip integration
- [ ] Node join/leave/failure detection

### Phase 3 — Distributed Operations (Week 4)
- [ ] gRPC transport + connection pooling
- [ ] Remote Get / Set / Delete
- [ ] Key rebalancing + near-cache mode

### Phase 4 — Locks + Rate Limiting (Week 5)
- [ ] Distributed locks (acquire/release/extend/fencing)
- [ ] Token bucket + sliding window rate limiter
- [ ] Atomic counters

### Phase 5 — Pub/Sub + Tags (Week 6)
- [ ] Event broker + subscriptions
- [ ] Tag-based invalidation
- [ ] Namespace support + event hooks

### Phase 6 — Standalone Mode (Week 7-8)
- [ ] HTTP REST API + CLI
- [ ] Docker image + Docker Compose
- [ ] Kubernetes manifests
- [ ] Web dashboard + Prometheus metrics

### Phase 7 — Polish + Launch (Week 9-10)
- [ ] Tests + benchmarks + examples
- [ ] Docs + README + blog post
- [ ] GitHub release + awesome-go submission

### Phase 8 — Future
- [ ] Redis protocol (RESP) compatibility
- [ ] Disk persistence (optional)
- [ ] Official client libs (Node.js, Python)
- [ ] TLS encryption
- [ ] Grafana dashboard template

---

## How Seentics Uses CacheGrid

```go
// Cache ClickHouse queries (200ms → 0.2ms)
cache.GetOrSet("dashboard:"+siteID, &stats, 30*time.Second, func() (interface{}, error) {
    return clickhouse.QueryDashboard(siteID)
})

// Rate limit API
r.Use(cachegrid.HTTPRateLimit(cache, 100, time.Minute))

// Lock for report generation
lock, _ := cache.Lock("report:"+siteID, cachegrid.LockOptions{TTL: 60*time.Second})
defer lock.Release()

// Invalidate on settings change
cache.InvalidateTag("site:" + siteID)

// Next.js dashboard uses standalone HTTP API
// GET http://cachegrid:6380/cache/dashboard:site_123
```

One tool replaces Redis for caching, rate limiting, and distributed locks.

---

*Build Phase 1 first. A great local cache with a clean API is already valuable. Each phase adds distributed superpowers on top.*
