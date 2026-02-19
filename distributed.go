package cachegrid

import (
	"context"
	"time"

	"github.com/shohag/cachegrid/internal/cluster"
)

// ownerAddr returns the RPC address of the node that owns the key.
// Returns "" if the key is local.
func (c *Cache) ownerAddr(key string) string {
	if c.ring == nil {
		return ""
	}
	owner := c.ring.GetNode(key)
	if owner == "" || owner == c.state.SelfName() {
		return ""
	}
	node, ok := c.state.GetNode(owner)
	if !ok {
		return ""
	}
	return node.RPCAddr
}

// distributedSet routes a Set operation based on the cache mode.
func (c *Cache) distributedSet(key string, data []byte, ttl time.Duration, tags []string) error {
	switch c.config.Mode {
	case Replicated:
		// Set locally
		c.getShard(key).Set(key, data, ttl)
		// Replicate to all other live nodes
		if c.state != nil {
			for _, node := range c.state.LiveNodes() {
				if node.Name == c.state.SelfName() {
					continue
				}
				addr := node.RPCAddr
				go c.transport.RemoteSet(context.Background(), addr, key, data, ttl, tags)
			}
		}
		return nil

	case NearCache:
		// Always set locally (near cache) + on the owner
		c.getShard(key).Set(key, data, ttl)
		if addr := c.ownerAddr(key); addr != "" {
			return c.transport.RemoteSet(context.Background(), addr, key, data, ttl, tags)
		}
		return nil

	default: // Partitioned
		if addr := c.ownerAddr(key); addr != "" {
			return c.transport.RemoteSet(context.Background(), addr, key, data, ttl, tags)
		}
		c.getShard(key).Set(key, data, ttl)
		return nil
	}
}

// distributedGet routes a Get operation based on the cache mode.
func (c *Cache) distributedGet(key string) ([]byte, bool) {
	switch c.config.Mode {
	case Replicated:
		// All nodes have all data — always local
		return c.getShard(key).Get(key)

	case NearCache:
		// Try local first
		if data, ok := c.getShard(key).Get(key); ok {
			return data, true
		}
		// Miss locally, fetch from owner
		if addr := c.ownerAddr(key); addr != "" {
			data, found, err := c.transport.RemoteGet(context.Background(), addr, key)
			if err == nil && found {
				// Cache locally for next time
				c.getShard(key).Set(key, data, c.config.DefaultTTL)
				return data, true
			}
		}
		return nil, false

	default: // Partitioned
		if addr := c.ownerAddr(key); addr != "" {
			data, found, err := c.transport.RemoteGet(context.Background(), addr, key)
			if err != nil {
				return nil, false
			}
			return data, found
		}
		return c.getShard(key).Get(key)
	}
}

// distributedDelete routes a Delete operation based on the cache mode.
func (c *Cache) distributedDelete(key string) {
	switch c.config.Mode {
	case Replicated:
		c.getShard(key).Delete(key)
		if c.state != nil {
			for _, node := range c.state.LiveNodes() {
				if node.Name == c.state.SelfName() {
					continue
				}
				go c.transport.RemoteDelete(context.Background(), node.RPCAddr, key)
			}
		}

	case NearCache:
		c.getShard(key).Delete(key)
		if addr := c.ownerAddr(key); addr != "" {
			c.transport.RemoteDelete(context.Background(), addr, key)
		}

	default: // Partitioned
		if addr := c.ownerAddr(key); addr != "" {
			c.transport.RemoteDelete(context.Background(), addr, key)
			return
		}
		c.getShard(key).Delete(key)
	}
}

// --- cluster.EventHandler implementation ---

func (c *Cache) OnNodeJoin(node *cluster.NodeInfo) {
	// Future: trigger key rebalancing
}

func (c *Cache) OnNodeLeave(node *cluster.NodeInfo) {
	// Close RPC connection to the departed node
	type connCloser interface{ CloseConnection(addr string) }
	if t, ok := c.transport.(connCloser); ok {
		t.CloseConnection(node.RPCAddr)
	}
}

func (c *Cache) OnNodeUpdate(node *cluster.NodeInfo) {}

// --- transport.LocalHandler implementation ---

func (c *Cache) HandleGet(key string) ([]byte, bool) {
	return c.getShard(key).Get(key)
}

func (c *Cache) HandleSet(key string, value []byte, ttl time.Duration, tags []string) {
	c.getShard(key).Set(key, value, ttl)
	if len(tags) > 0 {
		c.tags.Add(key, tags)
	}
}

func (c *Cache) HandleDelete(key string) bool {
	c.tags.Remove(key)
	return c.getShard(key).Delete(key)
}

func (c *Cache) HandleExists(key string) bool {
	return c.getShard(key).Exists(key)
}

func (c *Cache) HandleMGet(keys []string) map[string][]byte {
	results := make(map[string][]byte, len(keys))
	for _, key := range keys {
		if data, ok := c.getShard(key).Get(key); ok {
			results[key] = data
		}
	}
	return results
}

func (c *Cache) HandleIncr(key string, delta int64) (int64, error) {
	return c.getShard(key).Incr(key, delta, c.config.DefaultTTL)
}

func (c *Cache) HandleLockAcquire(key string, ttl time.Duration) (string, uint64, bool) {
	owner := ""
	if c.state != nil {
		owner = c.state.SelfName()
	}
	return c.lockEngine.Acquire(key, ttl, owner)
}

func (c *Cache) HandleLockRelease(key string, token string) bool {
	return c.lockEngine.Release(key, token)
}

func (c *Cache) HandleLockExtend(key string, token string, ttl time.Duration) bool {
	return c.lockEngine.Extend(key, token, ttl)
}
