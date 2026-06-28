// Package node manages Mycelium inference nodes: health checks,
// pool membership, and load-weighted selection.
// For RPC-type nodes, the health check queries device memory via
// the ggml-rpc binary protocol instead of just TCP dialing.
// For Ollama-type GPU nodes, VerifyGPU polls /api/ps to confirm
// models are loaded in VRAM, not silently fallen back to CPU.
package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/aaronrdavis/mycelium-api/internal/config"
	"github.com/aaronrdavis/mycelium-api/internal/rpc"
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
	Config     config.NodeConfig
	Status     Status
	Latency   time.Duration
	FreeMem   uint64 // Free device memory (bytes) - from RPC probe
	TotalMem  uint64 // Total device memory (bytes) - from RPC probe
	GPUVerified bool   // True if last VerifyGPU confirmed model in VRAM
	GPUModel   string // Model name from last GPU verification
	GPUOnCPU   bool   // True if model silently fell back to CPU
	rpcPool    *rpc.NodePool // Shared RPC pool for device queries
	mu         sync.RWMutex
}

// Manager tracks all nodes and their health.
type Manager struct {
	nodes   map[string]*Node
	pools   map[string][]*Node
	rpcPool *rpc.NodePool
	mu      sync.RWMutex
}

// NewManager creates a node manager from configuration.
func NewManager(cfg *config.Config) *Manager {
	m := &Manager{
		nodes:   make(map[string]*Node),
		pools:   make(map[string][]*Node),
		rpcPool: rpc.NewNodePool(),
	}
	for i := range cfg.Nodes {
		n := &Node{
			Config:  cfg.Nodes[i],
			Status:  StatusUnknown,
			rpcPool: m.rpcPool,
		}
		// Register RPC-type nodes with the RPC pool
		if n.Config.Protocol == config.ProtocolRPC {
			addr := fmt.Sprintf("%s:%d", n.Config.Host, n.Config.Port)
			m.rpcPool.Register(addr)
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

// SelectNodes picks up to maxNodes healthy nodes from the specified pools,
// ordered by score (best first). Used for hedged requests.
func (m *Manager) SelectNodes(poolNames []string, maxNodes int) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allHealthy []*Node
	seen := make(map[string]bool)

	for _, poolName := range poolNames {
		pool, ok := m.pools[poolName]
		if !ok || len(pool) == 0 {
			continue
		}

		for _, n := range pool {
			if seen[n.Config.Name] {
				continue
			}
			if n.GetStatus() == StatusHealthy || n.GetStatus() == StatusUnknown {
				allHealthy = append(allHealthy, n)
				seen[n.Config.Name] = true
			}
		}
	}

	if len(allHealthy) == 0 {
		return nil, fmt.Errorf("no healthy nodes in pools: %v", poolNames)
	}

	// Sort by score descending
	for i := 0; i < len(allHealthy); i++ {
		for j := i + 1; j < len(allHealthy); j++ {
			if scoreNode(allHealthy[j]) > scoreNode(allHealthy[i]) {
				allHealthy[i], allHealthy[j] = allHealthy[j], allHealthy[i]
			}
		}
	}

	if maxNodes > 0 && len(allHealthy) > maxNodes {
		allHealthy = allHealthy[:maxNodes]
	}

	return allHealthy, nil
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
// For RPC-type nodes, it queries the ggml-rpc server for device memory.
// For Ollama-type nodes, it does a TCP dial to the API port,
// then for GPU nodes, runs VerifyGPU to check VRAM placement.
func (n *Node) HealthCheck(ctx context.Context) error {
	if n.Config.Protocol == config.ProtocolRPC {
		return n.rpcHealthCheck()
	}
	if err := n.tcpHealthCheck(); err != nil {
		return err
	}
	// For GPU-type Ollama nodes, verify models are actually in VRAM
	if n.Config.Type == config.NodeTypeGPU && n.GetStatus() == StatusHealthy {
		_ = n.VerifyGPU()
	}
	return nil
}

// rpcHealthCheck queries the RPC server for device memory info.
func (n *Node) rpcHealthCheck() error {
	addr := fmt.Sprintf("%s:%d", n.Config.Host, n.Config.Port)
	client := rpc.NewClient(addr)
	
	start := time.Now()
	
	// Dial
	if err := client.Dial(); err != nil {
		n.mu.Lock()
		n.Status = StatusUnhealthy
		n.Latency = time.Since(start)
		n.mu.Unlock()
		log.Printf("[node] %s unhealthy (rpc dial): %v", n.Config.Name, err)
		return err
	}
	defer client.Close()
	
	// Query device memory
	mem, err := client.GetDeviceMemory()
	if err != nil {
		n.mu.Lock()
		n.Status = StatusUnhealthy
		n.Latency = time.Since(start)
		n.mu.Unlock()
		log.Printf("[node] %s unhealthy (rpc query): %v", n.Config.Name, err)
		return err
	}
	
	latency := time.Since(start)
	
	n.mu.Lock()
	n.Status = StatusHealthy
	n.Latency = latency
	n.FreeMem = mem.Free
	n.TotalMem = mem.Total
	n.mu.Unlock()
	
	log.Printf("[node] %s healthy (rpc, %s, mem: %s/%s, latency: %s)",
		n.Config.Name, n.Config.Type,
		rpc.FormatMemory(mem.Free), rpc.FormatMemory(mem.Total),
		latency.Round(time.Millisecond))
	return nil
}

// tcpHealthCheck does a simple TCP dial to check if a node is reachable.
func (n *Node) tcpHealthCheck() error {
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

// VerifyGPU polls the Ollama /api/ps endpoint to confirm that loaded models
// are actually in VRAM (GPU), not silently fallen back to CPU RAM.
// This catches the "routed to GPU, runs on CPU" bug from SynapticLlamas.
// Only meaningful for Ollama-protocol nodes with type "gpu".
func (n *Node) VerifyGPU() error {
	if n.Config.Protocol != config.ProtocolOllama {
		return nil // Only applies to Ollama nodes
	}

	apiPort := n.Config.APIPort
	if apiPort == 0 {
		apiPort = 11434 // Default Ollama port
	}

	url := fmt.Sprintf("http://%s:%d/api/ps", n.Config.Host, apiPort)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		n.mu.Lock()
		n.GPUVerified = false
		n.GPUOnCPU = false
		n.mu.Unlock()
		log.Printf("[gpu-verify] %s: failed to query /api/ps: %v", n.Config.Name, err)
		return err
	}
	defer resp.Body.Close()

	var psResp struct {
		Models []struct {
			Name     string `json:"name"`
			SizeVRAM int64  `json:"size_vram"`
			Size     int64  `json:"size"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&psResp); err != nil {
		log.Printf("[gpu-verify] %s: failed to parse /api/ps response: %v", n.Config.Name, err)
		return err
	}

	if len(psResp.Models) == 0 {
		n.mu.Lock()
		n.GPUVerified = false
		n.GPUOnCPU = false
		n.GPUModel = ""
		n.mu.Unlock()
		log.Printf("[gpu-verify] %s: no models loaded", n.Config.Name)
		return nil
	}

	// Check each loaded model: if size_vram > 0, it's on GPU
	allOnGPU := true
	var modelsOnCPU []string
	var primaryModel string

	for i, m := range psResp.Models {
		if i == 0 {
			primaryModel = m.Name
		}
		if m.SizeVRAM == 0 {
			allOnGPU = false
			modelsOnCPU = append(modelsOnCPU, m.Name)
		}
	}

	n.mu.Lock()
	n.GPUModel = primaryModel
	if allOnGPU {
		n.GPUVerified = true
		n.GPUOnCPU = false
		n.mu.Unlock()
		log.Printf("[gpu-verify] %s: ✅ models on GPU (VRAM confirmed)", n.Config.Name)
	} else {
		n.GPUVerified = false
		n.GPUOnCPU = true
		n.mu.Unlock()
		log.Printf("[gpu-verify] ⚠️  %s: models on CPU (not VRAM): %v — size_vram=0 for %d model(s)",
			n.Config.Name, modelsOnCPU, len(modelsOnCPU))
	}
	return nil
}

// ForceGPUReload attempts to force a model back onto GPU by unloading
// and reloading with num_gpu: -1. Adapted from SynapticLlamas gpu_controller.
func (n *Node) ForceGPUReload(modelName string) error {
	if n.Config.Protocol != config.ProtocolOllama {
		return fmt.Errorf("force reload only supported for Ollama nodes")
	}

	apiPort := n.Config.APIPort
	if apiPort == 0 {
		apiPort = 11434
	}
	baseURL := fmt.Sprintf("http://%s:%d", n.Config.Host, apiPort)

	client := &http.Client{Timeout: 30 * time.Second}

	// Step 1: Unload the model
	unloadReq := map[string]interface{}{
		"model":      modelName,
		"keep_alive": 0,
	}
	unloadBody, _ := json.Marshal(unloadReq)
	unloadURL := fmt.Sprintf("%s/api/generate", baseURL)
	resp, err := client.Post(unloadURL, "application/json", bytes.NewReader(unloadBody))
	if err != nil {
		return fmt.Errorf("unload failed: %w", err)
	}
	resp.Body.Close()

	time.Sleep(2 * time.Second)

	// Step 2: Reload with num_gpu: -1 (force all layers on GPU)
	reloadReq := map[string]interface{}{
		"model":      modelName,
		"prompt":     "",
		"keep_alive": "1h",
		"options": map[string]interface{}{
			"num_gpu": -1,
		},
	}
	reloadBody, _ := json.Marshal(reloadReq)
	reloadURL := fmt.Sprintf("%s/api/generate", baseURL)
	resp, err = client.Post(reloadURL, "application/json", bytes.NewReader(reloadBody))
	if err != nil {
		return fmt.Errorf("reload failed: %w", err)
	}
	resp.Body.Close()

	time.Sleep(2 * time.Second)

	// Step 3: Re-verify
	return n.VerifyGPU()
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

// GetFreeMem returns the node's free device memory (0 for non-RPC nodes).
func (n *Node) GetFreeMem() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.FreeMem
}

// GetTotalMem returns the node's total device memory (0 for non-RPC nodes).
func (n *Node) GetTotalMem() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.TotalMem
}

// GetName returns the node's name.
func (n *Node) GetName() string { return n.Config.Name }

// GetType returns the node's hardware type.
func (n *Node) GetType() config.NodeType { return n.Config.Type }

// GetPool returns the node's pool assignment.
func (n *Node) GetPool() string { return n.Config.Pool }

// GetHost returns the node's hostname.
func (n *Node) GetHost() string { return n.Config.Host }

// GetAPIPort returns the node's Ollama API port (0 if none).
func (n *Node) GetAPIPort() int { return n.Config.APIPort }

// GetWeight returns the node's load-balancing weight.
func (n *Node) GetWeight() int { return n.Config.Weight }

// GetRPCAddr returns the node's RPC address (host:port).
func (n *Node) GetRPCAddr() string {
	return fmt.Sprintf("%s:%d", n.Config.Host, n.Config.Port)
}

// GetGPUVerified returns whether the last VerifyGPU confirmed VRAM usage.
func (n *Node) GetGPUVerified() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.GPUVerified
}

// GetGPUOnCPU returns true if a model was detected on CPU instead of GPU.
func (n *Node) GetGPUOnCPU() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.GPUOnCPU
}

// GetGPUModel returns the model name from the last GPU verification.
func (n *Node) GetGPUModel() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.GPUModel
}
