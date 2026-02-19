package cluster

import (
	"sync"
	"time"
)

// NodeStatus represents the state of a cluster node.
type NodeStatus int

const (
	NodeAlive NodeStatus = iota
	NodeSuspect
	NodeDead
	NodeLeft
)

func (s NodeStatus) String() string {
	switch s {
	case NodeAlive:
		return "alive"
	case NodeSuspect:
		return "suspect"
	case NodeDead:
		return "dead"
	case NodeLeft:
		return "left"
	default:
		return "unknown"
	}
}

// NodeInfo represents a single node in the cluster.
type NodeInfo struct {
	Name     string
	Addr     string // ip:gossipPort
	RPCAddr  string // ip:rpcPort
	HTTPAddr string // ip:httpPort
	Status   NodeStatus
	JoinedAt time.Time
	Meta     NodeMeta
}

// NodeMeta is metadata carried via gossip.
type NodeMeta struct {
	RPCPort  int    `json:"rpc_port"`
	HTTPPort int    `json:"http_port,omitempty"`
	Version  string `json:"version,omitempty"`
}

// ClusterState tracks all known nodes in the cluster.
type ClusterState struct {
	mu    sync.RWMutex
	nodes map[string]*NodeInfo
	self  string
}

// NewClusterState creates a new cluster state tracker.
func NewClusterState(selfName string) *ClusterState {
	return &ClusterState{
		nodes: make(map[string]*NodeInfo),
		self:  selfName,
	}
}

// AddNode adds or updates a node in the cluster.
func (cs *ClusterState) AddNode(info *NodeInfo) {
	cs.mu.Lock()
	cs.nodes[info.Name] = info
	cs.mu.Unlock()
}

// RemoveNode removes a node from the cluster.
func (cs *ClusterState) RemoveNode(name string) {
	cs.mu.Lock()
	delete(cs.nodes, name)
	cs.mu.Unlock()
}

// UpdateStatus updates the status of a node.
func (cs *ClusterState) UpdateStatus(name string, status NodeStatus) {
	cs.mu.Lock()
	if node, ok := cs.nodes[name]; ok {
		node.Status = status
	}
	cs.mu.Unlock()
}

// GetNode returns a node by name.
func (cs *ClusterState) GetNode(name string) (*NodeInfo, bool) {
	cs.mu.RLock()
	node, ok := cs.nodes[name]
	cs.mu.RUnlock()
	return node, ok
}

// LiveNodes returns all nodes with status Alive.
func (cs *ClusterState) LiveNodes() []*NodeInfo {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	var result []*NodeInfo
	for _, node := range cs.nodes {
		if node.Status == NodeAlive {
			result = append(result, node)
		}
	}
	return result
}

// AllNodes returns all known nodes.
func (cs *ClusterState) AllNodes() []*NodeInfo {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	result := make([]*NodeInfo, 0, len(cs.nodes))
	for _, node := range cs.nodes {
		result = append(result, node)
	}
	return result
}

// SelfName returns this node's name.
func (cs *ClusterState) SelfName() string {
	return cs.self
}

// NodeCount returns the number of known nodes.
func (cs *ClusterState) NodeCount() int {
	cs.mu.RLock()
	n := len(cs.nodes)
	cs.mu.RUnlock()
	return n
}
