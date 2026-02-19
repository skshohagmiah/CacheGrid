package cluster

import (
	"sync"
	"testing"
	"time"
)

func TestClusterStateBasic(t *testing.T) {
	cs := NewClusterState("self")

	cs.AddNode(&NodeInfo{Name: "node-1", Status: NodeAlive, JoinedAt: time.Now()})
	cs.AddNode(&NodeInfo{Name: "node-2", Status: NodeAlive, JoinedAt: time.Now()})

	if cs.NodeCount() != 2 {
		t.Fatalf("expected 2 nodes, got %d", cs.NodeCount())
	}

	node, ok := cs.GetNode("node-1")
	if !ok || node.Name != "node-1" {
		t.Fatal("expected to find node-1")
	}

	_, ok = cs.GetNode("nonexistent")
	if ok {
		t.Fatal("expected not to find nonexistent node")
	}
}

func TestClusterStateRemove(t *testing.T) {
	cs := NewClusterState("self")
	cs.AddNode(&NodeInfo{Name: "node-1", Status: NodeAlive})
	cs.RemoveNode("node-1")

	if cs.NodeCount() != 0 {
		t.Fatalf("expected 0 nodes, got %d", cs.NodeCount())
	}
}

func TestClusterStateLiveNodes(t *testing.T) {
	cs := NewClusterState("self")
	cs.AddNode(&NodeInfo{Name: "alive-1", Status: NodeAlive})
	cs.AddNode(&NodeInfo{Name: "alive-2", Status: NodeAlive})
	cs.AddNode(&NodeInfo{Name: "dead-1", Status: NodeDead})

	live := cs.LiveNodes()
	if len(live) != 2 {
		t.Fatalf("expected 2 live nodes, got %d", len(live))
	}
}

func TestClusterStateUpdateStatus(t *testing.T) {
	cs := NewClusterState("self")
	cs.AddNode(&NodeInfo{Name: "node-1", Status: NodeAlive})
	cs.UpdateStatus("node-1", NodeDead)

	node, _ := cs.GetNode("node-1")
	if node.Status != NodeDead {
		t.Fatalf("expected NodeDead, got %v", node.Status)
	}
}

func TestClusterStateSelfName(t *testing.T) {
	cs := NewClusterState("my-node")
	if cs.SelfName() != "my-node" {
		t.Fatalf("expected 'my-node', got %q", cs.SelfName())
	}
}

func TestClusterStateConcurrent(t *testing.T) {
	cs := NewClusterState("self")
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cs.AddNode(&NodeInfo{Name: "node", Status: NodeAlive})
			cs.GetNode("node")
			cs.LiveNodes()
			cs.AllNodes()
			cs.NodeCount()
		}(i)
	}
	wg.Wait()
}
