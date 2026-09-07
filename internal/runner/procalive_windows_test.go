//go:build windows

package runner

import "syscall"

// stillActive is GetExitCodeProcess's answer for a process that has not
// exited. It is why existence is not liveness here: a process that has
// exited stays openable while any handle to it remains, so the exit code is
// the question that actually distinguishes the two.
const stillActive = 259

func processAlive(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

func killProcess(pid int) {
	h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer syscall.CloseHandle(h)
	syscall.TerminateProcess(h, 1)
}
