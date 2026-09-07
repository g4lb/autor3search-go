//go:build windows

package runner

import (
	"os/exec"

	"github.com/g4lb/autor3search-go/internal/winjob"
)

// procGroup is a Windows job object standing in for a process group.
//
// The two differ in when membership begins, and that is the whole shape of
// this file. A process group is inherited at CreateProcess; a job has to be
// joined after the fact, so newProcGroup makes the job, attach puts the
// started child in it, and everything the child starts from then on is in
// the job too. Kill-on-close covers the rest: even a path that never
// cancels takes the tree down when close runs.
type procGroup struct {
	job *winjob.Job
	err error
}

// newProcGroup creates the job and makes context cancellation terminate it.
//
// A job that cannot be created is remembered rather than raised: the
// command is still worth running without the guarantee, and attach is where
// the caller finds out, in one place, that this run's timeouts may leak a
// benchmark binary.
func newProcGroup(cmd *exec.Cmd) *procGroup {
	g := &procGroup{}
	job, err := winjob.Create("", true)
	if err != nil {
		g.err = err
		return g
	}
	g.job = job
	cmd.Cancel = func() error {
		if g.job == nil {
			return nil
		}
		// Terminating the job kills `go` and the compiled benchmark binary
		// it exec'd together, which is what the unix implementation gets
		// from signalling a negative pid.
		return g.job.Terminate()
	}
	return g
}

// attach puts the started child into the job. It must be called after Start
// and before the command is left to run.
func (g *procGroup) attach(cmd *exec.Cmd) error {
	if g.err != nil {
		return g.err
	}
	if g.job == nil || cmd.Process == nil {
		return nil
	}
	return g.job.AssignPID(cmd.Process.Pid)
}

// close releases the job handle, killing anything still inside it.
func (g *procGroup) close() {
	if g.job != nil {
		g.job.Close()
		g.job = nil
	}
}
