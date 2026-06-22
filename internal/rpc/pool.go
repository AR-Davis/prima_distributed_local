package rpc

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// NodeState tracks the RPC connection state and device info for a node.
type NodeState struct {
	Addr      string
	Connected bool
	Alignment uint64
	MaxSize   uint64
	FreeMem   uint64
	TotalMem  uint64
	LastError error
	LastCheck time.Time
	Latency   time.Duration
}

// NodePool manages RPC connections to multiple nodes.
type NodePool struct {
	mu    sync.RWMutex
	nodes map[string]*NodeState // addr -> state
}

// NewNodePool creates a new pool of RPC nodes.
func NewNodePool() *NodePool {
	return &NodePool{
		nodes: make(map[string]*NodeState),
	}
}

// Register adds a node address to the pool.
func (p *NodePool) Register(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.nodes[addr]; !exists {
		p.nodes[addr] = &NodeState{Addr: addr}
	}
}

// Unregister removes a node address from the pool.
func (p *NodePool) Unregister(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.nodes, addr)
}

// ProbeAll checks all registered nodes and updates their state.
// For each node, it dials, queries device memory, alignment, and max size,
// then disconnects. This gives us a health check + capacity snapshot.
func (p *NodePool) ProbeAll() map[string]*NodeState {
	p.mu.RLock()
	addrs := make([]string, 0, len(p.nodes))
	for addr := range p.nodes {
		addrs = append(addrs, addr)
	}
	p.mu.RUnlock()
	
	var wg sync.WaitGroup
	for _, addr := range addrs {
		wg.Add(1)
		go func(a string) {
			defer wg.Done()
			p.probeNode(a)
		}(addr)
	}
	wg.Wait()
	
	// Return a snapshot
	p.mu.RLock()
	result := make(map[string]*NodeState, len(p.nodes))
	for addr, state := range p.nodes {
		// Copy to avoid data races
		s := *state
		result[addr] = &s
	}
	p.mu.RUnlock()
	return result
}

// probeNode checks a single node's health and collects device info.
func (p *NodePool) probeNode(addr string) {
	p.mu.Lock()
	state, exists := p.nodes[addr]
	if !exists {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	
	client := NewClient(addr)
	start := time.Now()
	
	// Step 1: Connect
	if err := client.Dial(); err != nil {
		p.mu.Lock()
		state.Connected = false
		state.LastError = err
		state.LastCheck = time.Now()
		p.mu.Unlock()
		return
	}
	defer client.Close()
	
	// Step 2: Query device memory
	mem, err := client.GetDeviceMemory()
	if err != nil {
		p.mu.Lock()
		state.Connected = false
		state.LastError = err
		state.LastCheck = time.Now()
		p.mu.Unlock()
		return
	}
	
	// Step 3: Query alignment
	alignment, err := client.GetAlignment()
	if err != nil {
		log.Printf("[rpc] warning: get_alignment failed for %s: %v", addr, err)
		alignment = 64 // sensible default
	}
	
	// Step 4: Query max buffer size
	maxSize, err := client.GetMaxSize()
	if err != nil {
		log.Printf("[rpc] warning: get_max_size failed for %s: %v", addr, err)
		maxSize = 0
	}
	
	latency := time.Since(start)
	
	p.mu.Lock()
	state.Connected = true
	state.Alignment = alignment
	state.MaxSize = maxSize
	state.FreeMem = mem.Free
	state.TotalMem = mem.Total
	state.LastError = nil
	state.LastCheck = time.Now()
	state.Latency = latency
	p.mu.Unlock()
	
	log.Printf("[rpc] %s: connected, mem=%d/%d MB, alignment=%d, max_buf=%d MB, latency=%s",
		addr, mem.Free/(1024*1024), mem.Total/(1024*1024), alignment, maxSize/(1024*1024), latency)
}

// GetState returns the current state for all nodes.
func (p *NodePool) GetState() map[string]*NodeState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]*NodeState, len(p.nodes))
	for addr, state := range p.nodes {
		s := *state
		result[addr] = &s
	}
	return result
}

// SelectByMemory selects the best RPC node based on free memory.
// Returns the node address with the most free memory, or empty string if none available.
func (p *NodePool) SelectByMemory() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	var bestAddr string
	var bestFree uint64
	for addr, state := range p.nodes {
		if state.Connected && state.FreeMem > bestFree {
			bestFree = state.FreeMem
			bestAddr = addr
		}
	}
	return bestAddr
}

// SelectByLatency selects the fastest RPC node.
// Returns the node address with the lowest latency, or empty string if none available.
func (p *NodePool) SelectByLatency() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	var bestAddr string
	var bestLatency time.Duration = time.Hour // sentinel
	for addr, state := range p.nodes {
		if state.Connected && state.Latency < bestLatency {
			bestLatency = state.Latency
			bestAddr = addr
		}
	}
	return bestAddr
}

// FormatMemory formats a memory size in human-readable form.
func FormatMemory(bytes uint64) string {
	const (
		MB = 1024 * 1024
		GB = 1024 * MB
	)
	if bytes == 0 {
		return ""
	}
	if bytes >= 100*GB {
		return fmt.Sprintf("%.0f GB", float64(bytes)/float64(GB))
	}
	if bytes >= GB {
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	}
	return fmt.Sprintf("%d MB", bytes/MB)
}
