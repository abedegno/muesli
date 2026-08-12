//go:build windows

package embedded

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// processIsPostgresForPID reports whether the LIVE Windows process pid is the
// Postgres postmaster serving dataDir.
//
// Windows has no `ps`/argv equivalent that is both portable and documented:
// reading another process's full command line needs NtQueryInformationProcess,
// which is undocumented and awkward (see issue #647). QueryFullProcessImageName
// gives the executable path instead, which does not carry the "-D <dataDir>"
// launch argument Unix's argv check relies on. So identity here is
// approximated by path: the postgres binary that would serve dataDir lives at
// installRootForDataDir(dataDir)+"\bin\postgres.exe", and each data directory
// normally gets its own extracted runtime tree (runtimePathForDataDir), so
// that path is unique per data directory and this discriminates cleanly in the
// common case.
//
// The one case it cannot discriminate is two data directories sharing a single
// MUESLI_EMBEDDED_PG_BINARIES override, where both would resolve to the same
// expected path. That is an accepted residual limit of a path-based signal --
// nothing shipped a Windows implementation before this, so it is not a
// regression -- in the same spirit as the pid-reuse residual TOCTOU already
// documented around stopOwnPostmaster/forceKill in postgres.go and tracked
// separately in #645.
func processIsPostgresForPID(pid int, dataDir string) bool {
	if pid <= 0 || dataDir == "" {
		return false
	}
	imagePath, err := processImagePath(pid)
	if err != nil {
		return false // cannot establish identity -> do not signal
	}
	wantPath := filepath.Join(installRootForDataDir(dataDir), "bin", "postgres.exe")
	return postgresImagePathMatches(imagePath, wantPath)
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
