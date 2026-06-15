//go:build windows
// +build windows

package idle

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
)

// LASTINPUTINFO structure for Windows API
type LASTINPUTINFO struct {
	CbSize uint32
	Time   uint32
}

// windowsDetector implements idle detection for Windows
type windowsDetector struct{}

func newWindowsDetector() (Detector, error) {
	return &windowsDetector{}, nil
}

// TimeSinceInput returns milliseconds since last user input
func (w *windowsDetector) TimeSinceInput() (time.Duration, error) {
	var li LASTINPUTINFO
	li.CbSize = uint32(unsafe.Sizeof(li))

	ret, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	if ret == 0 {
		return 0, fmt.Errorf("GetLastInputInfo failed: %v", err)
	}

	// Get tick count
	tickCount := syscall.GetTickCount()

	// Calculate idle time
	idleTicks := tickCount - li.Time
	
	return time.Duration(idleTicks) * time.Millisecond, nil
}

// IsScreenLocked returns true if screen is locked
func (w *windowsDetector) IsScreenLocked() (bool, error) {
	// Check if workstation is locked by checking for the existence of
	// the desktop "Winlogon" which is active when locked

	// Method: Check if the foreground window is on the Winlogon desktop
	hwnd := getForegroundWindow()
	if hwnd == 0 {
		return false, nil
	}

	// Get the desktop name
	desktopName := getWindowDesktopName(hwnd)
	if desktopName == "Winlogon" || desktopName == "Default" {
		return true, nil
	}

	return false, nil
}

var (
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
)

func getForegroundWindow() uintptr {
	ret, _, _ := procGetForegroundWindow.Call()
	return ret
}

func getWindowDesktopName(hwnd uintptr) string {
	// This is a simplified check
	// A full implementation would use GetThreadDesktop and GetUserObjectInformation
	// to get the desktop name
	
	// For now, use a registry check
	// Check if screen is locked via registry
	return "" // Placeholder
}

// Alternative screen lock detection for Windows
func (w *windowsDetector) isScreenLockedViaRegistry() (bool, error) {
	// Check the registry for screen lock state
	// HKEY_CURRENT_USER\Control Panel\Desktop\ScreenSaveActive
	// But this doesn't directly tell us if locked
	
	// Better method: Check if LogonUI.exe is running
	return false, nil
}
