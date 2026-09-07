//go:build unix

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/g4lb/autor3search-go/internal/state"
)

// The two tests here are the ones that need a claim taken in THIS process
// to be visible to a second observer in the same process. That is a
// property of the advisory lock, so they live behind the unix tag next to
// the other lock tests rather than beside their subjects.
//
// On a platform without flock the claim is a no-op (see evallock_other.go):
// ClaimEval always succeeds, claimHeld always answers "nobody holds it",
// and so a second eval is NOT refused and `status` reports an idle run
// while an eval is in flight. Both are real degradations of the tool on
// that platform, deliberately accepted — `stop -force`, the reason the
// claim has to distinguish a live eval from a stale pid file, is
// unsupported there anyway. What must not happen is a test asserting the
// unix behaviour on a platform whose implementation promises the opposite.

func TestEvalRefusesToRunConcurrentlyWithAnotherEval(t *testing.T) {
	// Two evals on one run would fight over the same pinned worktree.
	dir, stateDir, _ := baselinedRepo(t)
	release, err := state.ClaimEval(stateDir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	var code int
	stderr := captureStderr(t, func() {
		code = runEval([]string{"-C", dir, "-json", "-desc", "test"})
	})
	if code != exitUsage {
		t.Fatalf("concurrent runEval = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "already running") {
		t.Errorf("stderr = %q, want it to name the running eval", stderr)
	}
}

func TestStatusReportsARunningEval(t *testing.T) {
	dir, stateDir, _ := baselinedRepo(t)
	release, err := state.ClaimEval(stateDir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	stdout := captureStdout(t, func() { runStatus([]string{"-C", dir}) })
	if !strings.Contains(stdout, "running") {
		t.Errorf("status output = %q, want it to report the eval in flight", stdout)
	}
}
