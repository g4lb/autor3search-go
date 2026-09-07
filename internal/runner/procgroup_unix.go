//go:build unix

package runner

import (
	"errors"
	"os/exec"
	"syscall"
)

// procGroup is the handle on a command's process tree. On unix the kernel
// keeps the tree for us — every descendant inherits the process group id —
// so there is nothing to carry between the calls.
type procGroup struct{}

// newProcGroup puts the child in its own process group and makes context
// cancellation kill the entire group.
//
// go test execs the compiled test binary as a grandchild. Killing only the
// direct child leaves that binary running: exec.CommandContext's default
// cancel signals one pid, not the group. A leaked benchmark process would
// keep consuming CPU and corrupt every later measurement on this machine.
func newProcGroup(cmd *exec.Cmd) *procGroup {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// A negative pid signals the whole process group.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil &&
			!errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	return &procGroup{}
}

// attach has nothing to do: Setpgid took effect inside the child before it
// ran a single instruction, which is the property the windows
// implementation has to work to approximate.
func (g *procGroup) attach(cmd *exec.Cmd) error { return nil }

// close has nothing to release.
func (g *procGroup) close() {}
