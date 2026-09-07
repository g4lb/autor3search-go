//go:build !unix && !windows

package runner

import "os/exec"

// procGroup is a no-op on a platform with neither process groups nor job
// objects. Timeouts there kill only the direct child; see
// procgroup_unix.go for what that costs.
type procGroup struct{}

func newProcGroup(cmd *exec.Cmd) *procGroup     { return &procGroup{} }
func (g *procGroup) attach(cmd *exec.Cmd) error { return nil }
func (g *procGroup) close()                     {}
