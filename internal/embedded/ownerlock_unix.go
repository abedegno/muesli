//go:build !windows

package embedded

import (
	"os"
	"syscall"
)

// flock is the right primitive on Unix: the kernel releases it when the holder
// dies however it dies, which is exactly the case being defended against.
func tryLockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

func isLockBusy(err error) bool {
	return err == syscall.EWOULDBLOCK || err == syscall.EAGAIN
}
