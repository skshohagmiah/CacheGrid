package cachegrid

import "time"

// StorageMode selects the storage backend.
type StorageMode int

const (
	// Memory is the default in-memory storage mode with LRU eviction.
	Memory StorageMode = iota
	// Disk uses PebbleDB for persistent disk-based storage.
	Disk
)

// Store is the storage backend interface for CacheGrid.
// Implementations must be safe for concurrent use.
type Store interface {
	// Get retrieves the raw serialized value for a key.
	// Returns nil, false on miss or expiry.
	Get(key string) ([]byte, bool)

	// Set stores a serialized value with the given TTL.
	// A TTL of 0 means no expiration.
	Set(key string, value []byte, ttl time.Duration)

	// Delete removes a key. Returns true if the key existed.
	Delete(key string) bool

	// Exists checks if a key exists and is not expired.
	Exists(key string) bool

	// TTL returns the remaining time-to-live for a key.
	// Returns -1 if no expiry, 0 if missing or expired.
	TTL(key string) time.Duration

	// Incr atomically increments a counter stored at key by delta.
	// If the key does not exist, it is initialized to 0 before incrementing.
	// The defaultTTL is applied only when creating a new counter.
	Incr(key string, delta int64, defaultTTL time.Duration) (int64, error)

	// DeleteExpired removes expired entries. Returns count of removed entries.
	DeleteExpired() int

	// Len returns the total number of stored items.
	Len() int

	// Close releases all resources held by the store.
	Close() error

	// SetOnEvict sets the callback fired when an entry is evicted (e.g., LRU).
	SetOnEvict(fn func(key string, value []byte))

	// SetOnExpire sets the callback fired when an entry expires.
	SetOnExpire(fn func(key string, value []byte))
}

// NewMemory creates a Cache with in-memory storage using sensible defaults.
func NewMemory() (*Cache, error) {
	return New(DefaultConfig())
}

// NewDisk creates a Cache with disk-based (Pebble) storage at the given path.
func NewDisk(path string) (*Cache, error) {
	config := DefaultConfig()
	config.StorageMode = Disk
	config.DiskPath = path
	return New(config)
}
