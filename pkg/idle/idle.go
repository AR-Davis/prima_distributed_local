// Package idle detects system idle state for background operation
package idle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Detector interface for platform-specific idle detection
type Detector interface {
	TimeSinceInput() (time.Duration, error)
	IsScreenLocked() (bool, error)
}

// State represents the current idle state
type State struct {
	IdleDuration   time.Duration `json:"idle_duration"`
	IsScreenLocked bool          `json:"is_screen_locked"`
	IsIdle         bool          `json:"is_idle"`
}

// Monitor tracks idle state over time
type Monitor struct {
	threshold    time.Duration
	cpuThreshold float64
	lastCheck    time.Time
}

// Config for idle monitoring
type Config struct {
	Enabled       bool
	Threshold     time.Duration
	CPUThreshold  float64
	CheckInterval time.Duration
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
	return &Monitor{
		threshold:    cfg.Threshold,
		cpuThreshold: cfg.CPUThreshold,
		lastCheck:    time.Now(),
	}, nil
}

// Check returns current idle state
func (m *Monitor) Check(ctx context.Context) (*State, error) {
	state := &State{}

	// Platform-specific idle detection
	idleDuration, _ := getIdleTime()
	state.IdleDuration = idleDuration

	locked, _ := isScreenLocked()
	state.IsScreenLocked = locked

	// Determine if idle
	state.IsIdle = m.isIdle(state)

	m.lastCheck = time.Now()

	return state, nil
}

// IsIdle returns true if system is currently idle
func (m *Monitor) IsIdle() bool {
	return m.isIdle(&State{IdleDuration: m.threshold + 1})
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
	if state.IdleDuration >= m.threshold {
		return true
	}
	if state.IsScreenLocked {
		return true
	}
	return false
}

// getIdleTime returns idle time based on platform
func getIdleTime() (time.Duration, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxIdleTime()
	case "darwin":
		return getDarwinIdleTime()
	case "windows":
		return getWindowsIdleTime()
	default:
		return 0, fmt.Errorf("unsupported platform")
	}
}

// isScreenLocked returns screen lock status
func isScreenLocked() (bool, error) {
	switch runtime.GOOS {
	case "linux":
		return isLinuxScreenLocked()
	case "darwin":
		return isDarwinScreenLocked()
	case "windows":
		return isWindowsScreenLocked()
	default:
		return false, fmt.Errorf("unsupported platform")
	}
}

// Linux implementations
func getLinuxIdleTime() (time.Duration, error) {
	// Try xprintidle first
	if path, err := exec.LookPath("xprintidle"); err == nil {
		cmd := exec.Command(path)
		output, err := cmd.Output()
		if err == nil {
			ms, _ := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
			return time.Duration(ms) * time.Millisecond, nil
		}
	}

	// Try reading /dev/input
	return getLinuxInputIdleTime()
}

func getLinuxInputIdleTime() (time.Duration, error) {
	// Check input device timestamps
	inputPath := "/dev/input"
	entries, err := os.ReadDir(inputPath)
	if err != nil {
		return 0, err
	}

	var lastActivity time.Time
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "event") {
			info, err := os.Stat(filepath.Join(inputPath, entry.Name()))
			if err == nil {
				if info.ModTime().After(lastActivity) {
					lastActivity = info.ModTime()
				}
			}
		}
	}

	if lastActivity.IsZero() {
		return 0, fmt.Errorf("no input activity detected")
	}

	return time.Since(lastActivity), nil
}

func isLinuxScreenLocked() (bool, error) {
	// Check for gnome-screensaver
	cmd := exec.Command("gnome-screensaver-command", "-q")
	output, _ := cmd.Output()
	if strings.Contains(string(output), "active") {
		return true, nil
	}

	// Check for xscreensaver
	cmd = exec.Command("xscreensaver-command", "-time")
	output, _ = cmd.Output()
	if strings.Contains(string(output), "locked") {
		return true, nil
	}

	return false, nil
}

// Darwin implementations
func getDarwinIdleTime() (time.Duration, error) {
	// Use ioreg to get HIDIdleTime
	cmd := exec.Command("ioreg", "-c", "IOHIDSystem")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	re := regexp.MustCompile(`"HIDIdleTime"\s*=\s*(\d+)`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) < 2 {
		return 0, fmt.Errorf("HIDIdleTime not found")
	}

	nanos, _ := strconv.ParseInt(matches[1], 10, 64)
	return time.Duration(nanos) * time.Nanosecond, nil
}

func isDarwinScreenLocked() (bool, error) {
	// Check if ScreenSaverEngine is running
	cmd := exec.Command("pgrep", "ScreenSaverEngine")
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	return false, nil
}

// Windows implementations
func getWindowsIdleTime() (time.Duration, error) {
	return 0, fmt.Errorf("Windows idle detection not implemented on this platform")
}

func isWindowsScreenLocked() (bool, error) {
	return false, fmt.Errorf("Windows screen lock detection not implemented on this platform")
}
