//go:build windows

package winjob

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"syscall"
	"unsafe"
)

// Not golang.org/x/sys/windows, which wraps all of these: its recent
// releases require a newer go directive than this module's go 1.21. Same
// reasoning as internal/state/evallock_windows.go.
var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procOpenJobObjectW           = kernel32.NewProc("OpenJobObjectW")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
)

const (
	// classExtendedLimitInformation is the info class that carries
	// LimitFlags.
	classExtendedLimitInformation = 9

	// limitKillOnJobClose kills whatever is still in the job when the last
	// handle to it closes. It is what makes a leaked benchmark impossible
	// rather than merely unlikely: even a caller that never reaches its
	// cancel path takes its subprocesses down when the handle goes.
	limitKillOnJobClose = 0x00002000

	// jobObjectAllAccess is what OpenJobObject needs to terminate.
	jobObjectAllAccess = 0x1F001F

	// processTerminate | processSetQuota is the pair AssignProcessToJobObject
	// requires of a process handle.
	processTerminate = 0x0001
	processSetQuota  = 0x0100
)

// ErrNotFound is returned by Open when no job of that name exists — an eval
// started before this tool grew job objects, or one that has already gone.
var ErrNotFound = fmt.Errorf("no such job object")

// Job is an open handle to a Windows job object.
type Job struct {
	h syscall.Handle
}

// Name derives the job name for a given eval pid.
//
// Keyed on the pid rather than on the run, because `stop -force` knows the
// pid — it reads it from the claim — and threading the state directory
// through the signalling path would change a signature that means nothing
// on unix. Pid reuse is not a hazard here: the claim's lock is what proves
// the eval is alive, and it is checked before this name is ever built.
//
// The Local\ prefix keeps the name in the caller's session namespace, so
// two users on one machine cannot collide or interfere.
func Name(pid int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("autor3search-go-eval-%d", pid)))
	return `Local\autor3search-go-` + hex.EncodeToString(sum[:8])
}

// Create makes a job object. An empty name makes an anonymous one, which is
// what a per-command job wants; killOnClose sets limitKillOnJobClose.
func Create(name string, killOnClose bool) (*Job, error) {
	var namep *uint16
	if name != "" {
		p, err := syscall.UTF16PtrFromString(name)
		if err != nil {
			return nil, fmt.Errorf("job name %q: %w", name, err)
		}
		namep = p
	}
	h, _, err := syscall.SyscallN(procCreateJobObjectW.Addr(),
		0, // no security attributes
		uintptr(unsafe.Pointer(namep)),
	)
	if h == 0 {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	j := &Job{h: syscall.Handle(h)}
	if killOnClose {
		if err := j.setKillOnClose(); err != nil {
			j.Close()
			return nil, err
		}
	}
	return j, nil
}

// Open returns an existing job object by name, or ErrNotFound.
func Open(name string) (*Job, error) {
	namep, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("job name %q: %w", name, err)
	}
	h, _, callErr := syscall.SyscallN(procOpenJobObjectW.Addr(),
		jobObjectAllAccess,
		0, // do not inherit the handle
		uintptr(unsafe.Pointer(namep)),
	)
	if h == 0 {
		if callErr == syscall.ERROR_FILE_NOT_FOUND {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open job object: %w", callErr)
	}
	return &Job{h: syscall.Handle(h)}, nil
}

// setKillOnClose is the one call here that passes a struct rather than a
// handle, so its layout has to match the C definition exactly — the fields
// below are JOBOBJECT_EXTENDED_LIMIT_INFORMATION, in order, and Go's
// alignment rules put them where the C compiler does on both 386 and amd64.
func (j *Job) setKillOnClose() error {
	var info jobObjectExtendedLimitInformation
	info.BasicLimitInformation.LimitFlags = limitKillOnJobClose
	r, _, err := syscall.SyscallN(procSetInformationJobObject.Addr(),
		uintptr(j.h),
		classExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r == 0 {
		return fmt.Errorf("set kill-on-close on job object: %w", err)
	}
	return nil
}

// AssignPID puts an already-running process into the job.
//
// There is a race here worth naming: the process is assigned after it has
// started, so a child that spawns its own children in that window escapes
// the job. It is not closable with os/exec, which offers no hook between
// CreateProcess and the first instruction of the child, and the window is
// microseconds against a `go test` that spends milliseconds before it execs
// anything. Everything the child starts after the assignment is in the job,
// including the compiled benchmark binary this exists to catch.
func (j *Job) AssignPID(pid int) error {
	h, err := openProcess(pid)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	r, _, callErr := syscall.SyscallN(procAssignProcessToJobObject.Addr(),
		uintptr(j.h),
		uintptr(h),
	)
	if r == 0 {
		return fmt.Errorf("assign pid %d to job object: %w", pid, callErr)
	}
	return nil
}

// AssignSelf puts the calling process into the job. Everything it starts
// afterwards is in the job too, inherited, with no assignment race.
func (j *Job) AssignSelf() error {
	r, _, err := syscall.SyscallN(procAssignProcessToJobObject.Addr(),
		uintptr(j.h),
		uintptr(syscall.Handle(^uintptr(0))), // GetCurrentProcess, the -1 pseudo-handle
	)
	if r == 0 {
		return fmt.Errorf("assign this process to job object: %w", err)
	}
	return nil
}

// Terminate kills every process in the job.
func (j *Job) Terminate() error {
	r, _, err := syscall.SyscallN(procTerminateJobObject.Addr(),
		uintptr(j.h),
		1, // exit code for the killed processes
	)
	if r == 0 {
		return fmt.Errorf("terminate job object: %w", err)
	}
	return nil
}

// Close releases the handle. With kill-on-close set, and no other handle
// open, this is also what kills anything still inside.
func (j *Job) Close() error {
	if j == nil || j.h == 0 {
		return nil
	}
	err := syscall.CloseHandle(j.h)
	j.h = 0
	return err
}

func openProcess(pid int) (syscall.Handle, error) {
	h, err := syscall.OpenProcess(processTerminate|processSetQuota, false, uint32(pid))
	if err != nil {
		return 0, fmt.Errorf("open process %d: %w", pid, err)
	}
	return h, nil
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}
