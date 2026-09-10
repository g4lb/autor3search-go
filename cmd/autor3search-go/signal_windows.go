//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/autor3search/go/internal/winjob"
)

// evalCleansUpAfterItself is false here, and that single fact is what the
// rest of this file is about. On unix `stop -force` asks eval to shut
// itself down and eval removes its own pid file on the way out. Windows has
// no signal to ask with, so -force ends the eval outright and the pid file
// it was holding is left for the stopping process to clear.
const evalCleansUpAfterItself = false

// evalJob is eval's own job object, held for the process's lifetime.
//
// A package-level variable because the handle must outlive
// registerProcessTree: while it is open the job is guaranteed to exist for
// `stop -force` to find by name, and eval's exit is what closes it.
var evalJob *winjob.Job

// registerProcessTree puts this eval, and therefore everything it starts,
// into a job object named after its pid.
//
// This is what makes -force possible at all. Windows can terminate a
// process by pid readily enough, but doing that to eval would orphan the
// `go test` benchmark binary running underneath it — the exact outcome
// -force exists to prevent, and the reason this platform refused to
// implement -force before. A job is the one handle that reaches the whole
// tree at once.
//
// Kill-on-close is deliberately NOT set: the job outliving eval by the
// moment it takes the handle to close is harmless, and a job that killed
// its members the instant a stray handle closed would be a foot-gun aimed
// at the eval itself.
func registerProcessTree() error {
	job, err := winjob.Create(winjob.Name(os.Getpid()), false)
	if err != nil {
		return err
	}
	if err := job.AssignSelf(); err != nil {
		job.Close()
		return err
	}
	evalJob = job
	return nil
}

// termEval ends the eval at pid, together with every process it started.
//
// There is no polite half on this platform. SIGTERM is what lets an eval on
// unix cancel its own context, tear down its benchmark subprocesses and
// record what it abandoned; Windows offers a process no equivalent it can
// receive while it is busy running a benchmark. So -force here is what its
// name says and what `status` advertises — "stop now, abandoning it" —
// rather than a request. The stop REQUEST is already written by the time
// this is called, so the polite path remains `stop` without -force, which
// works the same on every platform.
//
// Terminating the job rather than the process is the part that matters: it
// reaches the compiled benchmark binary as well as eval, so nothing is left
// behind burning CPU and corrupting later measurements.
func termEval(pid int) error {
	job, err := winjob.Open(winjob.Name(pid))
	if errors.Is(err, winjob.ErrNotFound) {
		return fmt.Errorf("eval (pid %d) did not register a process tree, so it cannot be "+
			"force-stopped without orphaning its benchmark binary. "+
			"The stop request was written, so it will exit the loop at its next verdict; "+
			"interrupt it with Ctrl+C to stop it sooner", pid)
	}
	if err != nil {
		return fmt.Errorf("open eval process tree (pid %d): %w", pid, err)
	}
	defer job.Close()
	if err := job.Terminate(); err != nil {
		return fmt.Errorf("terminate eval process tree (pid %d): %w", pid, err)
	}
	return nil
}

// killEvalGroup is the escalation, and on this platform it is the same call
// as termEval: there was no gentler first attempt for it to escalate from.
// It stays distinct because the shared flow in cmd_stop.go reaches for it
// when the claim is still held after the grace period, and repeating the
// termination is the right answer to that — a job whose members are already
// gone terminates harmlessly.
func killEvalGroup(pid int) error { return termEval(pid) }
