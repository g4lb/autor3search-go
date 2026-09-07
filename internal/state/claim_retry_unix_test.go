//go:build unix

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stalePIDFile writes the pid file an eval killed without cleanup leaves
// behind, and returns its path. It is what claimHeld opens and probes.
func stalePIDFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, EvalPIDFile)
	if err := os.WriteFile(path, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// holdLock takes the same shared lock claimHeld's probe takes, so a test can
// place a probe-shaped contender in ClaimEval's way at a chosen moment.
func holdLock(t *testing.T, path string) (release func()) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := tryLockShared(f)
	if err != nil {
		t.Fatal(err)
	}
	if !locked {
		t.Fatal("could not take the probe lock the test needs to hold")
	}
	return func() {
		unlock(f)
		f.Close()
	}
}

// TestClaimEvalRidesOutATransientProbe is the race the retry exists for. A
// human's `status` or `stop` has to contend for the lock to find out whether
// an eval is running; an eval starting inside that window used to read the
// refusal as an incumbent and abort — naming a pid from the stale file, so
// the message was wrong as well as spurious.
func TestClaimEvalRidesOutATransientProbe(t *testing.T) {
	dir := t.TempDir()
	path := stalePIDFile(t, dir)

	release := holdLock(t, path)
	// Held for one retry interval: far longer than a real probe, far shorter
	// than the retry budget.
	go func() {
		time.Sleep(claimRetryInterval)
		release()
	}()

	claimed, err := ClaimEval(dir, os.Getpid())
	if err != nil {
		t.Fatalf("ClaimEval gave up on a transient read-only probe: %v", err)
	}
	if err := claimed(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestClaimEvalStillRefusesAGenuineIncumbent is the other direction: the
// retry must ride out a probe without papering over a second eval. A holder
// that outlasts the whole retry budget is a real conflict and has to be
// reported as one — two evals sharing a pinned worktree would measure each
// other's checkouts.
func TestClaimEvalStillRefusesAGenuineIncumbent(t *testing.T) {
	dir := t.TempDir()
	path := stalePIDFile(t, dir)

	release := holdLock(t, path)
	defer release()

	start := time.Now()
	if _, err := ClaimEval(dir, os.Getpid()); err == nil {
		t.Fatal("ClaimEval succeeded while the claim was held, want a refusal")
	}
	// It must actually have waited, rather than refusing on the first try:
	// that is what distinguishes a probe from an incumbent.
	if waited := time.Since(start); waited < claimRetries*claimRetryInterval {
		t.Errorf("gave up after %s, want at least %s of retries", waited, claimRetries*claimRetryInterval)
	}
}
