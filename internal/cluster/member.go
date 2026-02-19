package cluster

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/hashicorp/memberlist"
)

// EventHandler is called when cluster topology changes.
type EventHandler interface {
	OnNodeJoin(node *NodeInfo)
	OnNodeLeave(node *NodeInfo)
	OnNodeUpdate(node *NodeInfo)
}

// MemberConfig configures the gossip layer.
type MemberConfig struct {
	NodeName string
	BindAddr string
	BindPort int
	Seeds    []string
	RPCPort  int
	HTTPPort int
}

// Membership manages cluster membership via gossip protocol.
type Membership struct {
	list    *memberlist.Memberlist
	config  MemberConfig
	state   *ClusterState
	ring    *Ring
	handler EventHandler
	meta    []byte
}

// NewMembership creates and starts the gossip membership layer.
func NewMembership(cfg MemberConfig, state *ClusterState, ring *Ring, handler EventHandler) (*Membership, error) {
	meta, err := json.Marshal(NodeMeta{
		RPCPort:  cfg.RPCPort,
		HTTPPort: cfg.HTTPPort,
	})
	if err != nil {
		return nil, fmt.Errorf("cluster: failed to marshal node meta: %w", err)
	}

	m := &Membership{
		config:  cfg,
		state:   state,
		ring:    ring,
		handler: handler,
		meta:    meta,
	}

	mlConfig := memberlist.DefaultLANConfig()
	mlConfig.Name = cfg.NodeName
	mlConfig.BindAddr = cfg.BindAddr
	mlConfig.BindPort = cfg.BindPort
	mlConfig.AdvertisePort = cfg.BindPort
	mlConfig.Delegate = m
	mlConfig.Events = m
	mlConfig.LogOutput = log.Default().Writer()

	list, err := memberlist.Create(mlConfig)
	if err != nil {
		return nil, fmt.Errorf("cluster: failed to create memberlist: %w", err)
	}
	m.list = list

	// Add self to ring and state
	selfNode := &NodeInfo{
		Name:     cfg.NodeName,
		Addr:     fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.BindPort),
		RPCAddr:  fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.RPCPort),
		HTTPAddr: fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.HTTPPort),
		Status:   NodeAlive,
		JoinedAt: time.Now(),
		Meta:     NodeMeta{RPCPort: cfg.RPCPort, HTTPPort: cfg.HTTPPort},
	}
	state.AddNode(selfNode)
	ring.AddNode(cfg.NodeName)

	return m, nil
}

// Join attempts to join the cluster using seed addresses.
func (m *Membership) Join() error {
	if len(m.config.Seeds) == 0 {
		return nil
	}
	n, err := m.list.Join(m.config.Seeds)
	if err != nil {
		return fmt.Errorf("cluster: failed to join %d seeds: %w", len(m.config.Seeds)-n, err)
	}
	return nil
}

// Leave gracefully leaves the cluster.
func (m *Membership) Leave(timeout time.Duration) error {
	return m.list.Leave(timeout)
}

// Shutdown shuts down the memberlist.
func (m *Membership) Shutdown() error {
	return m.list.Shutdown()
}

// LocalNode returns info about this node.
func (m *Membership) LocalNode() *NodeInfo {
	node, _ := m.state.GetNode(m.config.NodeName)
	return node
}

// Members returns the raw memberlist members.
func (m *Membership) Members() []*memberlist.Node {
	return m.list.Members()
}

// --- memberlist.Delegate implementation ---

func (m *Membership) NodeMeta(limit int) []byte {
	return m.meta
}

func (m *Membership) NotifyMsg(msg []byte) {
	// Used for custom gossip messages — not needed for Phase 2
}

func (m *Membership) GetBroadcasts(overhead, limit int) [][]byte {
	return nil
}

func (m *Membership) LocalState(join bool) []byte {
	return nil
}

func (m *Membership) MergeRemoteState(buf []byte, join bool) {
}

// --- memberlist.EventDelegate implementation ---

func (m *Membership) NotifyJoin(node *memberlist.Node) {
	info := m.nodeToInfo(node)
	m.state.AddNode(info)
	m.ring.AddNode(node.Name)

	if m.handler != nil && node.Name != m.config.NodeName {
		m.handler.OnNodeJoin(info)
	}
}

func (m *Membership) NotifyLeave(node *memberlist.Node) {
	info := m.nodeToInfo(node)
	info.Status = NodeLeft
	m.state.UpdateStatus(node.Name, NodeLeft)
	m.ring.RemoveNode(node.Name)

	if m.handler != nil && node.Name != m.config.NodeName {
		m.handler.OnNodeLeave(info)
	}
}

func (m *Membership) NotifyUpdate(node *memberlist.Node) {
	info := m.nodeToInfo(node)
	m.state.AddNode(info)

	if m.handler != nil && node.Name != m.config.NodeName {
		m.handler.OnNodeUpdate(info)
	}
}

func (m *Membership) nodeToInfo(node *memberlist.Node) *NodeInfo {
	var meta NodeMeta
	if len(node.Meta) > 0 {
		json.Unmarshal(node.Meta, &meta)
	}

	addr := node.Addr.String()
	rpcAddr := net.JoinHostPort(addr, strconv.Itoa(meta.RPCPort))
	httpAddr := net.JoinHostPort(addr, strconv.Itoa(meta.HTTPPort))

	return &NodeInfo{
		Name:     node.Name,
		Addr:     fmt.Sprintf("%s:%d", addr, node.Port),
		RPCAddr:  rpcAddr,
		HTTPAddr: httpAddr,
		Status:   NodeAlive,
		JoinedAt: time.Now(),
		Meta:     meta,
	}
}
