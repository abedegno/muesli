//go:build !windows

package api

import "syscall"

// buildStorageDiskUsage stats the storage dir's filesystem. A stat error is
// captured on this section only.
func buildStorageDiskUsage(path string) StorageDiskUsage {
	out := StorageDiskUsage{Path: path}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		out.Error = err.Error()
		return out
	}
	bsize := uint64(stat.Bsize) //nolint:unconvert // Bsize's width varies by arch
	out.TotalBytes = stat.Blocks * bsize
	out.FreeBytes = stat.Bavail * bsize
	return out
}
