package cachegrid

import (
	"context"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/shohag/cachegrid/internal/cache"
	"github.com/shohag/cachegrid/internal/cluster"
	"github.com/shohag/cachegrid/internal/lock"
	"github.com/shohag/cachegrid/internal/pubsub"
	"github.com/shohag/cachegrid/internal/ratelimit"
	"github.com/shohag/cachegrid/internal/transport"
)

// Item represents a cache entry for bulk operations.
type Item struct {
	Value interface{}
	TTL   time.Duration
}

// Cache is a sharded in-memory cache with LRU eviction and TTL support.
// It optionally forms a distributed cluster via gossip and RPC.
type Cache struct {
	shards []*cache.Shard
	config Config
	mask   uint32
	closed atomic.Bool
	done   chan struct{}

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

// New creates a new Cache with the given configuration.
func New(config Config) (*Cache, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	if config.NodeName == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "node-1"
		}
		config.NodeName = hostname
	}

	var perShardMax int64
	if config.MaxMemoryMB > 0 {
		perShardMax = (config.MaxMemoryMB * 1024 * 1024) / int64(config.NumShards)
	}

	shards := make([]*cache.Shard, config.NumShards)
	for i := range shards {
		shards[i] = cache.NewShard(perShardMax)
	}

	c := &Cache{
		shards:        shards,
		config:        config,
		mask:          uint32(config.NumShards - 1),
		done:          make(chan struct{}),
		lockEngine:    lock.NewEngine(),
		tokenBucket:   ratelimit.NewTokenBucket(),
		slidingWindow: ratelimit.NewSlidingWindow(),
		broker:        pubsub.NewBroker(),
		tags:          newTagIndex(),
	}

	// Wire shard callbacks to the broker
	for _, s := range c.shards {
		s.OnEvict = func(key string, value []byte) {
			c.broker.Publish(pubsub.Event{Type: pubsub.EventEvict, Key: key, Value: value})
			c.broker.FireEvict(key, value)
			c.tags.Remove(key)
		}
		s.OnExpire = func(key string, value []byte) {
			c.broker.Publish(pubsub.Event{Type: pubsub.EventExpire, Key: key, Value: value})
			c.tags.Remove(key)
		}
	}

	// Initialize cluster if ListenAddr is set
	if config.ListenAddr != "" {
		host, portStr, err := net.SplitHostPort(config.ListenAddr)
		if err != nil {
			return nil, fmt.Errorf("cachegrid: invalid ListenAddr %q: %w", config.ListenAddr, err)
		}
		if host == "" {
			host = "0.0.0.0"
		}
		port, _ := strconv.Atoi(portStr)

		cs := cluster.NewClusterState(config.NodeName)
		r := cluster.NewRing(config.VirtualNodes)

		// Start RPC transport
		t := transport.NewTCPTransport(c, 5*time.Second)
		rpcAddr := fmt.Sprintf("%s:%d", host, config.GRPCPort)
		if err := t.Start(rpcAddr); err != nil {
			return nil, fmt.Errorf("cachegrid: failed to start RPC transport: %w", err)
		}
		c.transport = t
		c.ring = r
		c.state = cs

		// Start gossip membership
		m, err := cluster.NewMembership(cluster.MemberConfig{
			NodeName: config.NodeName,
			BindAddr: host,
			BindPort: port,
			Seeds:    config.Peers,
			RPCPort:  config.GRPCPort,
			HTTPPort: config.HTTPPort,
		}, cs, r, c)
		if err != nil {
			t.Stop()
			return nil, fmt.Errorf("cachegrid: failed to create cluster: %w", err)
		}
		c.membership = m

		if len(config.Peers) > 0 {
			m.Join() // best-effort; seeds may not be up yet
		}
	}

	go c.startSweeper()

	return c, nil
}

// Set stores a value with the given TTL.
func (c *Cache) Set(key string, value interface{}, ttl time.Duration) error {
	if key == "" {
		return ErrKeyEmpty
	}
	if c.closed.Load() {
		return ErrShutdown
	}

	data, err := cache.Serialize(value)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSerializationFailed, err)
	}

	if ttl == 0 {
		ttl = c.config.DefaultTTL
	}

	if err := c.distributedSet(key, data, ttl, nil); err != nil {
		return err
	}

	c.broker.Publish(pubsub.Event{Type: pubsub.EventSet, Key: key, Value: data})
	c.broker.FireSet(key, data)
	return nil
}

// Get retrieves a value and deserializes it into dest.
func (c *Cache) Get(key string, dest interface{}) bool {
	if key == "" || c.closed.Load() {
		return false
	}

	data, ok := c.distributedGet(key)
	if !ok {
		c.broker.FireMiss(key)
		return false
	}

	if err := cache.Deserialize(data, dest); err != nil {
		return false
	}
	c.broker.FireHit(key)
	return true
}

// Delete removes a key from the cache.
func (c *Cache) Delete(key string) {
	if key == "" || c.closed.Load() {
		return
	}
	c.distributedDelete(key)
	c.broker.Publish(pubsub.Event{Type: pubsub.EventDelete, Key: key})
	c.broker.FireDelete(key)
	c.tags.Remove(key)
}

// Exists checks if a key exists and is not expired.
func (c *Cache) Exists(key string) bool {
	if key == "" || c.closed.Load() {
		return false
	}
	if addr := c.ownerAddr(key); addr != "" {
		ok, err := c.transport.RemoteExists(context.Background(), addr, key)
		return err == nil && ok
	}
	return c.getShard(key).Exists(key)
}

// TTL returns the remaining time-to-live for a key.
func (c *Cache) TTL(key string) time.Duration {
	if key == "" || c.closed.Load() {
		return 0
	}
	return c.getShard(key).TTL(key)
}

// GetOrSet retrieves a value or computes and stores it on miss.
func (c *Cache) GetOrSet(key string, dest interface{}, ttl time.Duration, fn func() (interface{}, error)) error {
	if key == "" {
		return ErrKeyEmpty
	}
	if c.closed.Load() {
		return ErrShutdown
	}

	if c.Get(key, dest) {
		return nil
	}

	value, err := fn()
	if err != nil {
		return err
	}

	if err := c.Set(key, value, ttl); err != nil {
		return err
	}

	data, err := cache.Serialize(value)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSerializationFailed, err)
	}
	return cache.Deserialize(data, dest)
}

// MGet retrieves multiple keys.
func (c *Cache) MGet(keys ...string) map[string][]byte {
	if c.closed.Load() {
		return nil
	}

	results := make(map[string][]byte, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if data, ok := c.distributedGet(key); ok {
			results[key] = data
		}
	}
	return results
}

// MSet stores multiple key-value pairs.
func (c *Cache) MSet(items map[string]Item) error {
	if c.closed.Load() {
		return ErrShutdown
	}
	for key, item := range items {
		if err := c.Set(key, item.Value, item.TTL); err != nil {
			return err
		}
	}
	return nil
}

// Incr atomically increments a counter.
func (c *Cache) Incr(key string, delta int64) (int64, error) {
	if key == "" {
		return 0, ErrKeyEmpty
	}
	if c.closed.Load() {
		return 0, ErrShutdown
	}
	if addr := c.ownerAddr(key); addr != "" {
		return c.transport.RemoteIncr(context.Background(), addr, key, delta)
	}
	return c.getShard(key).Incr(key, delta, c.config.DefaultTTL)
}

// Decr atomically decrements a counter.
func (c *Cache) Decr(key string, delta int64) (int64, error) {
	if key == "" {
		return 0, ErrKeyEmpty
	}
	if c.closed.Load() {
		return 0, ErrShutdown
	}
	if addr := c.ownerAddr(key); addr != "" {
		return c.transport.RemoteIncr(context.Background(), addr, key, -delta)
	}
	return c.getShard(key).Incr(key, -delta, c.config.DefaultTTL)
}

// Len returns the total number of items across all shards.
func (c *Cache) Len() int {
	n := 0
	for _, s := range c.shards {
		n += s.Len()
	}
	return n
}

// Shutdown gracefully stops all subsystems.
func (c *Cache) Shutdown() error {
	if c.closed.CompareAndSwap(false, true) {
		close(c.done)
		if c.membership != nil {
			c.membership.Leave(5 * time.Second)
			c.membership.Shutdown()
		}
		if c.transport != nil {
			c.transport.Stop()
		}
		c.lockEngine.Shutdown()
		c.broker.Shutdown()
	}
	return nil
}

// ClusterState returns the cluster state, or nil if running in local mode.
func (c *Cache) ClusterState() *cluster.ClusterState {
	return c.state
}

// Ring returns the hash ring, or nil if running in local mode.
func (c *Cache) Ring() *cluster.Ring {
	return c.ring
}

// Broker returns the pub/sub broker.
func (c *Cache) Broker() *pubsub.Broker {
	return c.broker
}

// LockEngine returns the lock engine.
func (c *Cache) LockEngine() *lock.Engine {
	return c.lockEngine
}

func (c *Cache) getShard(key string) *cache.Shard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return c.shards[h.Sum32()&c.mask]
}

func (c *Cache) startSweeper() {
	ticker := time.NewTicker(c.config.SweeperInterval)
	defer ticker.Stop()

	shardIdx := 0
	shardsPerTick := len(c.shards) / 4
	if shardsPerTick < 1 {
		shardsPerTick = 1
	}

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			for i := 0; i < shardsPerTick; i++ {
				c.shards[shardIdx].DeleteExpired()
				shardIdx = (shardIdx + 1) % len(c.shards)
			}
		}
	}
}
