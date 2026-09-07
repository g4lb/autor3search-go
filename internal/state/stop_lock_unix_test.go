//go:build unix

package state_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/g4lb/autor3search-go/internal/state"
)

// Unix-only: the assertion is that a HELD claim reads as running, which is
// what the flock in evallock_unix.go provides. Elsewhere tryLockExclusive
// is a no-op and EvalRunning falls back to the pid file's existence.
func TestClaimEvalIsVisibleToEvalRunning(t *testing.T) {
	dir := t.TempDir()
	release, err := state.ClaimEval(dir, 4321)
	if err != nil {
		t.Fatalf("ClaimEval: %v", err)
	}
	defer release()

	pid, running, err := state.EvalRunning(dir)
	if err != nil {
		t.Fatalf("EvalRunning: %v", err)
	}
	if !running || pid != 4321 {
		t.Fatalf("EvalRunning = (%d, %v); want (4321, true)", pid, running)
	}
}

// TestConcurrentEvalRunningQueriesAgreeWithEachOther pins the other half of
// the shared probe: two queries running at once must not see each other. With
// an exclusive probe one of them took the lock and the other read that as a
// running eval, so `status` and `stop` could each report the other as the
// experiment in flight.
func TestConcurrentEvalRunningQueriesAgreeWithEachOther(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, state.EvalPIDFile), []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	running := make([]bool, 8)
	errs := make([]error, 8)
	start := make(chan struct{})
	for i := range running {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, running[i], errs[i] = state.EvalRunning(dir)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range running {
		if errs[i] != nil {
			t.Fatalf("query %d: %v", i, errs[i])
		}
		if running[i] {
			t.Errorf("query %d reported an eval running; only the other queries were there", i)
		}
	}
}
