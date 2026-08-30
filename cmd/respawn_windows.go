//go:build windows

package main

import "os"

// respawnProcess starts a replacement process after a settings change.
// Windows does not support syscall.Exec, so the old process exits after the
// replacement has inherited its terminal handles.
func respawnProcess() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}

	proc, err := os.StartProcess(executable, append([]string{executable}, os.Args[1:]...), &os.ProcAttr{
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		return err
	}

	return proc.Release()
}
