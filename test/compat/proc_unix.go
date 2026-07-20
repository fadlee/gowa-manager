//go:build !windows

package compat

import (
	"os/exec"
	"testing"
)

// setNewProcessGroup is a no-op on Unix; signals are delivered directly via
// os.Process.Signal.
func setNewProcessGroup(_ *exec.Cmd) {}

// sendWindowsCtrlBreak is a no-op stub on Unix; the real implementation
// lives in proc_windows.go. This stub exists so go vet on Linux can
// resolve the symbol referenced in production-data_test.go.
func sendWindowsCtrlBreak(_ *testing.T, _ int) {}
