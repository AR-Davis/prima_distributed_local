// Package service contains the worker process wrapper
package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/AR-Davis/prima_distributed_local/pkg/config"
	"github.com/AR-Davis/prima_distributed_local/pkg/idle"
)

// Worker manages the prima worker process
type Worker struct {
	cfg       *config.Config
	cmd       *exec.Cmd
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	idleMon   *idle.Monitor
	running   bool
	mu        sync.RWMutex
}

// NewWorker creates a new worker
func NewWorker(cfg *config.Config) (*Worker, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create idle monitor if needed
	var idleMon *idle.Monitor
	if cfg.Node.Schedule.Enabled && cfg.Node.Schedule.IdleDetection {
		idleCfg := idle.DefaultConfig()
		idleCfg.Threshold = time.Duration(cfg.Node.Schedule.IdleMinutes) * time.Minute
		idleCfg.Enabled = cfg.Node.Schedule.IdleDetection

		var err error
		idleMon, err = idle.NewMonitor(idleCfg)
		if err != nil {
			// Idle detection failed, but we can still run
			fmt.Printf("Warning: idle detection unavailable: %v\n", err)
		}
	}

	return &Worker{
		cfg:     cfg,
		ctx:     ctx,
		cancel:  cancel,
		idleMon: idleMon,
	}, nil
}

// Start starts the worker
func (w *Worker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return fmt.Errorf("worker already running")
	}

	// Get prima binary path
	primaPath, err := w.getPrimaBinary()
	if err != nil {
		return err
	}

	// Build arguments
	args := []string{
		"--model", w.getModelPath(),
		"--head", w.cfg.Cluster.HeadNode,
		"--threads", fmt.Sprintf("%d", w.cfg.Node.Resources.CPUPercent),
	}

	if w.cfg.Node.Resources.GPULayers > 0 {
		args = append(args, "--n-gpu-layers", fmt.Sprintf("%d", w.cfg.Node.Resources.GPULayers))
	}

	// Add any extra args from config
	// TODO: Parse w.cfg.Prima.Args

	// Create command
	w.cmd = exec.CommandContext(w.ctx, primaPath, args...)
	w.cmd.Stdout = os.Stdout
	w.cmd.Stderr = os.Stderr

	// Set resource limits on Unix
	if w.cfg.Node.Resources.MemoryGB > 0 {
		w.setMemoryLimit()
	}

	// Start the process
	if err := w.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start prima: %w", err)
	}

	w.running = true

	// Start monitoring goroutine
	w.wg.Add(1)
	go w.monitor()

	// Start idle detection if configured
	if w.idleMon != nil && w.cfg.Node.Schedule.IdleDetection {
		w.wg.Add(1)
		go w.idleLoop()
	}

	return nil
}

// Stop stops the worker
func (w *Worker) Stop() error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	w.mu.Unlock()

	// Cancel context
	w.cancel()

	// Graceful shutdown
	if w.cmd != nil && w.cmd.Process != nil {
		// Try graceful shutdown first
		w.cmd.Process.Signal(syscall.SIGTERM)

		// Wait up to 10 seconds
		done := make(chan error, 1)
		go func() {
			done <- w.cmd.Wait()
		}()

		select {
		case <-done:
			// Clean exit
		case <-time.After(10 * time.Second):
			// Force kill
			w.cmd.Process.Kill()
		}
	}

	// Wait for goroutines
	w.wg.Wait()

	return nil
}

// IsRunning returns true if worker is running
func (w *Worker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// monitor watches the process and restarts if needed
func (w *Worker) monitor() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		if w.cmd == nil {
			time.Sleep(time.Second)
			continue
		}

		err := w.cmd.Wait()
		if err != nil {
			fmt.Printf("Prima process exited: %v\n", err)
		}

		// Check if we should restart
		w.mu.RLock()
		shouldRestart := w.running
		w.mu.RUnlock()

		if !shouldRestart {
			return
		}

		// Restart after delay
		fmt.Println("Restarting prima in 5 seconds...")
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(5 * time.Second):
			// Restart
			w.mu.Lock()
			if w.running {
				if err := w.startPrima(); err != nil {
					fmt.Printf("Failed to restart prima: %v\n", err)
				}
			}
			w.mu.Unlock()
		}
	}
}

// idleLoop manages idle detection for middle tier
func (w *Worker) idleLoop() {
	defer w.wg.Done()

	if w.idleMon == nil {
		return
	}

	// For middle tier: start idle, wait for active, pause, repeat
	// For lightweight: similar but more aggressive

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		// Wait for system to become idle
		fmt.Println("Waiting for system to become idle...")
		if err := w.idleMon.WaitForIdle(w.ctx); err != nil {
			if err == context.Canceled {
				return
			}
			continue
		}

		// System is now idle, ensure worker is running
		fmt.Println("System idle - ensuring worker is running")
		w.mu.Lock()
		if !w.running {
			if err := w.startPrima(); err != nil {
				fmt.Printf("Failed to start on idle: %v\n", err)
			}
		}
		w.mu.Unlock()

		// Wait for system to become active
		fmt.Println("Waiting for system to become active...")
		if err := w.idleMon.WaitForActive(w.ctx); err != nil {
			if err == context.Canceled {
				return
			}
			continue
		}

		// System is now active
		if w.cfg.Node.Tier == config.TierMiddle {
			// Middle tier: pause but don't stop completely
			fmt.Println("System active - pausing worker (middle tier)")
			// TODO: Implement pause/resume
		} else {
			// Lightweight tier: stop completely
			fmt.Println("System active - stopping worker (lightweight tier)")
			w.Stop()
			return // Exit idle loop
		}
	}
}

// startPrima starts the prima process
func (w *Worker) startPrima() error {
	primaPath, err := w.getPrimaBinary()
	if err != nil {
		return err
	}

	args := []string{
		"--model", w.getModelPath(),
		"--head", w.cfg.Cluster.HeadNode,
	}

	w.cmd = exec.CommandContext(w.ctx, primaPath, args...)
	w.cmd.Stdout = os.Stdout
	w.cmd.Stderr = os.Stderr

	return w.cmd.Start()
}

// getPrimaBinary returns the path to the prima binary
func (w *Worker) getPrimaBinary() (string, error) {
	// Check config first
	// TODO: if w.cfg.Prima.BinaryPath != "" {
	//     return w.cfg.Prima.BinaryPath, nil
	// }

	// Check in data directory
	dataDir := config.DataDir()
	primaPath := filepath.Join(dataDir, "bin", "prima")
	if _, err := os.Stat(primaPath); err == nil {
		return primaPath, nil
	}

	// Check in PATH
	if path, err := exec.LookPath("prima"); err == nil {
		return path, nil
	}

	// Check for prima.cpp
	if path, err := exec.LookPath("prima.cpp"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("prima binary not found. Please install prima.cpp or configure binary_path")
}

// getModelPath returns the path to the model
func (w *Worker) getModelPath() string {
	// TODO: Check w.cfg.Prima.ModelPath
	// For now, return a placeholder
	return "/path/to/model.gguf"
}

// setMemoryLimit sets OS-specific memory limits
func (w *Worker) setMemoryLimit() {
	// Unix-specific: setrlimit
	// This is a no-op stub - actual implementation would use syscall.Setrlimit
}
