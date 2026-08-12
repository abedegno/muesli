//go:build !windows

package embedded

import (
	"os/exec"
	"strconv"
	"strings"
)

// processIsPostgresForPID reports whether the LIVE process pid is a Postgres
// postmaster serving dataDir, by inspecting its command line.
//
// `ps -o args=` behaves the same on macOS and Linux, and Postgres records its
// data directory in its own command line, so no platform-specific clock
// handling is needed.
func processIsPostgresForPID(pid int, dataDir string) bool {
	if pid <= 0 || dataDir == "" {
		return false
	}
	out, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false // cannot establish identity -> do not signal
	}
	args := strings.TrimSpace(string(out))
	if args == "" {
		return false
	}
	return strings.Contains(args, "postgres") && strings.Contains(args, dataDir)
}
