package cachegrid

import (
	"fmt"
	"time"
)

// Mode defines the cache distribution strategy.
type Mode int

const (
	Partitioned Mode = iota
	Replicated
	NearCache
)

// Config holds configuration for a Cache instance.
type Config struct {
	// NumShards is the number of internal shards. Must be a power of 2.
	// Default: 256.
	NumShards int

	// MaxMemoryMB is the maximum memory in megabytes across all shards.
	// 0 means unlimited.
	MaxMemoryMB int64

	// DefaultTTL is the default expiration for entries when no TTL is specified.
	// 0 means no expiry by default.
	DefaultTTL time.Duration

	// SweeperInterval controls how often the background goroutine
	// checks for expired entries. Default: 1 second.
	SweeperInterval time.Duration

	// --- Cluster Configuration ---

	// ListenAddr is the gossip protocol listen address (e.g., ":7946").
	// If empty, the cache runs in local-only mode.
	ListenAddr string

	// Peers is the list of seed nodes for cluster discovery.
	Peers []string

	// Mode is the cache distribution strategy.
	Mode Mode

	// NodeName is a unique identifier for this node. If empty, hostname is used.
	NodeName string

	// GRPCPort is the port for inter-node gRPC/RPC communication. Default: 7947.
	GRPCPort int

	// HTTPPort is the HTTP server port for standalone mode. Default: 6380.
	HTTPPort int

	// VirtualNodes is the number of virtual nodes per physical node on the hash ring.
	// Default: 150.
	VirtualNodes int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		NumShards:       256,
		MaxMemoryMB:     0,
		DefaultTTL:      0,
		SweeperInterval: time.Second,
		Mode:            Partitioned,
		GRPCPort:        7947,
		HTTPPort:        6380,
		VirtualNodes:    150,
	}
}

func (c *Config) validate() error {
	if c.NumShards <= 0 {
		c.NumShards = 256
	}
	if c.NumShards&(c.NumShards-1) != 0 {
		return fmt.Errorf("cachegrid: NumShards must be a power of 2, got %d", c.NumShards)
	}
	if c.MaxMemoryMB < 0 {
		return fmt.Errorf("cachegrid: MaxMemoryMB must be >= 0, got %d", c.MaxMemoryMB)
	}
	if c.SweeperInterval <= 0 {
		c.SweeperInterval = time.Second
	}
	return nil
}
