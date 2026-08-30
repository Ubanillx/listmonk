//go:build !windows

package main

import (
	"os"
	"syscall"
)

// respawnProcess replaces the current process after a settings change.
func respawnProcess() error {
	return syscall.Exec(os.Args[0], os.Args, os.Environ())
}
