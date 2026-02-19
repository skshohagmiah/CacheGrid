package transport

import (
	"context"
	"time"
)

// Transport defines the interface for inter-node communication.
type Transport interface {
	RemoteGet(ctx context.Context, addr string, key string) (value []byte, found bool, err error)
	RemoteSet(ctx context.Context, addr string, key string, value []byte, ttl time.Duration, tags []string) error
	RemoteDelete(ctx context.Context, addr string, key string) error
	RemoteExists(ctx context.Context, addr string, key string) (bool, error)
	RemoteMGet(ctx context.Context, addr string, keys []string) (map[string][]byte, error)
	RemoteIncr(ctx context.Context, addr string, key string, delta int64) (int64, error)
	RemoteLockAcquire(ctx context.Context, addr string, key string, ttl time.Duration) (token string, fencingToken uint64, acquired bool, err error)
	RemoteLockRelease(ctx context.Context, addr string, key string, token string) (bool, error)
	RemoteLockExtend(ctx context.Context, addr string, key string, token string, ttl time.Duration) (bool, error)
	Start(listenAddr string) error
	Stop() error
}

// LocalHandler processes requests that arrive from remote nodes.
type LocalHandler interface {
	HandleGet(key string) ([]byte, bool)
	HandleSet(key string, value []byte, ttl time.Duration, tags []string)
	HandleDelete(key string) bool
	HandleExists(key string) bool
	HandleMGet(keys []string) map[string][]byte
	HandleIncr(key string, delta int64) (int64, error)
	HandleLockAcquire(key string, ttl time.Duration) (token string, fencingToken uint64, acquired bool)
	HandleLockRelease(key string, token string) bool
	HandleLockExtend(key string, token string, ttl time.Duration) bool
}
