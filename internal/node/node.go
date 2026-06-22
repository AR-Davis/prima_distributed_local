// Package node manages Mycelium inference nodes: health checks,
// pool membership, and load-weighted selection.
package node

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/aaronrdavis/mycelium-api/internal/config"
)

// Status represents node health.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusUnknown   Status = "unknown"
)

// Node tracks a single inference node's state.
type Node struct {
	Config  config.NodeConfig
	Status  Status
	Latency time.Duration
	mu      sync.RWMutex
}

// Manager tracks all nodes and their health.
type Manager struct {
	nodes map[string]*Node
	pools map[string][]*Node
	mu    sync.RWMutex
}

// NewManager creates a node manager from configuration.
func NewManager(cfg *config.Config) *Manager {
	m := &Manager{
		nodes: make(map[string]*Node),
		pools: make(map[string][]*Node),
	}
	for i := range cfg.Nodes {
		n := &Node{
			Config: cfg.Nodes[i],
			Status: StatusUnknown,
		}
		m.nodes[n.Config.Name] = n
		m.pools[n.Config.Pool] = append(m.pools[n.Config.Pool], n)
	}
	return m
}

// SelectNode picks a healthy node from the specified pools.
func (m *Manager) SelectNode(poolNames []string) (*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, poolName := range poolNames {
		pool, ok := m.pools[poolName]
		if !ok || len(pool) == 0 {
			continue
		}

		var healthy []*Node
		for _, n := range pool {
			if n.GetStatus() == StatusHealthy || n.GetStatus() == StatusUnknown {
				healthy = append(healthy, n)
			}
		}

		if len(healthy) == 0 {
			continue
		}

		best := healthy[0]
		for _, n := range healthy[1:] {
			if scoreNode(n) > scoreNode(best) {
				best = n
			}
		}
		return best, nil
	}

	return nil, fmt.Errorf("no healthy nodes in pools: %v", poolNames)
}

func scoreNode(n *Node) float64 {
	latencyFactor := 1.0
	lat := n.GetLatency()
	if lat > 0 {
		latencyFactor = 1.0 / (1.0 + lat.Seconds())
	}
	return float64(n.GetWeight()) * latencyFactor
}

// AllNodes returns all known nodes.
func (m *Manager) AllNodes() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		result = append(result, n)
	}
	return result
}

// HealthCheck probes a single node.
func (n *Node) HealthCheck(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", n.Config.Host, n.Config.Port)
	if n.Config.APIPort > 0 {
		addr = fmt.Sprintf("%s:%d", n.Config.Host, n.Config.APIPort)
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	latency := time.Since(start)

	n.mu.Lock()
	if err != nil {
		n.Status = StatusUnhealthy
		n.Latency = latency
		n.mu.Unlock()
		log.Printf("[node] %s unhealthy: %v (%s)", n.Config.Name, err, latency.Round(time.Millisecond))
		return err
	}
	conn.Close()

	n.Status = StatusHealthy
	n.Latency = latency
	n.mu.Unlock()
	log.Printf("[node] %s healthy (%s, %s latency)", n.Config.Name, n.Config.Type, latency.Round(time.Millisecond))
	return nil
}

// StartHealthChecks runs periodic health checks on all nodes.
func (m *Manager) StartHealthChecks(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for _, n := range m.AllNodes() {
		_ = n.HealthCheck(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, n := range m.AllNodes() {
				go func(node *Node) {
					_ = node.HealthCheck(ctx)
				}(n)
			}
		}
	}
}

// GetNodeByName returns a node by name.
func (m *Manager) GetNodeByName(name string) (*Node, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.nodes[name]
	return n, ok
}

// --- Accessor methods for cross-package use ---

// GetStatus returns the node's current status.
func (n *Node) GetStatus() Status {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.Status
}

// GetLatency returns the node's last health-check latency.
func (n *Node) GetLatency() time.Duration {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.Latency
}

// GetName returns the node's name.
func (n *Node) GetName() string {
	return n.Config.Name
}

// GetType returns the node's hardware type.
func (n *Node) GetType() config.NodeType {
	return n.Config.Type
}

// GetPool returns the node's pool assignment.
func (n *Node) GetPool() string {
	return n.Config.Pool
}

// GetHost returns the node's hostname.
func (n *Node) GetHost() string {
	return n.Config.Host
}

// GetAPIPort returns the node's Ollama API port (0 if none).
func (n *Node) GetAPIPort() int {
	return n.Config.APIPort
}

// GetWeight returns the node's load-balancing weight.
func (n *Node) GetWeight() int {
	return n.Config.Weight
}
