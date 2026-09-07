//go:build !unix && !windows

package main

import "fmt"

// evalCleansUpAfterItself is moot where nothing can be signalled at all,
// and true is the conservative reading: never remove a pid file this
// process did not see released.
const evalCleansUpAfterItself = true

// registerProcessTree has nothing to register: there is no process group
// and no job object to put an eval in.
func registerProcessTree() error { return nil }

// errForceUnsupported explains why only the forcing half of `stop` is
// missing here. There is no SIGTERM to ask eval to cancel its own context
// with, and killing it outright would orphan the `go test` benchmark binary
// (see the unix implementation for why that matters). The stop request is
// written before either of these is ever called, so the graceful path works
// on every platform.
func errForceUnsupported(pid int) error {
	return fmt.Errorf("stop -force is not supported on this platform: "+
		"interrupt the running eval (pid %d) with Ctrl+C instead. "+
		"The stop request was written, so the agent will exit the loop at its next verdict", pid)
}

func termEval(pid int) error      { return errForceUnsupported(pid) }
func killEvalGroup(pid int) error { return errForceUnsupported(pid) }
