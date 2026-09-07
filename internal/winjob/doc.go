// Package winjob wraps the Windows job-object calls that give this tool the
// two guarantees a unix process group gives it for free: killing a command
// kills everything it started, and `stop -force` can end an eval together
// with the benchmark binaries running underneath it.
//
// The package is empty on every other platform. It exists as a package
// rather than as another pair of build-tagged files inside internal/runner
// because two callers need it — the runner, per command, and eval, over
// itself — and neither should own the syscall glue.
package winjob
