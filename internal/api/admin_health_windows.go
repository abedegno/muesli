//go:build windows

package api

import "golang.org/x/sys/windows"

// buildStorageDiskUsage stats the storage dir's filesystem. A stat error is
// captured on this section only.
func buildStorageDiskUsage(path string) StorageDiskUsage {
	out := StorageDiskUsage{Path: path}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &out.FreeBytes, &out.TotalBytes, nil); err != nil {
		out.Error = err.Error()
		out.TotalBytes = 0
		out.FreeBytes = 0
	}
	return out
}
