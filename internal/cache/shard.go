package cache

import (
	"sync"
	"time"
)

// Shard is a single partition of the cache, protected by its own mutex.
type Shard struct {
	mu      sync.RWMutex
	items   map[string]*Entry
	lru     *LRUList
	Size    int64
	MaxSize int64 // 0 = unlimited

	// Callbacks invoked when entries are evicted or expired.
	// These are called while the shard lock is NOT held.
	OnEvict  func(key string, value []byte)
	OnExpire func(key string, value []byte)
}

// NewShard creates a new shard with the given memory limit.
func NewShard(maxSize int64) *Shard {
	return &Shard{
		items:   make(map[string]*Entry),
		lru:     NewLRUList(),
		MaxSize: maxSize,
	}
}

// Get retrieves the raw value for a key. Returns nil, false on miss or expiry.
// Promotes the entry in the LRU list on hit.
func (s *Shard) Get(key string) ([]byte, bool) {
	s.mu.Lock()
	e, ok := s.items[key]
	if !ok {
		s.mu.Unlock()
		return nil, false
	}
	if e.IsExpired() {
		s.removeLocked(key, e)
		s.mu.Unlock()
		return nil, false
	}
	s.lru.MoveToFront(e.Element)
	val := e.Value
	s.mu.Unlock()
	return val, true
}

// Set stores a serialized value with the given TTL. Overwrites existing entries.
func (s *Shard) Set(key string, value []byte, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	sz := EstimateSize(key, value)

	s.mu.Lock()
	if existing, ok := s.items[key]; ok {
		s.Size -= existing.Size
		s.lru.Remove(existing.Element)
	}

	e := &Entry{
		Key:       key,
		Value:     value,
		ExpiresAt: expiresAt,
		Size:      sz,
	}
	e.Element = s.lru.PushFront(e)
	s.items[key] = e
	s.Size += sz

	s.evictLocked()
	s.mu.Unlock()
}

// Delete removes a key from the shard. Returns true if the key existed.
func (s *Shard) Delete(key string) bool {
	s.mu.Lock()
	e, ok := s.items[key]
	if ok {
		s.removeLocked(key, e)
	}
	s.mu.Unlock()
	return ok
}

// Exists checks if a key exists and is not expired.
func (s *Shard) Exists(key string) bool {
	s.mu.RLock()
	e, ok := s.items[key]
	if ok && e.IsExpired() {
		s.mu.RUnlock()
		s.mu.Lock()
		if e2, ok2 := s.items[key]; ok2 && e2.IsExpired() {
			s.removeLocked(key, e2)
		}
		s.mu.Unlock()
		return false
	}
	s.mu.RUnlock()
	return ok
}

// TTL returns the remaining time-to-live for a key.
// Returns -1 if no expiry, 0 if missing or expired.
func (s *Shard) TTL(key string) time.Duration {
	s.mu.RLock()
	e, ok := s.items[key]
	if !ok {
		s.mu.RUnlock()
		return 0
	}
	if e.IsExpired() {
		s.mu.RUnlock()
		s.mu.Lock()
		if e2, ok2 := s.items[key]; ok2 && e2.IsExpired() {
			s.removeLocked(key, e2)
		}
		s.mu.Unlock()
		return 0
	}
	t := e.TTL()
	s.mu.RUnlock()
	return t
}

// Incr atomically increments a counter stored as int64.
// If the key does not exist, it is initialized to 0 before incrementing.
func (s *Shard) Incr(key string, delta int64, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var current int64

	if e, ok := s.items[key]; ok && !e.IsExpired() {
		if err := Deserialize(e.Value, &current); err != nil {
			return 0, err
		}
		// Preserve original TTL
		ttl = 0
		if !e.ExpiresAt.IsZero() {
			remaining := time.Until(e.ExpiresAt)
			if remaining > 0 {
				ttl = remaining
			}
		}
		s.Size -= e.Size
		s.lru.Remove(e.Element)
		delete(s.items, key)
	} else if ok && e.IsExpired() {
		s.removeLocked(key, e)
	}

	current += delta

	value, err := Serialize(current)
	if err != nil {
		return 0, err
	}

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	sz := EstimateSize(key, value)
	newEntry := &Entry{
		Key:       key,
		Value:     value,
		ExpiresAt: expiresAt,
		Size:      sz,
	}
	newEntry.Element = s.lru.PushFront(newEntry)
	s.items[key] = newEntry
	s.Size += sz

	s.evictLocked()
	return current, nil
}

// DeleteExpired removes all expired entries from this shard.
// Returns the number of entries removed.
func (s *Shard) DeleteExpired() int {
	now := time.Now()
	type expired struct {
		key   string
		value []byte
	}
	var expiredEntries []expired

	s.mu.Lock()
	count := 0
	for key, e := range s.items {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			if s.OnExpire != nil {
				expiredEntries = append(expiredEntries, expired{key: key, value: e.Value})
			}
			s.removeLocked(key, e)
			count++
		}
	}
	s.mu.Unlock()

	// Fire callbacks outside the lock
	for _, ex := range expiredEntries {
		s.OnExpire(ex.key, ex.value)
	}
	return count
}

// Len returns the number of items in this shard.
func (s *Shard) Len() int {
	s.mu.RLock()
	n := len(s.items)
	s.mu.RUnlock()
	return n
}

func (s *Shard) removeLocked(key string, e *Entry) {
	s.lru.Remove(e.Element)
	s.Size -= e.Size
	delete(s.items, key)
}

func (s *Shard) evictLocked() {
	if s.MaxSize <= 0 {
		return
	}
	for s.Size > s.MaxSize && s.lru.Len() > 0 {
		oldest := s.lru.RemoveOldest()
		if oldest != nil {
			s.Size -= oldest.Size
			delete(s.items, oldest.Key)
			if s.OnEvict != nil {
				// Fire callback outside lock via goroutine to avoid deadlock
				go s.OnEvict(oldest.Key, oldest.Value)
			}
		}
	}
}
