//go:build unix

package state

import (
	"errors"
	"os"
	"syscall"
)

// tryLockExclusive takes a non-blocking exclusive advisory lock on f,
// reporting false when another process already holds it.
//
// flock(2) is used rather than a lockfile-plus-pid convention because the
// kernel releases it when the holding process dies by ANY means, including a
// SIGKILL that runs no cleanup. That is exactly the case a stop-and-kill
// path has to get right — see ClaimEval.
func tryLockExclusive(f *os.File) (bool, error) {
	return tryLock(f, syscall.LOCK_EX)
}

// tryLockShared takes a non-blocking SHARED lock on f, reporting false when
// an exclusive holder — a running eval — already has it.
//
// It exists so the read-only query in claimHeld does not have to take an
// exclusive lock to find out whether anyone holds one. A shared lock still
// conflicts with the exclusive lock ClaimEval takes, which is what makes it a
// valid probe, but two concurrent probes no longer conflict with EACH OTHER:
// a `status` and a `stop` looking at the same run at the same moment used to
// serialize, and either could report the other as the running eval.
func tryLockShared(f *os.File) (bool, error) {
	return tryLock(f, syscall.LOCK_SH)
}

func tryLock(f *os.File, how int) (bool, error) {
	err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

// unlock releases a lock taken by tryLockExclusive.
func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// releaseClaim drops the claim ClaimEval took on path, held through f.
//
// Remove BEFORE closing: closing drops the flock, and a concurrent
// EvalRunning that acquired it in between would otherwise read a pid file
// this process is about to delete. Either order leaves the same end state,
// and neither can report a live eval that is gone.
func releaseClaim(f *os.File, path string) error {
	rmErr := os.Remove(path)
	if rmErr != nil && os.IsNotExist(rmErr) {
		rmErr = nil
	}
	if closeErr := f.Close(); rmErr == nil {
		rmErr = closeErr
	}
	return rmErr
}
