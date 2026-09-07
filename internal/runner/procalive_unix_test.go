//go:build unix

package runner

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid still exists. Signal 0 performs the
// permission and existence checks without delivering anything.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	// EPERM means it exists and belongs to someone else — alive, for this
	// test's purposes.
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

func killProcess(pid int) { syscall.Kill(pid, syscall.SIGKILL) }
