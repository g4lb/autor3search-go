//go:build windows

package state

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// Windows has no flock(2), but LockFileEx provides the one property the
// claim actually depends on: the lock is dropped by the kernel when the
// holding handle closes, however the holder died — SIGKILL's equivalent, a
// panic, or the power going out. A pid file plus a liveness convention
// could not promise that, which is why ClaimEval is written against a lock.
//
// Not golang.org/x/sys/windows, which wraps both calls already: its recent
// releases require a newer go directive than this module's go 1.21, so
// depending on it would mean either breaking the 1.21 CI job or pinning a
// stale version of it forever. Two procs out of kernel32 is the smaller
// commitment.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002

	// errorLockViolation is what LOCKFILE_FAIL_IMMEDIATELY reports when
	// someone else holds a conflicting lock. It is this call's spelling of
	// EWOULDBLOCK, and the only failure that means "held by another" rather
	// than "the lock could not be taken at all".
	errorLockViolation = syscall.Errno(33)
)

// The lock covers ONE BYTE at an offset no pid file will ever reach, rather
// than the file's contents, and that is the difference between a working
// claim and a broken one: a Windows byte-range lock is MANDATORY, not
// advisory like flock. Locking the bytes the pid lives in would make every
// READER fail — EvalRunning reads the pid precisely while the claim is
// held, and ClaimEval reads it to name the incumbent in the message it
// returns when the claim is refused. Both would get ERROR_LOCK_VIOLATION
// where they expect an answer.
//
// Locking past every byte the file will ever hold (2^62, on a file that
// holds a decimal pid and a newline) keeps the lock a pure signal, which is
// the advisory role flock plays on unix. Ranges beyond end-of-file are
// legal to lock and are released the same way.
const (
	lockOffsetHigh = 0x40000000 // byte 1<<62
	lockBytesLow   = 1
)

// tryLockExclusive takes a non-blocking exclusive lock on f, reporting
// false when another handle already holds a conflicting one.
func tryLockExclusive(f *os.File) (bool, error) {
	return tryLock(f, lockfileExclusiveLock)
}

// tryLockShared is the matching non-blocking SHARED lock, for the read-only
// probe in claimHeld. Passing no lock-type flag asks for a shared lock.
func tryLockShared(f *os.File) (bool, error) {
	return tryLock(f, 0)
}

func tryLock(f *os.File, flags uint32) (bool, error) {
	// LockFileEx requires an OVERLAPPED even for an ordinary synchronous
	// handle, because that struct is where the start of the range lives.
	ol := &syscall.Overlapped{OffsetHigh: lockOffsetHigh}
	// syscall.SyscallN, not procLockFileEx.Call: the compiler recognizes a
	// uintptr(unsafe.Pointer(x)) written inside a SyscallN argument list
	// and keeps x alive across the call. Through Call — an ordinary
	// variadic function — it makes no such promise.
	r, _, err := syscall.SyscallN(
		procLockFileEx.Addr(),
		f.Fd(),
		uintptr(flags|lockfileFailImmediately),
		0, // reserved, must be zero
		lockBytesLow,
		0, // bytes to lock, high half
		uintptr(unsafe.Pointer(ol)),
	)
	if r != 0 {
		return true, nil
	}
	if errors.Is(err, errorLockViolation) {
		return false, nil
	}
	return false, err
}

// unlock releases a lock taken by tryLockExclusive or tryLockShared. It has
// to name the same range that was locked; a mismatched range is an error,
// not a no-op.
func unlock(f *os.File) error {
	ol := &syscall.Overlapped{OffsetHigh: lockOffsetHigh}
	r, _, err := syscall.SyscallN(
		procUnlockFileEx.Addr(),
		f.Fd(),
		0, // reserved, must be zero
		lockBytesLow,
		0, // bytes to unlock, high half
		uintptr(unsafe.Pointer(ol)),
	)
	if r != 0 {
		return nil
	}
	return err
}

// releaseClaim drops the claim ClaimEval took on path, held through f.
//
// Close BEFORE removing, the opposite of the unix order: os.OpenFile does
// not ask for FILE_SHARE_DELETE, so Windows refuses to remove a file this
// process still holds open, and the pid file would survive every eval.
//
// Closing first also drops the lock first, which is the race the unix
// implementation avoids by removing first — but it is harmless in this
// direction. An EvalRunning that takes the lock in the gap concludes that
// nobody holds the claim, which is exactly true: this eval has finished.
// The pid file it may still see in that instant is not consulted, because
// claimHeld answers from the lock and not from the file's existence.
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
