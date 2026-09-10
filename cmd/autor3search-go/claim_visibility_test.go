//go:build unix || windows

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/autor3search/go/internal/state"
)

// The two tests here are the ones that need a claim taken in THIS process
// to be visible to a second observer in the same process. That is a
// property of the lock rather than of either command, so they are tagged to
// the platforms that have one — flock on unix, LockFileEx on windows —
// rather than living beside their subjects.
//
// Anywhere else the claim is a no-op (see evallock_other.go): ClaimEval
// always succeeds, claimHeld always answers "nobody holds it", and so a
// second eval is NOT refused and `status` reports an idle run while an eval
// is in flight. What must not happen is a test asserting the locked
// behaviour on a platform whose implementation promises the opposite.

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
