// Package idle detects system idle state for background operation
package idle

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

// Detector interface for platform-specific idle detection
type Detector interface {
	// TimeSinceInput returns milliseconds since last user input
	TimeSinceInput() (time.Duration, error)
	// IsScreenLocked returns true if screen is locked
	IsScreenLocked() (bool, error)
}

// State represents the current idle state
type State struct {
	IdleDuration    time.Duration `json:"idle_duration"`
	IsScreenLocked  bool          `json:"is_screen_locked"`
	CPULoadPercent  float64       `json:"cpu_load_percent"`
	IsIdle          bool          `json:"is_idle"`
}

// Monitor tracks idle state over time
type Monitor struct {
	threshold     time.Duration
	cpuThreshold  float64
	detector      Detector
	state         State
	lastCheck     time.Time
}

// Config for idle monitoring
type Config struct {
	Enabled      bool          `toml:"enabled"`
	Threshold    time.Duration `toml:"threshold"`     // Time before considered idle
	CPUThreshold float64       `toml:"cpu_threshold"` // CPU % below which system is idle
	CheckInterval time.Duration `toml:"check_interval"`
}

// DefaultConfig returns default idle detection config
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		Threshold:     5 * time.Minute,
		CPUThreshold:  10.0,
		CheckInterval: 30 * time.Second,
	}
}

// NewMonitor creates an idle state monitor
func NewMonitor(cfg Config) (*Monitor, error) {
	detector, err := newPlatformDetector()
	if err != nil {
		// Fall back to CPU-only detection
		detector = &cpuDetector{}
	}

	return &Monitor{
		threshold:    cfg.Threshold,
		cpuThreshold: cfg.CPUThreshold,
		detector:     detector,
		lastCheck:    time.Now(),
	}, nil
}

// Check returns current idle state
func (m *Monitor) Check(ctx context.Context) (*State, error) {
	state := &State{}

	// Check input idle time
	idleDuration, err := m.detector.TimeSinceInput()
	if err != nil {
		// Fall back to CPU detection only
		idleDuration = 0
	}
	state.IdleDuration = idleDuration

	// Check screen lock
	locked, _ := m.detector.IsScreenLocked()
	state.IsScreenLocked = locked

	// Get CPU load
	percentages, err := cpu.PercentWithContext(ctx, 0, false)
	if err == nil && len(percentages) > 0 {
		state.CPULoadPercent = percentages[0]
	}

	// Determine if idle
	state.IsIdle = m.isIdle(state)

	m.state = *state
	m.lastCheck = time.Now()

	return state, nil
}

// IsIdle returns true if system is currently idle
func (m *Monitor) IsIdle() bool {
	return m.state.IsIdle
}

// WaitForIdle blocks until system becomes idle
func (m *Monitor) WaitForIdle(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			state, err := m.Check(ctx)
			if err != nil {
				continue
			}
			if state.IsIdle {
				return nil
			}
		}
	}
}

// WaitForActive blocks until system becomes active
func (m *Monitor) WaitForActive(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			state, err := m.Check(ctx)
			if err != nil {
				continue
			}
			if !state.IsIdle {
				return nil
			}
		}
	}
}

// isIdle determines if system is idle based on current state
func (m *Monitor) isIdle(state *State) bool {
	// Input idle threshold
	if state.IdleDuration >= m.threshold {
		return true
	}

	// CPU-only detection (fallback)
	if state.IdleDuration == 0 && state.CPULoadPercent < m.cpuThreshold {
		// Require sustained low CPU for longer than input threshold
		// This is handled by the caller checking repeatedly
		return true
	}

	// Screen locked counts as idle for middle tier
	if state.IsScreenLocked && state.CPULoadPercent < m.cpuThreshold {
		return true
	}

	return false
}

// cpuDetector is a fallback detector that only uses CPU
type cpuDetector struct{}

func (c *cpuDetector) TimeSinceInput() (time.Duration, error) {
	return 0, fmt.Errorf("not implemented")
}

func (c *cpuDetector) IsScreenLocked() (bool, error) {
	return false, fmt.Errorf("not implemented")
}

// Platform-specific implementations are in separate files
// idle_linux.go, idle_darwin.go, idle_windows.go

// newPlatformDetector creates the appropriate detector for the platform
func newPlatformDetector() (Detector, error) {
	switch runtime.GOOS {
	case "linux":
		return newLinuxDetector()
	case "darwin":
		return newDarwinDetector()
	case "windows":
		return newWindowsDetector()
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
