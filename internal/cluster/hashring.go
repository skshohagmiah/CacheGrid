package cluster

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

// DefaultVirtualNodes is the default number of virtual nodes per physical node.
const DefaultVirtualNodes = 150

// Ring is a consistent hash ring that maps keys to cluster nodes.
type Ring struct {
	mu           sync.RWMutex
	vnodes       int
	sortedHashes []uint32
	hashMap      map[uint32]string // hash -> node name
	members      map[string]bool
}

// NewRing creates a new consistent hash ring.
func NewRing(vnodes int) *Ring {
	if vnodes <= 0 {
		vnodes = DefaultVirtualNodes
	}
	return &Ring{
		vnodes:  vnodes,
		hashMap: make(map[uint32]string),
		members: make(map[string]bool),
	}
}

// AddNode adds a physical node with virtual nodes to the ring.
func (r *Ring) AddNode(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.members[name] {
		return
	}

	r.members[name] = true
	for i := 0; i < r.vnodes; i++ {
		h := hashKey(fmt.Sprintf("%s-%d", name, i))
		r.sortedHashes = append(r.sortedHashes, h)
		r.hashMap[h] = name
	}
	sort.Slice(r.sortedHashes, func(i, j int) bool {
		return r.sortedHashes[i] < r.sortedHashes[j]
	})
}

// RemoveNode removes a physical node and all its virtual nodes from the ring.
func (r *Ring) RemoveNode(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.members[name] {
		return
	}

	delete(r.members, name)

	// Rebuild sorted hashes without this node
	newHashes := make([]uint32, 0, len(r.sortedHashes)-r.vnodes)
	for _, h := range r.sortedHashes {
		if r.hashMap[h] != name {
			newHashes = append(newHashes, h)
		} else {
			delete(r.hashMap, h)
		}
	}
	r.sortedHashes = newHashes
}

// GetNode returns the node that owns the given key.
// Returns "" if the ring is empty.
func (r *Ring) GetNode(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.sortedHashes) == 0 {
		return ""
	}

	h := hashKey(key)
	idx := sort.Search(len(r.sortedHashes), func(i int) bool {
		return r.sortedHashes[i] >= h
	})
	if idx >= len(r.sortedHashes) {
		idx = 0 // wrap around
	}
	return r.hashMap[r.sortedHashes[idx]]
}

// GetNodes returns up to n distinct physical nodes for the given key,
// walking clockwise around the ring. Used for replication.
func (r *Ring) GetNodes(key string, n int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.sortedHashes) == 0 {
		return nil
	}

	memberCount := len(r.members)
	if n > memberCount {
		n = memberCount
	}

	h := hashKey(key)
	idx := sort.Search(len(r.sortedHashes), func(i int) bool {
		return r.sortedHashes[i] >= h
	})
	if idx >= len(r.sortedHashes) {
		idx = 0
	}

	seen := make(map[string]bool, n)
	result := make([]string, 0, n)

	for i := 0; i < len(r.sortedHashes) && len(result) < n; i++ {
		pos := (idx + i) % len(r.sortedHashes)
		name := r.hashMap[r.sortedHashes[pos]]
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}

// Members returns all physical node names in the ring.
func (r *Ring) Members() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, 0, len(r.members))
	for name := range r.members {
		result = append(result, name)
	}
	return result
}

// IsEmpty returns true if the ring has no nodes.
func (r *Ring) IsEmpty() bool {
	r.mu.RLock()
	empty := len(r.members) == 0
	r.mu.RUnlock()
	return empty
}

// HasNode returns true if the node is in the ring.
func (r *Ring) HasNode(name string) bool {
	r.mu.RLock()
	has := r.members[name]
	r.mu.RUnlock()
	return has
}

func hashKey(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}
