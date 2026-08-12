//go:build !windows

package embedded

import (
	"os"
	"syscall"
)

// processAlive reports whether pid names a live process.
//
// Signal 0 sends nothing; the kernel still validates the pid and permissions,
// so this is the portable Unix idiom for a liveness probe (`kill(pid, 0)`).
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}
