package cachegrid

import (
	"hash/fnv"
	"time"

	"github.com/skshohagmiah/cachegrid/internal/cache"
)

// MemoryStore is an in-memory Store backed by sharded maps with LRU eviction.
type MemoryStore struct {
	shards []*cache.Shard
	mask   uint32
}

// NewMemoryStore creates a MemoryStore with the given shard count and per-shard memory limit.
func NewMemoryStore(numShards int, perShardMaxBytes int64) *MemoryStore {
	shards := make([]*cache.Shard, numShards)
	for i := range shards {
		shards[i] = cache.NewShard(perShardMaxBytes)
	}
	return &MemoryStore{
		shards: shards,
		mask:   uint32(numShards - 1),
	}
}

func (m *MemoryStore) getShard(key string) *cache.Shard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return m.shards[h.Sum32()&m.mask]
}

func (m *MemoryStore) Get(key string) ([]byte, bool) {
	return m.getShard(key).Get(key)
}

func (m *MemoryStore) Set(key string, value []byte, ttl time.Duration) {
	m.getShard(key).Set(key, value, ttl)
}

func (m *MemoryStore) Delete(key string) bool {
	return m.getShard(key).Delete(key)
}

func (m *MemoryStore) Exists(key string) bool {
	return m.getShard(key).Exists(key)
}

func (m *MemoryStore) TTL(key string) time.Duration {
	return m.getShard(key).TTL(key)
}

func (m *MemoryStore) Incr(key string, delta int64, defaultTTL time.Duration) (int64, error) {
	return m.getShard(key).Incr(key, delta, defaultTTL)
}

func (m *MemoryStore) DeleteExpired() int {
	total := 0
	for _, s := range m.shards {
		total += s.DeleteExpired()
	}
	return total
}

func (m *MemoryStore) Len() int {
	n := 0
	for _, s := range m.shards {
		n += s.Len()
	}
	return n
}

func (m *MemoryStore) Close() error {
	return nil
}

func (m *MemoryStore) SetOnEvict(fn func(key string, value []byte)) {
	for _, s := range m.shards {
		s.OnEvict = fn
	}
}

func (m *MemoryStore) SetOnExpire(fn func(key string, value []byte)) {
	for _, s := range m.shards {
		s.OnExpire = fn
	}
}
