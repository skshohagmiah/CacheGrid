# CacheGrid Architecture

This document provides a deep dive into how CacheGrid works internally — the storage layer, distribution logic, subsystems, and all public APIs.

## Table of Contents

- [Overview](#overview)
- [Storage Layer](#storage-layer)
  - [Store Interface](#store-interface)
  - [MemoryStore](#memorystore)
  - [PebbleStore](#pebblestore)
- [Core Cache](#core-cache)
  - [Cache Struct](#cache-struct)
  - [Request Flow](#request-flow)
  - [Serialization](#serialization)
  - [TTL and Expiry](#ttl-and-expiry)
- [Distribution Layer](#distribution-layer)
  - [Cache Modes](#cache-modes)
  - [Consistent Hash Ring](#consistent-hash-ring)
  - [Gossip Membership](#gossip-membership)
  - [Inter-Node Transport](#inter-node-transport)
- [Subsystems](#subsystems)
  - [Distributed Locks](#distributed-locks)
  - [Rate Limiting](#rate-limiting)
  - [Pub/Sub](#pubsub)
  - [Tag-Based Invalidation](#tag-based-invalidation)
  - [Namespaces](#namespaces)
  - [HTTP Middleware](#http-middleware)
- [Standalone Server](#standalone-server)
- [Public API Reference](#public-api-reference)
- [Configuration Reference](#configuration-reference)
- [Error Types](#error-types)

---

## Overview

CacheGrid is organized in layers:

```
┌─────────────────────────────────────────────────┐
│           Public API  (cachegrid package)        │
│   Cache · NamespacedCache · LockHandle · ...     │
├─────────────────────────────────────────────────┤
│           Store Interface (pluggable)            │
│         MemoryStore  ·  PebbleStore              │
├──────────┬──────────┬──────────┬────────────────┤
│ internal │ internal │ internal │ internal/lock   │
│ /cache   │ /cluster │/transport│ internal/       │
│          │          │          │  ratelimit      │
│          │          │          │ internal/pubsub │
├──────────┴──────────┴──────────┴────────────────┤
│          internal/server  (HTTP REST)            │
└─────────────────────────────────────────────────┘
```

Key design principles:
- **Pluggable storage** — The `Store` interface decouples the cache logic from the storage backend
- **Sharded concurrency** — Memory backend uses power-of-2 shards with per-shard mutexes for minimal lock contention
- **Gossip-based clustering** — No coordinator needed; nodes discover and manage membership via hashicorp/memberlist
- **Subsystem independence** — Locks, rate limiters, and pub/sub are separate engines from the key-value store

---

## Storage Layer

### Store Interface

Defined in `store.go`. This is the core abstraction that allows swapping between in-memory and disk storage:

```go
type Store interface {
    Get(key string) ([]byte, bool)
    Set(key string, value []byte, ttl time.Duration)
    Delete(key string) bool
    Exists(key string) bool
    TTL(key string) time.Duration
    Incr(key string, delta int64, defaultTTL time.Duration) (int64, error)
    DeleteExpired() int
    Len() int
    Close() error
    SetOnEvict(fn func(key string, value []byte))
    SetOnExpire(fn func(key string, value []byte))
}
```

All values flowing through the Store are already serialized as `[]byte` using msgpack. The Cache layer handles serialization/deserialization before/after calling the Store.

Two storage modes are available:

| Constant | Value | Backend |
|----------|-------|---------|
| `Memory` | `0` | In-memory sharded maps (default) |
| `Disk` | `1` | CockroachDB PebbleDB |

### MemoryStore

**File:** `store_memory.go`

Wraps the existing sharded in-memory implementation.

**Architecture:**

```
MemoryStore
├── shards[0]  ─── map[string]*Entry + LRUList + RWMutex
├── shards[1]  ─── map[string]*Entry + LRUList + RWMutex
├── ...
└── shards[N-1] ── map[string]*Entry + LRUList + RWMutex
```

**Key routing:** FNV-32a hash of the key, bitmasked with `numShards - 1`:

```go
h := fnv.New32a()
h.Write([]byte(key))
shard := shards[h.Sum32() & mask]
```

This gives O(1) shard selection with uniform distribution.

**Per-shard components:**
- `map[string]*Entry` — The actual key-value store
- `*LRUList` — Doubly-linked list (via `container/list`) tracking access order
- `sync.RWMutex` — Per-shard locking for concurrent access
- `Size` / `MaxSize` — Current and maximum byte sizes for memory limit enforcement

**Entry structure** (defined in `internal/cache/entry.go`):

```go
type Entry struct {
    Key       string
    Value     []byte         // msgpack-serialized
    ExpiresAt time.Time      // zero means no expiry
    Size      int64          // estimated memory footprint
    Element   *list.Element  // pointer into LRU list
}
```

**Memory estimation:** Each entry's size is estimated as:
```
len(key) + len(value) + 184 bytes overhead
```
The 184 bytes accounts for the Entry struct, string/slice headers, and list element.

**LRU eviction:** When a shard's `Size` exceeds `MaxSize` after a Set, the oldest entries are removed from the tail of the LRU list until the shard fits within its budget. The `OnEvict` callback fires for each evicted entry.

**Expiry:** Entries with a non-zero `ExpiresAt` are lazily expired on read (Get/Exists/TTL) and proactively removed by the background sweeper via `DeleteExpired()`.

### PebbleStore

**File:** `store_pebble.go`

Backed by [CockroachDB's Pebble](https://github.com/cockroachdb/pebble), an LSM-tree key-value store inspired by RocksDB.

**On-disk encoding:**

```
┌──────────────────────┬──────────────────┐
│  8 bytes             │  N bytes         │
│  expiresAt (nanos)   │  value (raw)     │
│  big-endian uint64   │                  │
└──────────────────────┴──────────────────┘
```

- `expiresAt = 0` means no expiration
- The value portion is the already-serialized msgpack bytes

**Key characteristics:**
- **No sharding needed** — Pebble handles its own internal concurrency via LSM-tree structure
- **No LRU eviction** — Disk is effectively unbounded; the `OnEvict` callback is never fired
- **Lazy expiry** — Expired entries are cleaned up on read (Get, Exists, TTL) and by the periodic sweeper
- **Atomic counters** — `Incr()` uses a `sync.Mutex` for read-modify-write safety
- **Item counting** — Maintained via `atomic.Int64`; incremented on Set (new key), decremented on Delete
- **Batch deletes** — `DeleteExpired()` uses `pebble.Batch` for efficient bulk removal

**Copy safety:** Pebble returns byte slices that are only valid until `closer.Close()`. All values are copied to owned memory before the closer is released.

---

## Core Cache

### Cache Struct

**File:** `cache.go`

```go
type Cache struct {
    store  Store              // pluggable storage backend
    config Config
    closed atomic.Bool
    done   chan struct{}       // signals shutdown to goroutines

    // Cluster (nil in local-only mode)
    membership *cluster.Membership
    ring       *cluster.Ring
    state      *cluster.ClusterState
    transport  transport.Transport

    // Subsystems (always initialized)
    lockEngine    *lock.Engine
    tokenBucket   *ratelimit.TokenBucket
    slidingWindow *ratelimit.SlidingWindow
    broker        *pubsub.Broker
    tags          *tagIndex
}
```

### Request Flow

A typical `Set` operation:

```
cache.Set("key", value, ttl)
    │
    ├── Validate key (non-empty, not shutdown)
    ├── cache.Serialize(value) → []byte via msgpack
    ├── Apply DefaultTTL if ttl == 0
    │
    ├── distributedSet(key, data, ttl, tags)
    │   ├── Partitioned: ownerAddr(key) → remote RPC or local store.Set
    │   ├── Replicated:  local store.Set + fan-out RPC to all nodes
    │   └── NearCache:   local store.Set + RPC to owner
    │
    └── broker.Publish(EventSet) + broker.FireSet callbacks
```

A typical `Get` operation:

```
cache.Get("key", &dest)
    │
    ├── Validate key (non-empty, not shutdown)
    │
    ├── distributedGet(key) → ([]byte, bool)
    │   ├── Partitioned: ownerAddr(key) → remote RPC or local store.Get
    │   ├── Replicated:  always local store.Get
    │   └── NearCache:   local first, fallback to owner via RPC
    │
    ├── cache.Deserialize(data, &dest) via msgpack
    └── broker.FireHit or broker.FireMiss
```

### Serialization

**File:** `internal/cache/serializer.go`

Uses `github.com/vmihailenco/msgpack/v5` for compact binary serialization.

```go
func Serialize(v interface{}) ([]byte, error)    // any Go type → []byte
func Deserialize(data []byte, v interface{}) error // []byte → Go type
```

Special case: if the value is already `[]byte`, it is passed through without encoding.

### TTL and Expiry

Two mechanisms handle expired entries:

1. **Lazy expiry** — When `Get`, `Exists`, or `TTL` encounters an expired entry, it deletes it immediately and returns a miss. This guarantees clients never see stale data.

2. **Background sweeper** — A goroutine runs on a configurable interval (`SweeperInterval`, default 1s) and calls `store.DeleteExpired()` to proactively clean up expired entries. This prevents memory/disk from growing unbounded with entries that are never read.

```go
func (c *Cache) startSweeper() {
    ticker := time.NewTicker(c.config.SweeperInterval)
    for {
        select {
        case <-c.done: return
        case <-ticker.C: c.store.DeleteExpired()
        }
    }
}
```

---

## Distribution Layer

### Cache Modes

**File:** `distributed.go`

Three distribution strategies control how keys are routed across cluster nodes:

**Partitioned** (default):
- Each key has one owner determined by the hash ring
- Reads and writes go to the owner node
- Best for: large datasets, even distribution

**Replicated**:
- All nodes hold all data
- Writes fan out to all live nodes (async)
- Reads are always local (fast)
- Best for: read-heavy workloads with small datasets

**NearCache**:
- Hybrid: writes go to both local and owner
- Reads try local first, fall back to owner on miss
- Local copy cached on first read from owner
- Best for: hot keys in read-heavy workloads

### Consistent Hash Ring

**File:** `internal/cluster/hashring.go`

```go
type Ring struct {
    mu           sync.RWMutex
    hashes       []uint32           // sorted hash values
    ring         map[uint32]string  // hash → node name
    virtualNodes int                // default: 150
}
```

**Algorithm:**
- Each physical node gets `virtualNodes` positions on the ring (150 by default)
- Positions are FNV-32a hashes of `"nodeName-0"`, `"nodeName-1"`, etc.
- Key lookup: hash the key, binary search the sorted hash list, return the next node clockwise (wrapping around)
- `GetNode(key)` returns the single owner
- `GetNodes(key, n)` returns `n` distinct nodes (used for replication)

**Virtual nodes** ensure even key distribution. With 150 virtual nodes per physical node, a 3-node cluster has ~450 ring positions, giving well-balanced partitioning.

### Gossip Membership

**File:** `internal/cluster/member.go`

Uses [hashicorp/memberlist](https://github.com/hashicorp/memberlist) for decentralized cluster membership.

**How it works:**
1. Node starts and binds to `ListenAddr` (e.g., `:7946`)
2. Joins the cluster by contacting seed nodes (`Peers`)
3. Memberlist propagates join/leave events via gossip protocol
4. On join: node is added to the hash ring and cluster state
5. On leave/failure: node is removed, connections are closed

**Metadata:** Each node broadcasts its RPC port, HTTP port, and version via memberlist's metadata mechanism.

**Event flow:**
```
memberlist.NodeJoin → ClusterState.AddNode + Ring.AddNode + Cache.OnNodeJoin
memberlist.NodeLeave → ClusterState.RemoveNode + Ring.RemoveNode + Cache.OnNodeLeave
```

### Inter-Node Transport

**File:** `internal/transport/rpc.go`

A custom TCP+msgpack RPC implementation for inter-node communication.

**Interface:**

```go
type Transport interface {
    RemoteGet(ctx, addr, key) ([]byte, bool, error)
    RemoteSet(ctx, addr, key, value, ttl, tags) error
    RemoteDelete(ctx, addr, key) error
    RemoteExists(ctx, addr, key) (bool, error)
    RemoteMGet(ctx, addr, keys) (map[string][]byte, error)
    RemoteIncr(ctx, addr, key, delta) (int64, error)
    RemoteLockAcquire(ctx, addr, key, ttl) (token, fencing, acquired, error)
    RemoteLockRelease(ctx, addr, key, token) (bool, error)
    RemoteLockExtend(ctx, addr, key, token, ttl) (bool, error)
    Start(addr string) error
    Stop()
}
```

**Implementation details:**
- TCP connections with 5-second timeout
- Connection pooling per remote address
- Messages serialized with msgpack
- The `LocalHandler` interface is implemented by `Cache` to handle incoming requests:

```go
type LocalHandler interface {
    HandleGet(key) ([]byte, bool)
    HandleSet(key, value, ttl, tags)
    HandleDelete(key) bool
    HandleExists(key) bool
    HandleMGet(keys) map[string][]byte
    HandleIncr(key, delta) (int64, error)
    HandleLockAcquire(key, ttl) (token, fencing, acquired)
    HandleLockRelease(key, token) bool
    HandleLockExtend(key, token, ttl) bool
}
```

---

## Subsystems

### Distributed Locks

**Files:** `lock.go`, `internal/lock/lock.go`

**Lock Engine** (per-node):

```go
type Engine struct {
    mu         sync.Mutex
    locks      map[string]*Entry
    fencingSeq atomic.Uint64
    done       chan struct{}
}
```

**Features:**
- **Fencing tokens** — Each lock acquisition produces a monotonically increasing `uint64` fencing token. Clients can use this to detect stale writes in a distributed system.
- **Random ownership tokens** — 16-byte hex tokens for identity verification on release/extend.
- **Auto-release** — A background goroutine checks every 1 second and removes expired locks.
- **Distributed routing** — Lock requests are routed to the key's owner via the hash ring, just like cache operations.

**Public API:**

```go
// Acquire with retry
lock, err := cache.Lock(key, LockOptions{TTL, RetryCount, RetryDelay})

// Non-blocking attempt
lock, ok := cache.TryLock(key, ttl)

// On the LockHandle
lock.Release()          // release the lock
lock.Extend(newTTL)     // extend TTL
lock.Token() uint64     // get fencing token
```

### Rate Limiting

**Files:** `ratelimit.go`, `internal/ratelimit/tokenbucket.go`, `internal/ratelimit/slidingwindow.go`

Two algorithms are available:

**Token Bucket:**
- Tokens refill at a fixed rate over the window
- `tokens = min(limit, tokens + elapsed/window * limit)`
- Allows bursts up to the limit
- State stored per-key in a `sync.Map`

**Sliding Window:**
- Tracks individual request timestamps within the window
- Counts requests in the current window
- More precise than fixed windows, no burst allowance
- State stored per-key in a `sync.Map`

**Public API:**

```go
allowed, state := cache.RateLimit(key, RateLimitOptions{Limit, Window})
allowed, state := cache.RateLimitSliding(key, SlidingWindowOptions{Limit, Window})

// RateLimitState
type RateLimitState struct {
    Allowed   bool
    Remaining int64
    Limit     int64
    ResetsAt  time.Time
}
```

### Pub/Sub

**Files:** `pubsub.go`, `internal/pubsub/broker.go`

**Broker:**

```go
type Broker struct {
    subscriptions map[uint64]*Subscription
    nextID        atomic.Uint64

    // Hook callbacks
    onHit, onMiss           []func(key string)
    onEvict, onSet          []func(key string, value []byte)
    onDelete                []func(key string)
}
```

**Event types:**

| Constant | Fired when |
|----------|------------|
| `EventSet` | A key is set or updated |
| `EventDelete` | A key is explicitly deleted |
| `EventExpire` | A key expires (via sweeper or lazy) |
| `EventEvict` | A key is evicted by LRU |

**Subscriptions** use glob pattern matching (e.g., `user:*`, `cache:*`). Each subscription has a buffered channel (256 capacity). Events are dropped (non-blocking) if the subscriber can't keep up.

**Hook callbacks** (`OnHit`, `OnMiss`, `OnEvict`, `OnSet`, `OnDelete`) are synchronous and fire on the calling goroutine. They are intended for lightweight operations like metrics collection.

**Public API:**

```go
sub := cache.Subscribe("user:*")
events := sub.Events()  // <-chan CacheEvent
sub.Close()

cache.OnHit(func(key string) { ... })
cache.OnMiss(func(key string) { ... })
cache.OnEvict(func(key string, value interface{}) { ... })
cache.OnSet(func(key string, value interface{}) { ... })
cache.OnDelete(func(key string) { ... })
```

### Tag-Based Invalidation

**File:** `tags.go`

Maintains a bidirectional index:

```go
type tagIndex struct {
    tagToKeys map[string]map[string]struct{}  // tag → set of keys
    keyToTags map[string][]string             // key → list of tags
}
```

**Operations:**
- `SetWithTags(key, value, ttl, tags)` — Stores a value and associates it with tags
- `InvalidateTag(tag)` — Deletes all keys associated with the tag
- When a key is deleted/evicted/expired, its tag associations are automatically cleaned up

Thread-safe via `sync.RWMutex`.

### Namespaces

**File:** `namespace.go`

Provides multi-tenant key isolation by prefixing all keys with a namespace:

```go
type NamespacedCache struct {
    cache     *Cache
    namespace string
}
```

All operations prefix keys with `"namespace:"`. Tags are also prefixed. This is a thin wrapper — no data duplication.

**Available methods:** `Set, Get, Delete, Exists, TTL, GetOrSet, Incr, Decr, SetWithTags, InvalidateTag`

### HTTP Middleware

**File:** `middleware.go`

**`HTTPRateLimit(cache, limit, window)`** — Rate limits by client IP using token bucket. Sets `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` headers. Returns `429 Too Many Requests` when exceeded.

Client IP extraction priority: `X-Forwarded-For` → `X-Real-IP` → `RemoteAddr`.

**`HTTPCache(cache, ttl)`** — Caches GET responses. Uses a SHA-256 hash of method + URL as the cache key. Serves cached responses with `X-Cache: HIT` header. Only caches 2xx and 3xx responses.

---

## Standalone Server

**Files:** `cmd/cachegrid/main.go`, `internal/server/`

The standalone server exposes all cache functionality over HTTP REST.

### HTTP Routes

| Method | Route | Handler | Description |
|--------|-------|---------|-------------|
| `GET` | `/cache/{key}` | `handleCacheGet` | Get value (returns JSON body) |
| `PUT` | `/cache/{key}` | `handleCacheSet` | Set value (body = value, `X-TTL` header, `X-Tags` header) |
| `DELETE` | `/cache/{key}` | `handleCacheDelete` | Delete key |
| `HEAD` | `/cache/{key}` | `handleCacheExists` | Check existence (200 or 404) |
| `POST` | `/cache/_mget` | `handleCacheMGet` | Bulk get (`{"keys": ["a","b"]}`) |
| `POST` | `/locks/{key}` | `handleLockAcquire` | Acquire lock (`{"ttl": "30s"}`) |
| `DELETE` | `/locks/{key}` | `handleLockRelease` | Release lock (`X-Lock-Token` header) |
| `PUT` | `/locks/{key}` | `handleLockExtend` | Extend lock TTL |
| `POST` | `/ratelimit/{key}` | `handleRateLimit` | Check rate limit |
| `GET` | `/subscribe/{pattern}` | `handleSubscribeSSE` | Server-Sent Events stream |
| `GET` | `/cluster/nodes` | `handleClusterNodes` | Cluster node list |
| `GET` | `/health` | `handleHealth` | Health check |
| `GET` | `/metrics` | `handleMetrics` | Prometheus-format metrics |

### Server Middleware

- CORS headers (permissive by default)
- Request logging
- Recovery from panics

---

## Public API Reference

### Constructors

| Function | Description |
|----------|-------------|
| `New(config Config) (*Cache, error)` | Create cache with full configuration |
| `NewMemory() (*Cache, error)` | Create in-memory cache with defaults |
| `NewDisk(path string) (*Cache, error)` | Create disk-backed cache at path |
| `DefaultConfig() Config` | Returns default configuration |

### Cache Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `Set` | `(key string, value interface{}, ttl time.Duration) error` | Store a value |
| `Get` | `(key string, dest interface{}) bool` | Retrieve and deserialize |
| `Delete` | `(key string)` | Remove a key |
| `Exists` | `(key string) bool` | Check key existence |
| `TTL` | `(key string) time.Duration` | Get remaining TTL (-1 = no expiry, 0 = missing) |
| `GetOrSet` | `(key string, dest interface{}, ttl time.Duration, fn func() (interface{}, error)) error` | Cache-aside pattern |
| `MGet` | `(keys ...string) map[string][]byte` | Bulk get |
| `MSet` | `(items map[string]Item) error` | Bulk set |
| `Incr` | `(key string, delta int64) (int64, error)` | Atomic increment |
| `Decr` | `(key string, delta int64) (int64, error)` | Atomic decrement |
| `Len` | `() int` | Total item count |
| `SetWithTags` | `(key string, value interface{}, ttl time.Duration, tags []string) error` | Set with tag associations |
| `InvalidateTag` | `(tag string)` | Delete all keys with tag |
| `WithNamespace` | `(namespace string) *NamespacedCache` | Create namespaced view |
| `Lock` | `(key string, opts LockOptions) (*LockHandle, error)` | Acquire distributed lock |
| `TryLock` | `(key string, ttl time.Duration) (*LockHandle, bool)` | Non-blocking lock attempt |
| `RateLimit` | `(key string, opts RateLimitOptions) (bool, RateLimitState)` | Token bucket rate limit |
| `RateLimitSliding` | `(key string, opts SlidingWindowOptions) (bool, RateLimitState)` | Sliding window rate limit |
| `Subscribe` | `(pattern string) *Subscription` | Subscribe to cache events |
| `OnHit` | `(fn func(key string))` | Register hit callback |
| `OnMiss` | `(fn func(key string))` | Register miss callback |
| `OnEvict` | `(fn func(key string, value interface{}))` | Register eviction callback |
| `OnSet` | `(fn func(key string, value interface{}))` | Register set callback |
| `OnDelete` | `(fn func(key string))` | Register delete callback |
| `Store` | `() Store` | Access underlying storage backend |
| `ClusterState` | `() *cluster.ClusterState` | Access cluster state (nil if local) |
| `Ring` | `() *cluster.Ring` | Access hash ring (nil if local) |
| `Broker` | `() *pubsub.Broker` | Access pub/sub broker |
| `LockEngine` | `() *lock.Engine` | Access lock engine |
| `Shutdown` | `() error` | Graceful shutdown |

### LockHandle Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `Release` | `() error` | Release the lock |
| `Extend` | `(ttl time.Duration) error` | Extend lock TTL |
| `Token` | `() uint64` | Get fencing token |

### NamespacedCache Methods

All standard cache operations with automatic key prefixing: `Set, Get, Delete, Exists, TTL, GetOrSet, Incr, Decr, SetWithTags, InvalidateTag`.

### HTTP Middleware

| Function | Signature | Description |
|----------|-----------|-------------|
| `HTTPRateLimit` | `(c *Cache, limit int64, window time.Duration) func(http.Handler) http.Handler` | IP-based rate limiting |
| `HTTPCache` | `(c *Cache, ttl time.Duration) func(http.Handler) http.Handler` | GET response caching |

---

## Configuration Reference

```go
type Config struct {
    // Storage backend
    StorageMode     StorageMode   // Memory (default) or Disk
    DiskPath        string        // required for Disk mode

    // Memory mode settings
    NumShards       int           // default: 256, must be power of 2
    MaxMemoryMB     int64         // default: 0 (unlimited)

    // General
    DefaultTTL      time.Duration // default: 0 (no expiry)
    SweeperInterval time.Duration // default: 1s

    // Cluster
    ListenAddr      string        // gossip bind (empty = local-only)
    Peers           []string      // seed nodes for discovery
    Mode            Mode          // Partitioned, Replicated, NearCache
    NodeName        string        // default: hostname
    GRPCPort        int           // default: 7947
    HTTPPort        int           // default: 6380
    VirtualNodes    int           // default: 150
}
```

### Validation Rules

- `StorageMode == Disk` requires `DiskPath` to be non-empty
- `StorageMode == Memory` requires `NumShards` to be a power of 2
- `MaxMemoryMB` must be >= 0
- `SweeperInterval` defaults to 1 second if <= 0
- `NumShards` defaults to 256 if <= 0

---

## Error Types

Defined in `errors.go`:

| Error | Meaning |
|-------|---------|
| `ErrNotFound` | Key does not exist |
| `ErrExpired` | Key has expired |
| `ErrKeyEmpty` | Empty string passed as key |
| `ErrSerializationFailed` | msgpack encode/decode failed |
| `ErrShutdown` | Operation attempted after Shutdown() |
| `ErrNodeNotFound` | Referenced cluster node not found |
| `ErrRemoteCall` | Inter-node RPC failed |
| `ErrLockNotAcquired` | Lock could not be acquired (all retries exhausted) |
| `ErrLockNotHeld` | Lock release/extend failed (expired or wrong token) |
| `ErrRateLimited` | Rate limit exceeded |
