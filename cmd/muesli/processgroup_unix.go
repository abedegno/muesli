//go:build !windows

package main

import "syscall"

func configureEmbeddedProcessGroup() error {
	return syscall.Setpgid(0, 0)
}
