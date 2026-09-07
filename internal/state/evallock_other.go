//go:build !unix

package state

import "os"

// tryLockExclusive has no advisory-locking equivalent here, so it reports
// the lock as always available. The consequence is that EvalRunning falls
// back to trusting the pid file's existence, and a pid file left behind by
// a killed eval reads as "no eval running" — the conservative direction:
// `stop -force` is unsupported on this platform anyway (see the command's
// platform-specific implementation), so nothing here signals a pid.
func tryLockExclusive(f *os.File) (bool, error) { return true, nil }

// tryLockShared is the matching no-op for the read-only probe in claimHeld.
func tryLockShared(f *os.File) (bool, error) { return true, nil }

// unlock is the matching no-op.
func unlock(f *os.File) error { return nil }

// releaseClaim drops the claim ClaimEval took on path, held through f.
//
// Close BEFORE removing — the opposite of the unix order, and the reason
// this is a platform-specific helper at all. Windows opens a file without
// FILE_SHARE_DELETE unless asked, which os.OpenFile does not, so removing a
// file this process still holds open fails with a sharing violation and
// leaves eval.pid behind after every eval. The ordering the unix
// implementation protects has nothing to protect here: the locks in this
// file are no-ops, so there is no window in which a racing EvalRunning could
// take the lock and read a doomed pid file — it never consults the lock.
func releaseClaim(f *os.File, path string) error {
	closeErr := f.Close()
	rmErr := os.Remove(path)
	if rmErr != nil && os.IsNotExist(rmErr) {
		rmErr = nil
	}
	if rmErr != nil {
		return rmErr
	}
	return closeErr
}
