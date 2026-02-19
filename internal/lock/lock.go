package lock

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// Entry represents a held lock.
type Entry struct {
	Key          string
	Token        string
	FencingToken uint64
	ExpiresAt    time.Time
	Owner        string
}

// IsExpired returns true if the lock has expired.
func (e *Entry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// Engine manages distributed locks.
type Engine struct {
	mu         sync.Mutex
	locks      map[string]*Entry
	fencingSeq atomic.Uint64
	done       chan struct{}
}

// NewEngine creates a new lock engine.
func NewEngine() *Engine {
	e := &Engine{
		locks: make(map[string]*Entry),
		done:  make(chan struct{}),
	}
	go e.startCleanup()
	return e
}

// Acquire attempts to acquire a lock.
func (e *Engine) Acquire(key string, ttl time.Duration, owner string) (token string, fencingToken uint64, acquired bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if existing, ok := e.locks[key]; ok && !existing.IsExpired() {
		return "", 0, false
	}

	token = generateToken()
	fencing := e.fencingSeq.Add(1)

	e.locks[key] = &Entry{
		Key:          key,
		Token:        token,
		FencingToken: fencing,
		ExpiresAt:    time.Now().Add(ttl),
		Owner:        owner,
	}
	return token, fencing, true
}

// Release releases a lock if the token matches.
func (e *Engine) Release(key string, token string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	entry, ok := e.locks[key]
	if !ok {
		return false
	}
	if entry.Token != token {
		return false
	}
	delete(e.locks, key)
	return true
}

// Extend extends a lock's TTL if the token matches.
func (e *Engine) Extend(key string, token string, ttl time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	entry, ok := e.locks[key]
	if !ok || entry.IsExpired() {
		return false
	}
	if entry.Token != token {
		return false
	}
	entry.ExpiresAt = time.Now().Add(ttl)
	return true
}

// IsHeld returns true if the lock is currently held.
func (e *Engine) IsHeld(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, ok := e.locks[key]
	return ok && !entry.IsExpired()
}

// ActiveLocks returns all currently held (non-expired) locks.
func (e *Engine) ActiveLocks() []*Entry {
	e.mu.Lock()
	defer e.mu.Unlock()
	var result []*Entry
	for _, entry := range e.locks {
		if !entry.IsExpired() {
			result = append(result, entry)
		}
	}
	return result
}

// Shutdown stops the cleanup goroutine.
func (e *Engine) Shutdown() {
	close(e.done)
}

func (e *Engine) startCleanup() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
			e.mu.Lock()
			for key, entry := range e.locks {
				if entry.IsExpired() {
					delete(e.locks, key)
				}
			}
			e.mu.Unlock()
		}
	}
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
