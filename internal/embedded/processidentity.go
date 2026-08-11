package embedded

import (
	"os/exec"
	"strconv"
	"strings"
)

// processIsPostgresFor reports whether the LIVE process pid is a Postgres
// postmaster serving dataDir.
//
// A pid file records only what was true when it was written. After a hard exit
// the file survives, and if the OS reuses that pid the file describes a process
// that no longer exists. Signalling on the file alone can hit a stranger (#601).
//
// argv rather than the pid file's recorded start time: `ps -o args=` behaves the
// same on macOS and Linux, and Postgres records its data directory in its own
// command line, so no platform-specific clock handling is needed.
var processIsPostgresFor = processIsPostgresForPID

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
