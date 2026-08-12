package embedded

import (
	"path/filepath"
	"strings"
)

// processIsPostgresFor reports whether the LIVE process pid is a Postgres
// postmaster serving dataDir.
//
// A pid file records only what was true when it was written. After a hard exit
// the file survives, and if the OS reuses that pid the file describes a process
// that no longer exists. Signalling on the file alone can hit a stranger (#601).
//
// The actual check is platform-specific: see processidentity_unix.go (argv via
// `ps`, which behaves the same on macOS and Linux and needs no platform-specific
// clock handling) and processidentity_windows.go (executable path, since
// Windows has no portable way to read another process's argv). Kept as a var,
// not a plain function, so tests can substitute it (see
// postgres_ownership_test.go).
var processIsPostgresFor = processIsPostgresForPID

// postgresImagePathMatches is the pure decision behind the Windows identity
// check: does a live process's executable path match the postgres binary
// expected for a given data directory. Factored out of
// processidentity_windows.go so it is testable without OpenProcess/a real
// Windows process handle -- the comparison itself has nothing OS-specific
// about it once both paths are strings.
func postgresImagePathMatches(imagePath, wantPath string) bool {
	if imagePath == "" || wantPath == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(imagePath), filepath.Clean(wantPath))
}
