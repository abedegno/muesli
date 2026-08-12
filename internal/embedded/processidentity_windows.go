//go:build windows

package embedded

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// processIsPostgresForPID reports whether the LIVE Windows process pid is the
// Postgres postmaster serving dataDir.
//
// Windows has no `ps`/argv equivalent that is both portable and documented:
// reading another process's full command line needs NtQueryInformationProcess,
// which is undocumented and awkward (see issue #647). QueryFullProcessImageName
// gives the executable path instead, which does not carry the "-D <dataDir>"
// launch argument Unix's argv check relies on -- and by itself that is not
// enough: when MUESLI_EMBEDDED_PG_BINARIES points multiple data directories at
// one shared binaries root, every one of them resolves to the SAME expected
// postgres.exe path, so path comparison alone cannot tell "postgres serving
// THIS dataDir" from "postgres serving a DIFFERENT dataDir under the same
// binaries root" -- a false positive here is not cosmetic, since callers act
// on it by sending SIGINT/TerminateProcess. This function therefore uses two
// different strategies depending on how much we know about pid:
//
//  1. If THIS process itself started the postmaster at pid for dataDir, that
//     was recorded (recordWindowsOwnedProcess, called from postgres.go right
//     after Start() succeeds) together with its creation time. Comparing pid's
//     current creation time against that recorded value is unambiguous
//     identity regardless of a shared binaries root or pid reuse: Windows does
//     not reuse a pid while anything still references the underlying process
//     object, and two distinct process creations do not coincidentally share a
//     100-nanosecond FILETIME. This is the path used by stopOwnPostmaster and
//     forceKill in postgres.go, which are the two call sites that actually
//     signal/kill a process on a positive match.
//
//  2. Otherwise pid was read only from postmaster.pid on disk -- an orphan
//     left by a previous, now-gone instance (reapOrphanPostgres) or the
//     fallback path in postmasterStillRunning. There is no local record for
//     it, so identity falls back to the executable-path comparison, PLUS a
//     second, independent signal: postmaster.pid's own recorded start
//     timestamp (line 3) cross-checked against the live process's actual
//     creation time (windowsForeignIdentityMatches). Two data directories
//     sharing a binaries root still resolve to the same path, but their
//     postmasters were started at different instants, so the timestamps
//     discriminate where the path alone cannot. This remains weaker than (1)
//     -- a coincidental match within the tolerance window is not literally
//     impossible -- but it is a real, checked second signal, not a
//     documented-and-ignored gap.
func processIsPostgresForPID(pid int, dataDir string) bool {
	if pid <= 0 || dataDir == "" {
		return false
	}

	if rec, ok := lookupWindowsOwnedProcess(dataDir); ok && rec.pid == pid {
		current, err := processCreationTimeNanos(pid)
		return windowsOwnedIdentityMatches(err, rec.creationNanos, current)
	}

	imagePath, err := processImagePath(pid)
	if err != nil {
		return false // cannot establish identity -> do not signal
	}
	wantPath := filepath.Join(installRootForDataDir(dataDir), "bin", "postgres.exe")
	pathMatches := postgresImagePathMatches(imagePath, wantPath)

	recordedStart, startErr := postmasterRecordedStartTime(dataDir)
	if startErr != nil {
		return false // cannot establish the second signal -> do not signal
	}
	creationNanos, err := processCreationTimeNanos(pid)
	if err != nil {
		return false
	}
	actualCreation := time.Unix(0, creationNanos)

	return windowsForeignIdentityMatches(pathMatches, recordedStart, actualCreation, windowsStartTimeTolerance)
}

// processImagePath returns the full path of the executable backing the live
// process pid, via QueryFullProcessImageName -- the Windows API for reading
// another process's executable path without needing SeDebugPrivilege.
func processImagePath(pid int) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// processCreationTimeNanos returns the live process pid's creation time, as
// Unix-epoch nanoseconds, via GetProcessTimes. This is the OS input to both
// windowsOwnedIdentityMatches (exact-match path) and the foreign-pid
// start-time cross-check.
func processCreationTimeNanos(pid int) (int64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	return creation.Nanoseconds(), nil
}

// postmasterRecordedStartTime reads and parses line 3 of dataDir's
// postmaster.pid -- the epoch-seconds timestamp Postgres itself records for
// when it started -- for the foreign-pid identity cross-check.
//
// Deliberately independent of readPostmasterInfo (shared, postgres.go) rather
// than extending its return values: this is a Windows-only defense-in-depth
// signal, and keeping it self-contained means it cannot affect Unix behavior
// or Unix's existing tests at all.
func postmasterRecordedStartTime(dataDir string) (time.Time, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, postmasterPIDFile))
	if err != nil {
		return time.Time{}, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		return time.Time{}, fmt.Errorf("postmaster pid: too few lines for a start timestamp")
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(lines[2]), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("postmaster pid: invalid start timestamp: %w", err)
	}
	return time.Unix(secs, 0), nil
}

// windowsOwnedProcess records the pid and creation time of a postmaster THIS
// process itself started for a given data directory (see
// recordWindowsOwnedProcess).
type windowsOwnedProcess struct {
	pid           int
	creationNanos int64
}

// windowsOwnedProcesses is process-local, in-memory bookkeeping: it maps a
// data directory to the postmaster this process itself launched for it, so
// processIsPostgresForPID can use exact creation-time identity instead of
// falling back to the weaker, shared-binaries-root-ambiguous executable-path
// comparison for pids we started ourselves. It intentionally does not survive
// a process restart -- after a crash, a relaunched app has no such record and
// correctly falls back to the foreign-pid path when reaping an orphan.
var windowsOwnedProcesses = struct {
	mu sync.Mutex
	m  map[string]windowsOwnedProcess
}{m: make(map[string]windowsOwnedProcess)}

// recordWindowsOwnedProcess notes that this process itself just started pid
// as the postmaster for dataDir. Called from postgres.go's start() right
// after Start() succeeds and the pid is read back, i.e. the one moment this
// process can be completely certain of the pid's identity.
//
// Best-effort: if the creation time cannot be read, dataDir is simply left
// unrecorded, and processIsPostgresForPID falls back to the foreign-pid path
// (executable path + start-time cross-check) for it instead of failing.
func recordWindowsOwnedProcess(dataDir string, pid int) {
	creationNanos, err := processCreationTimeNanos(pid)
	if err != nil {
		return
	}
	key := filepath.Clean(dataDir)
	windowsOwnedProcesses.mu.Lock()
	windowsOwnedProcesses.m[key] = windowsOwnedProcess{pid: pid, creationNanos: creationNanos}
	windowsOwnedProcesses.mu.Unlock()
}

// forgetWindowsOwnedProcess drops any record for dataDir. Called from
// postgres.go's Stop() once this instance no longer claims a pid for dataDir.
func forgetWindowsOwnedProcess(dataDir string) {
	key := filepath.Clean(dataDir)
	windowsOwnedProcesses.mu.Lock()
	delete(windowsOwnedProcesses.m, key)
	windowsOwnedProcesses.mu.Unlock()
}

func lookupWindowsOwnedProcess(dataDir string) (windowsOwnedProcess, bool) {
	key := filepath.Clean(dataDir)
	windowsOwnedProcesses.mu.Lock()
	defer windowsOwnedProcesses.mu.Unlock()
	rec, ok := windowsOwnedProcesses.m[key]
	return rec, ok
}
