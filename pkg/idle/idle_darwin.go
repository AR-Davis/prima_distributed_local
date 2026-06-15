//go:build darwin
// +build darwin

package idle

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// darwinDetector implements idle detection for macOS
type darwinDetector struct{}

func newDarwinDetector() (Detector, error) {
	return &darwinDetector{}, nil
}

// TimeSinceInput returns milliseconds since last user input
func (d *darwinDetector) TimeSinceInput() (time.Duration, error) {
	// Try IOKit first (most accurate)
	return d.getIOKitIdleTime()
}

// IsScreenLocked returns true if screen is locked
func (d *darwinDetector) IsScreenLocked() (bool, error) {
	// Check if CGSessionCopyCurrentDictionary returns null
	// or if the screen saver is running

	// Method 1: Check if loginwindow is active
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err == nil {
		// If loginwindow is the frontmost app, screen is locked
		if strings.Contains(string(output), "loginwindow") {
			// Check if it's actually the frontmost app
			return d.checkScreenLockedViaCGSession()
		}
	}

	return false, nil
}

// getIOKitIdleTime gets idle time using IOKit HIDIdleTime
func (d *darwinDetector) getIOKitIdleTime() (time.Duration, error) {
	// Use ioreg to read HIDIdleTime
	cmd := exec.Command("ioreg", "-c", "IOHIDSystem")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ioreg failed: %w", err)
	}

	// Parse HIDIdleTime from output
	// Format: "HIDIdleTime" = 12345678901234567890
	re := regexp.MustCompile(`"HIDIdleTime"\s*=\s*(\d+)`)
	matches := re.FindStringSubmatch(string(output))
	
	if len(matches) < 2 {
		return 0, fmt.Errorf("HIDIdleTime not found")
	}

	// HIDIdleTime is in nanoseconds
	nanos, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse HIDIdleTime: %w", err)
	}

	return time.Duration(nanos) * time.Nanosecond, nil
}

// checkScreenLockedViaCGSession checks if screen is locked via CoreGraphics
func (d *darwinDetector) checkScreenLockedViaCGSession() (bool, error) {
	// Use Python to check CGSession
	script := `
import Quartz
print(Quartz.CGSessionCopyCurrentDictionary())
`
	cmd := exec.Command("python3", "-c", script)
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	// If output contains "OnConsoleKey = 0" or doesn't contain "kCGSessionOnConsoleKey"
	// then the screen is locked
	outputStr := string(output)
	if strings.Contains(outputStr, "kCGSessionOnConsoleKey") {
		return false, nil // Screen is not locked
	}
	return true, nil // Screen is locked
}

// Alternative using pmset (less accurate but always available)
func (d *darwinDetector) getPMSetIdleTime() (time.Duration, error) {
	// Get last wake time and calculate idle from that
	cmd := exec.Command("pmset", "-g", "log")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Parse the output for last wake time
	// This is a simplified approach
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Wake") {
			// Found a wake event, but we need idle since last input
			// This method is not very accurate
			break
		}
	}

	return 0, fmt.Errorf("pmset method not fully implemented")
}
