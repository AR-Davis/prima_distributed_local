// Package llamaserver manages a llama-server subprocess with RPC backend
// offload. It launches llama-server with --rpc pointing to healthy RPC nodes,
// and exposes its OpenAI-compatible API as a backend for mycelium-api.
package llamaserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Manager controls a llama-server subprocess.
type Manager struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	binaryPath  string
	modelPath   string
	port        int
	host        string
	wsl         bool   // if true, run via wsl -e
	extraArgs   []string
	cancel      context.CancelFunc
	healthURL   string
	started     bool
}

// Config holds llama-server launch parameters.
type Config struct {
	BinaryPath string   // path to llama-server binary (in WSL)
	ModelPath  string   // path to .gguf model file
	Port       int      // port to listen on (localhost)
	WSL        bool     // launch via wsl -e
	ExtraArgs  []string // additional CLI args (--ngl, etc.)
}

// NewManager creates a llama-server process manager.
func NewManager(cfg Config) *Manager {
	port := cfg.Port
	if port == 0 {
		port = 8090
	}
	return &Manager{
		binaryPath: cfg.BinaryPath,
		modelPath:  cfg.ModelPath,
		port:       port,
		host:       "0.0.0.0",
		wsl:        cfg.WSL,
		extraArgs:  cfg.ExtraArgs,
		healthURL:  fmt.Sprintf("http://localhost:%d/health", port),
	}
}

// Start launches llama-server with the given RPC nodes.
// If rpcNodes is empty, llama-server runs in CPU-only mode (no RPC offload).
func (m *Manager) Start(rpcNodes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("llama-server already running")
	}

	args := []string{
		"-m", m.modelPath,
		"--port", strconv.Itoa(m.port),
		"--host", m.host,
	}
	args = append(args, m.extraArgs...)

	if len(rpcNodes) > 0 {
		args = append(args, "--rpc", strings.Join(rpcNodes, ","))
	}

	ctx, cancel := context.WithCancel(context.Background())

	var cmd *exec.Cmd
	if m.wsl {
		// Build the command string for WSL
		cmdStr := m.binaryPath + " " + quoteArgs(args)
		cmd = exec.CommandContext(ctx, "wsl", "-e", "bash", "-c", cmdStr)
	} else {
		cmd = exec.CommandContext(ctx, m.binaryPath, args...)
	}

	log.Printf("[llamaserver] starting: %s %s", m.binaryPath, strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start llama-server: %w", err)
	}

	m.cmd = cmd
	m.cancel = cancel
	m.started = true

	// Wait for health in a goroutine
	go func() {
		if err := m.waitForHealth(120 * time.Second); err != nil {
			log.Printf("[llamaserver] health check failed: %v", err)
		}
	}()

	return nil
}

// Stop terminates the llama-server process.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	if m.cancel != nil {
		m.cancel()
	}
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}

	m.started = false
	log.Printf("[llamaserver] stopped")
	return nil
}

// Restart stops and relaunches with a new RPC node list.
func (m *Manager) Restart(rpcNodes []string) error {
	if err := m.Stop(); err != nil {
		return err
	}
	time.Sleep(2 * time.Second) // let port release
	return m.Start(rpcNodes)
}

// IsRunning checks if the process is still alive.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started && m.cmd != nil && m.cmd.ProcessState == nil
}

// IsHealthy checks if llama-server responds on /health.
func (m *Manager) IsHealthy() bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(m.healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// Port returns the port llama-server is listening on.
func (m *Manager) Port() int {
	return m.port
}

// BaseURL returns the base URL for API calls.
func (m *Manager) BaseURL() string {
	return fmt.Sprintf("http://localhost:%d", m.port)
}

// waitForHealth polls the /health endpoint until it responds or timeout.
func (m *Manager) waitForHealth(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}

	for time.Now().Before(deadline) {
		// Check if process died
		if !m.IsRunning() {
			return fmt.Errorf("llama-server process exited")
		}

		resp, err := client.Get(m.healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				log.Printf("[llamaserver] healthy on port %d", m.port)
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("llama-server did not become healthy within %s", timeout)
}

// quoteArgs joins args into a shell-quoted string for WSL bash -c.
func quoteArgs(args []string) string {
	var parts []string
	for _, a := range args {
		// Simple quoting: if it contains spaces or special chars, wrap in single quotes
		if strings.ContainsAny(a, " \t'\"\\") {
			parts = append(parts, "'"+strings.ReplaceAll(a, "'", "'\\''")+"'")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// PortAvailable checks if a TCP port is available.
func PortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}