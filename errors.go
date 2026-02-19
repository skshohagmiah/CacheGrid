package cachegrid

import "errors"

var (
	ErrNotFound            = errors.New("cachegrid: key not found")
	ErrExpired             = errors.New("cachegrid: key expired")
	ErrKeyEmpty            = errors.New("cachegrid: key must not be empty")
	ErrSerializationFailed = errors.New("cachegrid: serialization failed")
	ErrShutdown            = errors.New("cachegrid: cache is shut down")
	ErrNodeNotFound        = errors.New("cachegrid: node not found in cluster")
	ErrRemoteCall          = errors.New("cachegrid: remote call failed")
	ErrLockNotAcquired     = errors.New("cachegrid: lock could not be acquired")
	ErrLockNotHeld         = errors.New("cachegrid: lock not held or expired")
	ErrRateLimited         = errors.New("cachegrid: rate limit exceeded")
)
