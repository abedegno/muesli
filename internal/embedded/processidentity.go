package embedded

import (
	"path/filepath"
	"strings"
	"time"
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
// clock handling) and processidentity_windows.go (Windows has no portable way
// to read another process's argv, so it combines an executable-path comparison
// with a process-creation-time cross-check -- see that file for why path alone
// is not sufficient). Kept as a var, not a plain function, so tests can
// substitute it (see postgres_ownership_test.go).
var processIsPostgresFor = processIsPostgresForPID

// postgresImagePathMatches is the pure decision behind one half of the Windows
// identity check: does a live process's executable path match the postgres
// binary expected for a given data directory. Factored out of
// processidentity_windows.go so it is testable without OpenProcess/a real
// Windows process handle -- the comparison itself has nothing OS-specific
// about it once both paths are strings.
func postgresImagePathMatches(imagePath, wantPath string) bool {
	if imagePath == "" || wantPath == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(imagePath), filepath.Clean(wantPath))
}

// windowsStartTimeTolerance bounds how far a live process's creation time may
// drift from the start timestamp Postgres itself recorded in postmaster.pid
// (see postmasterStartTimeMatches) and still be treated as the same process.
// It absorbs the pid file's 1-second resolution plus a small measurement gap;
// it is far smaller than any realistic gap between two unrelated postmasters
// starting, which is the discrimination this exists to provide.
const windowsStartTimeTolerance = 5 * time.Second

// postmasterStartTimeMatches is the pure decision behind the Windows
// identity check's second signal: does a live process's actual creation time
// line up with the start timestamp Postgres itself wrote into postmaster.pid.
// Factored out of processidentity_windows.go so it is testable without a real
// process or a real pid file.
func postmasterStartTimeMatches(recorded, actual time.Time, tolerance time.Duration) bool {
	if recorded.IsZero() || actual.IsZero() {
		return false
	}
	diff := actual.Sub(recorded)
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// windowsForeignIdentityMatches is the pure decision behind the Windows
// identity check for a pid this process did NOT itself start (read only from
// postmaster.pid on disk -- reapOrphanPostgres, and the fallback path in
// postmasterStillRunning). Executable path alone cannot discriminate between
// two data directories that share one MUESLI_EMBEDDED_PG_BINARIES binaries
// root: both resolve to the identical expected postgres.exe path. The
// start-time cross-check is the second, independent signal that tells them
// apart, since two different data directories' postmasters were started at
// different instants even when they run the same shared binary.
func windowsForeignIdentityMatches(pathMatches bool, recordedStart, actualCreation time.Time, tolerance time.Duration) bool {
	if !pathMatches {
		return false
	}
	return postmasterStartTimeMatches(recordedStart, actualCreation, tolerance)
}

// windowsOwnedIdentityMatches is the pure decision behind the Windows
// identity check for a pid THIS process itself started (recorded via
// recordWindowsOwnedProcess right after Start() succeeded, looked up by
// dataDir in processidentity_windows.go). Comparing the process's creation
// time -- captured once, at the moment we are certain of its identity --
// against what is live now is unambiguous regardless of pid reuse or a
// shared MUESLI_EMBEDDED_PG_BINARIES binaries root: a pid file cannot record
// a stale/reused pid's creation time as matching ours by coincidence at
// 100-nanosecond FILETIME resolution.
func windowsOwnedIdentityMatches(err error, recordedCreationNanos, currentCreationNanos int64) bool {
	return err == nil && recordedCreationNanos == currentCreationNanos
}
