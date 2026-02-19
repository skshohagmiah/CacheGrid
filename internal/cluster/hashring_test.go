package cluster

import (
	"fmt"
	"testing"
)

func TestRingBasic(t *testing.T) {
	r := NewRing(150)

	r.AddNode("node-1")
	r.AddNode("node-2")
	r.AddNode("node-3")

	owner := r.GetNode("test-key")
	if owner == "" {
		t.Fatal("expected a node owner, got empty")
	}

	// Same key should always map to same node
	for i := 0; i < 100; i++ {
		if r.GetNode("test-key") != owner {
			t.Fatal("consistent hashing violation: same key mapped to different nodes")
		}
	}
}

func TestRingEmpty(t *testing.T) {
	r := NewRing(150)
	if r.GetNode("key") != "" {
		t.Fatal("expected empty string for empty ring")
	}
	if !r.IsEmpty() {
		t.Fatal("expected ring to be empty")
	}
}

func TestRingDistribution(t *testing.T) {
	r := NewRing(150)
	r.AddNode("node-1")
	r.AddNode("node-2")
	r.AddNode("node-3")

	counts := make(map[string]int)
	total := 10000
	for i := 0; i < total; i++ {
		owner := r.GetNode(fmt.Sprintf("key-%d", i))
		counts[owner]++
	}

	// Each node should get between 15% and 50% of keys (with 150 vnodes)
	for name, count := range counts {
		pct := float64(count) / float64(total) * 100
		if pct < 15 || pct > 50 {
			t.Fatalf("node %s got %.1f%% of keys (expected 15-50%%)", name, pct)
		}
	}
}

func TestRingAddRemove(t *testing.T) {
	r := NewRing(150)
	r.AddNode("node-1")
	r.AddNode("node-2")

	// Record ownership of 1000 keys
	before := make(map[string]string)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		before[key] = r.GetNode(key)
	}

	// Add a third node
	r.AddNode("node-3")

	// Count how many keys changed ownership
	changed := 0
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		if r.GetNode(key) != before[key] {
			changed++
		}
	}

	// Consistent hashing: only ~1/3 of keys should move
	pct := float64(changed) / 1000 * 100
	if pct > 50 {
		t.Fatalf("too many keys moved: %.1f%% (expected <50%%)", pct)
	}
}

func TestRingRemoveNode(t *testing.T) {
	r := NewRing(150)
	r.AddNode("node-1")
	r.AddNode("node-2")
	r.AddNode("node-3")

	r.RemoveNode("node-2")

	if r.HasNode("node-2") {
		t.Fatal("node-2 should be removed")
	}
	if !r.HasNode("node-1") || !r.HasNode("node-3") {
		t.Fatal("node-1 and node-3 should still exist")
	}

	// All keys should map to node-1 or node-3
	for i := 0; i < 100; i++ {
		owner := r.GetNode(fmt.Sprintf("key-%d", i))
		if owner != "node-1" && owner != "node-3" {
			t.Fatalf("unexpected owner: %s", owner)
		}
	}
}

func TestRingGetNodes(t *testing.T) {
	r := NewRing(150)
	r.AddNode("node-1")
	r.AddNode("node-2")
	r.AddNode("node-3")

	nodes := r.GetNodes("test-key", 2)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0] == nodes[1] {
		t.Fatal("GetNodes returned duplicate nodes")
	}

	// Request more than available
	nodes = r.GetNodes("test-key", 5)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes (all), got %d", len(nodes))
	}
}

func TestRingDuplicateAdd(t *testing.T) {
	r := NewRing(150)
	r.AddNode("node-1")
	r.AddNode("node-1") // duplicate

	if len(r.Members()) != 1 {
		t.Fatalf("expected 1 member, got %d", len(r.Members()))
	}
}

func TestRingSingleNode(t *testing.T) {
	r := NewRing(150)
	r.AddNode("only-node")

	for i := 0; i < 100; i++ {
		if r.GetNode(fmt.Sprintf("key-%d", i)) != "only-node" {
			t.Fatal("single node should own all keys")
		}
	}
}
