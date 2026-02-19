package cachegrid

import (
	"context"
	"time"
)

// LockOptions configures lock acquisition behavior.
type LockOptions struct {
	TTL        time.Duration // lock auto-release time (required)
	RetryCount int           // number of retries (0 = no retry)
	RetryDelay time.Duration // delay between retries
}

// LockHandle represents a held distributed lock.
type LockHandle struct {
	cache        *Cache
	key          string
	token        string
	fencingToken uint64
	ownerAddr    string // "" if local
}

// Release releases the lock.
func (h *LockHandle) Release() error {
	if h.ownerAddr != "" {
		ok, err := h.cache.transport.RemoteLockRelease(context.Background(), h.ownerAddr, h.key, h.token)
		if err != nil {
			return err
		}
		if !ok {
			return ErrLockNotHeld
		}
		return nil
	}
	if !h.cache.lockEngine.Release(h.key, h.token) {
		return ErrLockNotHeld
	}
	return nil
}

// Extend extends the lock TTL.
func (h *LockHandle) Extend(ttl time.Duration) error {
	if h.ownerAddr != "" {
		ok, err := h.cache.transport.RemoteLockExtend(context.Background(), h.ownerAddr, h.key, h.token, ttl)
		if err != nil {
			return err
		}
		if !ok {
			return ErrLockNotHeld
		}
		return nil
	}
	if !h.cache.lockEngine.Extend(h.key, h.token, ttl) {
		return ErrLockNotHeld
	}
	return nil
}

// Token returns the fencing token for this lock.
func (h *LockHandle) Token() uint64 {
	return h.fencingToken
}

// Lock acquires a distributed lock with retry logic.
func (c *Cache) Lock(key string, opts LockOptions) (*LockHandle, error) {
	if key == "" {
		return nil, ErrKeyEmpty
	}
	if c.closed.Load() {
		return nil, ErrShutdown
	}

	lockKey := "__lock:" + key

	for attempt := 0; attempt <= opts.RetryCount; attempt++ {
		if addr := c.ownerAddr(lockKey); addr != "" {
			token, fencing, acquired, err := c.transport.RemoteLockAcquire(
				context.Background(), addr, lockKey, opts.TTL)
			if err != nil {
				return nil, err
			}
			if acquired {
				return &LockHandle{
					cache: c, key: lockKey, token: token,
					fencingToken: fencing, ownerAddr: addr,
				}, nil
			}
		} else {
			owner := ""
			if c.state != nil {
				owner = c.state.SelfName()
			}
			token, fencing, acquired := c.lockEngine.Acquire(lockKey, opts.TTL, owner)
			if acquired {
				return &LockHandle{
					cache: c, key: lockKey, token: token,
					fencingToken: fencing,
				}, nil
			}
		}

		if attempt < opts.RetryCount && opts.RetryDelay > 0 {
			time.Sleep(opts.RetryDelay)
		}
	}
	return nil, ErrLockNotAcquired
}

// TryLock attempts a single non-blocking lock acquisition.
func (c *Cache) TryLock(key string, ttl time.Duration) (*LockHandle, bool) {
	handle, err := c.Lock(key, LockOptions{TTL: ttl})
	if err != nil {
		return nil, false
	}
	return handle, true
}
