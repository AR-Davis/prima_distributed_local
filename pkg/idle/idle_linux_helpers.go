//go:build linux
// +build linux

package idle

import (
	"os/exec"
)

// execLookPathImpl wraps exec.LookPath
func execLookPathImpl(name string) (string, error) {
	return exec.LookPath(name)
}

// runCommandImpl wraps exec.Command
func runCommandImpl(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.Output()
}
