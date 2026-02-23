package cachegrid

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble"
	icache "github.com/skshohagmiah/cachegrid/internal/cache"
)

// PebbleStore is a disk-based Store backed by CockroachDB's Pebble engine.
type PebbleStore struct {
	db       *pebble.DB
	mu       sync.Mutex // protects Incr read-modify-write
	count    atomic.Int64
	onEvict  func(key string, value []byte)
	onExpire func(key string, value []byte)
	cbMu     sync.RWMutex // protects callback pointers
}

// NewPebbleStore opens or creates a Pebble database at the given path.
func NewPebbleStore(path string) (*PebbleStore, error) {
	db, err := pebble.Open(path, &pebble.Options{})
	if err != nil {
		return nil, err
	}
	ps := &PebbleStore{db: db}
	// Count existing keys
	ps.count.Store(ps.countKeys())
	return ps, nil
}

// encode packs a value and its expiration into a single byte slice.
// Format: [8 bytes expiresAt unix nano, big-endian][value bytes]
// expiresAt of 0 means no expiration.
func (p *PebbleStore) encode(value []byte, ttl time.Duration) []byte {
	var expiresAtNano int64
	if ttl > 0 {
		expiresAtNano = time.Now().Add(ttl).UnixNano()
	}
	buf := make([]byte, 8+len(value))
	binary.BigEndian.PutUint64(buf[:8], uint64(expiresAtNano))
	copy(buf[8:], value)
	return buf
}

// decode extracts the value and expiration from a raw byte slice.
func (p *PebbleStore) decode(raw []byte) (value []byte, expiresAtNano int64) {
	if len(raw) < 8 {
		return nil, 0
	}
	expiresAtNano = int64(binary.BigEndian.Uint64(raw[:8]))
	value = make([]byte, len(raw)-8)
	copy(value, raw[8:])
	return
}

func (p *PebbleStore) isExpired(expiresAtNano int64) bool {
	return expiresAtNano != 0 && time.Now().UnixNano() > expiresAtNano
}

func (p *PebbleStore) Get(key string) ([]byte, bool) {
	raw, closer, err := p.db.Get([]byte(key))
	if err != nil {
		return nil, false
	}
	// Copy raw before closing — Pebble's slice is only valid until Close.
	rawCopy := make([]byte, len(raw))
	copy(rawCopy, raw)
	closer.Close()

	value, expiresAt := p.decode(rawCopy)
	if p.isExpired(expiresAt) {
		p.db.Delete([]byte(key), pebble.NoSync)
		p.count.Add(-1)
		return nil, false
	}
	return value, true
}

func (p *PebbleStore) Set(key string, value []byte, ttl time.Duration) {
	// Check if key already exists to keep count accurate
	_, closer, err := p.db.Get([]byte(key))
	exists := err == nil
	if exists {
		closer.Close()
	}

	encoded := p.encode(value, ttl)
	if err := p.db.Set([]byte(key), encoded, pebble.NoSync); err != nil {
		return
	}
	if !exists {
		p.count.Add(1)
	}
}

func (p *PebbleStore) Delete(key string) bool {
	_, closer, err := p.db.Get([]byte(key))
	if err != nil {
		return false
	}
	closer.Close()

	if err := p.db.Delete([]byte(key), pebble.NoSync); err != nil {
		return false
	}
	p.count.Add(-1)
	return true
}

func (p *PebbleStore) Exists(key string) bool {
	raw, closer, err := p.db.Get([]byte(key))
	if err != nil {
		return false
	}
	rawCopy := make([]byte, len(raw))
	copy(rawCopy, raw)
	closer.Close()

	_, expiresAt := p.decode(rawCopy)
	if p.isExpired(expiresAt) {
		p.db.Delete([]byte(key), pebble.NoSync)
		p.count.Add(-1)
		return false
	}
	return true
}

func (p *PebbleStore) TTL(key string) time.Duration {
	raw, closer, err := p.db.Get([]byte(key))
	if err != nil {
		return 0
	}
	rawCopy := make([]byte, len(raw))
	copy(rawCopy, raw)
	closer.Close()

	_, expiresAtNano := p.decode(rawCopy)
	if expiresAtNano == 0 {
		return -1
	}
	remaining := time.Until(time.Unix(0, expiresAtNano))
	if remaining <= 0 {
		p.db.Delete([]byte(key), pebble.NoSync)
		p.count.Add(-1)
		return 0
	}
	return remaining
}

func (p *PebbleStore) Incr(key string, delta int64, defaultTTL time.Duration) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var current int64
	var ttl time.Duration

	raw, closer, err := p.db.Get([]byte(key))
	if err == nil {
		rawCopy := make([]byte, len(raw))
		copy(rawCopy, raw)
		closer.Close()

		value, expiresAtNano := p.decode(rawCopy)
		if !p.isExpired(expiresAtNano) {
			if err := icache.Deserialize(value, &current); err != nil {
				return 0, err
			}
			// Preserve original TTL
			if expiresAtNano != 0 {
				remaining := time.Until(time.Unix(0, expiresAtNano))
				if remaining > 0 {
					ttl = remaining
				}
			}
		} else {
			// Expired — treat as new key
			ttl = defaultTTL
			p.count.Add(-1) // will be re-added by Set logic below
		}
	} else {
		// New key
		ttl = defaultTTL
	}

	current += delta

	newValue, err := icache.Serialize(current)
	if err != nil {
		return 0, err
	}

	encoded := p.encode(newValue, ttl)
	_, closer2, getErr := p.db.Get([]byte(key))
	existed := getErr == nil
	if existed {
		closer2.Close()
	}

	if err := p.db.Set([]byte(key), encoded, pebble.NoSync); err != nil {
		return 0, err
	}
	if !existed {
		p.count.Add(1)
	}

	return current, nil
}

func (p *PebbleStore) DeleteExpired() int {
	now := time.Now().UnixNano()
	type expiredEntry struct {
		key   string
		value []byte
	}
	var expired []expiredEntry

	iter, err := p.db.NewIter(nil)
	if err != nil {
		return 0
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		raw := iter.Value()
		if len(raw) < 8 {
			continue
		}
		expiresAtNano := int64(binary.BigEndian.Uint64(raw[:8]))
		if expiresAtNano != 0 && now > expiresAtNano {
			key := make([]byte, len(iter.Key()))
			copy(key, iter.Key())
			value := make([]byte, len(raw)-8)
			copy(value, raw[8:])
			expired = append(expired, expiredEntry{key: string(key), value: value})
		}
	}

	if len(expired) == 0 {
		return 0
	}

	batch := p.db.NewBatch()
	for _, e := range expired {
		batch.Delete([]byte(e.key), nil)
	}
	batch.Commit(pebble.NoSync)
	batch.Close()

	p.count.Add(int64(-len(expired)))

	// Fire callbacks outside of any locks
	p.cbMu.RLock()
	fn := p.onExpire
	p.cbMu.RUnlock()
	if fn != nil {
		for _, e := range expired {
			fn(e.key, e.value)
		}
	}

	return len(expired)
}

func (p *PebbleStore) Len() int {
	return int(p.count.Load())
}

func (p *PebbleStore) Close() error {
	return p.db.Close()
}

func (p *PebbleStore) SetOnEvict(fn func(key string, value []byte)) {
	p.cbMu.Lock()
	p.onEvict = fn
	p.cbMu.Unlock()
}

func (p *PebbleStore) SetOnExpire(fn func(key string, value []byte)) {
	p.cbMu.Lock()
	p.onExpire = fn
	p.cbMu.Unlock()
}

// countKeys iterates through all keys to get initial count.
func (p *PebbleStore) countKeys() int64 {
	var count int64
	iter, err := p.db.NewIter(nil)
	if err != nil {
		return 0
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}
	return count
}
