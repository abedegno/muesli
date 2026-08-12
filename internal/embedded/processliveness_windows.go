//go:build windows

package embedded

import "golang.org/x/sys/windows"

// processAlive reports whether pid names a live process.
//
// OpenProcess + GetExitCodeProcess is the Windows analogue of the Unix
// `kill(pid, 0)` probe in processliveness_unix.go: it does not signal the
// process, it just asks the kernel whether the handle's target has produced an
// exit code yet -- STILL_ACTIVE means it has not. As with the Unix check, a pid
// can be recycled between the caller capturing it and this check running; that
// TOCTOU class is tracked in #645 and is out of scope here, the same residual
// risk already documented around stopOwnPostmaster/forceKill in postgres.go.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	err = windows.GetExitCodeProcess(h, &exitCode)
	return windowsProcessAlive(err, exitCode)
}
