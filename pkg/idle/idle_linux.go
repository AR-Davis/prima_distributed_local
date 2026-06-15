//go:build linux
// +build linux

package idle

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// linuxDetector implements idle detection for Linux
type linuxDetector struct {
	sessionType string // x11, wayland, tty
}

func newLinuxDetector() (Detector, error) {
	// Detect session type
	sessionType := os.Getenv("XDG_SESSION_TYPE")
	if sessionType == "" {
		// Check for Wayland
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			sessionType = "wayland"
		} else if os.Getenv("DISPLAY") != "" {
			sessionType = "x11"
		} else {
			sessionType = "tty"
		}
	}

	return &linuxDetector{sessionType: sessionType}, nil
}

// TimeSinceInput returns milliseconds since last user input
func (l *linuxDetector) TimeSinceInput() (time.Duration, error) {
	switch l.sessionType {
	case "x11":
		return l.getX11IdleTime()
	case "wayland":
		return l.getWaylandIdleTime()
	case "tty":
		return l.getTTYIdleTime()
	default:
		return 0, fmt.Errorf("unknown session type: %s", l.sessionType)
	}
}

// IsScreenLocked returns true if screen is locked
func (l *linuxDetector) IsScreenLocked() (bool, error) {
	// Check for common screen lockers
	lockFiles := []string{
		"/var/run/screenlocked",          // Custom
		"/tmp/screenlocked",              // Custom
		os.ExpandEnv("$HOME/.screenlocked"), // Custom
	}

	for _, f := range lockFiles {
		if _, err := os.Stat(f); err == nil {
			return true, nil
		}
	}

	// Check for gnome-screensaver, xscreensaver, etc.
	// This is a simplified check
	if l.sessionType == "x11" {
		return l.checkXScreenLocked()
	}

	return false, nil
}

// getX11IdleTime gets idle time from X11 using xprintidle or XScreenSaver
func (l *linuxDetector) getX11IdleTime() (time.Duration, error) {
	// Try xprintidle first (most accurate)
	if path, err := execLookPath("xprintidle"); err == nil {
		data, err := runCommand(path)
		if err == nil {
			ms, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err == nil {
				return time.Duration(ms) * time.Millisecond, nil
			}
		}
	}

	// Try using XScreenSaver info
	// This requires the XScreenSaver extension
	return l.getXSSIdleTime()
}

// getXSSIdleTime gets idle time from XScreenSaver extension
func (l *linuxDetector) getXSSIdleTime() (time.Duration, error) {
	display := os.Getenv("DISPLAY")
	if display == "" {
		return 0, fmt.Errorf("DISPLAY not set")
	}

	// Use xssstate or parse XScreenSaver output
	if path, err := execLookPath("xssstate"); err == nil {
		data, err := runCommand(path, "-i")
		if err == nil {
			// Parse "idle: 12345" output
			fields := strings.Fields(string(data))
			for i, f := range fields {
				if f == "idle:" && i+1 < len(fields) {
					ms, err := strconv.ParseInt(fields[i+1], 10, 64)
					if err == nil {
						return time.Duration(ms) * time.Millisecond, nil
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("no X11 idle detection method available")
}

// getWaylandIdleTime gets idle time from Wayland compositor
func (l *linuxDetector) getWaylandIdleTime() (time.Duration, error) {
	// Try swayidle or other Wayland idle monitors
	// This is limited as Wayland doesn't have a standard idle protocol

	// Check for KDE on Wayland
	if os.Getenv("KDE_FULL_SESSION") != "" {
		return l.getKDEIdleTime()
	}

	// Check for GNOME on Wayland
	if os.Getenv("GNOME_DESKTOP_SESSION_ID") != "" {
		return l.getGNOMEIdleTime()
	}

	return 0, fmt.Errorf("Wayland idle detection not available")
}

// getTTYIdleTime gets idle time from TTY
func (l *linuxDetector) getTTYIdleTime() (time.Duration, error) {
	// For TTY, use /dev/input event timestamps
	// or check last activity in /var/log

	// Get the most recent activity from input devices
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

// getKDEIdleTime gets idle time from KDE/Plasma
func (l *linuxDetector) getKDEIdleTime() (time.Duration, error) {
	// Try qdbus or dbus-send
	if path, err := execLookPath("qdbus"); err == nil {
		data, err := runCommand(path, "org.kde.ScreenSaver", "/ScreenSaver", "GetSessionIdleTime")
		if err == nil {
			ms, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err == nil {
				return time.Duration(ms) * time.Millisecond, nil
			}
		}
	}

	return 0, fmt.Errorf("KDE idle detection failed")
}

// getGNOMEIdleTime gets idle time from GNOME
func (l *linuxDetector) getGNOMEIdleTime() (time.Duration, error) {
	// Try gdbus
	if path, err := execLookPath("gdbus"); err == nil {
		// GNOME session manager
		data, err := runCommand(path, "call", "--session",
			"--dest", "org.gnome.SessionManager",
			"--object-path", "/org/gnome/SessionManager",
			"--method", "org.gnome.SessionManager.GetSessionIdleTime")
		if err == nil {
			// Parse output
			// Output format: "(uint64 12345,)"
			if idx := strings.Index(string(data), "uint64"); idx != -1 {
				rest := strings.TrimSpace(string(data)[idx+6:])
				rest = strings.Trim(rest, "(),")
				ms, err := strconv.ParseInt(rest, 10, 64)
				if err == nil {
					return time.Duration(ms) * time.Millisecond, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("GNOME idle detection failed")
}

// checkXScreenLocked checks if X11 screen is locked
func (l *linuxDetector) checkXScreenLocked() (bool, error) {
	// Check for xscreensaver
	if path, err := execLookPath("xscreensaver-command"); err == nil {
		data, err := runCommand(path, "-time")
		if err == nil {
			return strings.Contains(string(data), "locked"), nil
		}
	}

	// Check for gnome-screensaver
	if path, err := execLookPath("gnome-screensaver-command"); err == nil {
		data, err := runCommand(path, "-q")
		if err == nil {
			return strings.Contains(string(data), "active"), nil
		}
	}

	// Check for light-locker
	if path, err := execLookPath("light-locker-command"); err == nil {
		data, err := runCommand(path, "-q")
		if err == nil {
			return strings.Contains(string(data), "The screensaver is locked"), nil
		}
	}

	return false, fmt.Errorf("no screen lock detection available")
}

// Helper functions
func execLookPath(name string) (string, error) {
	return execLookPathImpl(name)
}

func runCommand(name string, args ...string) ([]byte, error) {
	return runCommandImpl(name, args...)
}
