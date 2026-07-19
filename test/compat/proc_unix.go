//go:build !windows

package compat

import "os/exec"

// setNewProcessGroup is a no-op on Unix; signals are delivered directly via
// os.Process.Signal.
func setNewProcessGroup(_ *exec.Cmd) {}
