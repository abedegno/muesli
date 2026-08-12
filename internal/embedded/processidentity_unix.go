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

// recordWindowsOwnedProcess and forgetWindowsOwnedProcess are Windows-only
// bookkeeping (see processidentity_windows.go) that let the Windows identity
// check discriminate by data directory even when multiple data directories
// share one MUESLI_EMBEDDED_PG_BINARIES binaries root. Unix's ps/argv check
// already discriminates by data directory (argv contains it directly), so
// these are no-ops here; they exist on this platform only so postgres.go can
// call them unconditionally from shared code.
func recordWindowsOwnedProcess(dataDir string, pid int) {}
func forgetWindowsOwnedProcess(dataDir string)          {}
