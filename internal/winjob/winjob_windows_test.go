//go:build windows

package winjob

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// startSleeper starts a process that will not exit on its own, and returns
// a channel that closes when it does.
//
// ping rather than timeout: it does not need a console, which a test
// process does not have.
func startSleeper(t *testing.T) (*exec.Cmd, chan error) {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "ping", "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return cmd, done
}

// mustStillBeRunning is what keeps these tests from passing vacuously. If
// the sleeper died on its own — ping missing, cmd.exe refusing the
// arguments — then every "it exited after we terminated the job" assertion
// below would be satisfied by a process nothing killed.
func mustStillBeRunning(t *testing.T, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("sleeper exited before anything terminated it (%v); "+
			"the test would have proved nothing", err)
	case <-time.After(500 * time.Millisecond):
	}
}

// TestTerminateByNameKillsTheTree walks the exact path `stop -force` takes:
// one process creates a named job and puts a running process in it, and a
// SECOND, holding nothing but the name, opens it and terminates everything
// inside. Everything about the feature that can be wrong — the name being
// derivable, the handle being openable from elsewhere, the assignment
// sticking, the termination reaching the member — is wrong here first.
func TestTerminateByNameKillsTheTree(t *testing.T) {
	name := Name(os.Getpid())
	job, err := Create(name, false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer job.Close()

	cmd, done := startSleeper(t)
	killed := false
	defer func() {
		if !killed && cmd.Process != nil {
			cmd.Process.Kill()
			<-done
		}
	}()

	if err := job.AssignPID(cmd.Process.Pid); err != nil {
		t.Fatalf("AssignPID: %v", err)
	}
	mustStillBeRunning(t, done)

	opened, err := Open(name)
	if err != nil {
		t.Fatalf("Open by name: %v", err)
	}
	defer opened.Close()
	if err := opened.Terminate(); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	select {
	case <-done:
		killed = true
	case <-time.After(15 * time.Second):
		t.Fatal("process in the terminated job is still running after 15s")
	}
}

// TestOpenReportsAMissingJob is the case `stop -force` has to tell a human
// about: an eval started before this tool registered process trees, which
// must produce a refusal naming the situation rather than a kill that
// orphans a benchmark binary.
func TestOpenReportsAMissingJob(t *testing.T) {
	_, err := Open(Name(-1))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open of a job that does not exist = %v, want ErrNotFound", err)
	}
}

// TestKillOnCloseTakesTheTreeWithIt covers the runner's per-command job,
// whose guarantee is that closing the handle is enough — no cancel path
// need run for the benchmark binary to die.
func TestKillOnCloseTakesTheTreeWithIt(t *testing.T) {
	job, err := Create("", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	cmd, done := startSleeper(t)
	killed := false
	defer func() {
		if !killed && cmd.Process != nil {
			cmd.Process.Kill()
			<-done
		}
	}()
	if err := job.AssignPID(cmd.Process.Pid); err != nil {
		t.Fatalf("AssignPID: %v", err)
	}
	mustStillBeRunning(t, done)

	if err := job.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
		killed = true
	case <-time.After(15 * time.Second):
		t.Fatal("process outlived the closed kill-on-close job")
	}
}
