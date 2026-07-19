//go:build windows

package compat

import (
	"fmt"
	"os/exec"
	"syscall"
	"testing"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGenerateConsoleCtrlEvent   = kernel32.NewProc("GenerateConsoleCtrlEvent")
)

// setNewProcessGroup configures the command to create the child process in a
// new process group (CREATE_NEW_PROCESS_GROUP). This is required so that
// GenerateConsoleCtrlEvent can target the group with a Ctrl+Break event,
// which the Go runtime maps to os.Interrupt for graceful shutdown.
func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// sendWindowsCtrlBreak sends a CTRL_BREAK_EVENT to the process group
// identified by pid. The Go runtime handles this as os.Interrupt, triggering
// graceful shutdown.
func sendWindowsCtrlBreak(t *testing.T, pid int) {
	t.Helper()
	const CTRL_BREAK_EVENT uint32 = 1
	r1, _, err := procGenerateConsoleCtrlEvent.Call(
		uintptr(CTRL_BREAK_EVENT),
		uintptr(pid),
	)
	if r1 == 0 {
		t.Logf("GenerateConsoleCtrlEvent failed for pid %d: %v (will force-kill on cleanup)", pid, err)
	}
}

// keep unsafe import referenced for future extension if needed.
var _ = unsafe.Sizeof(0)

// fmt is used in logging above.
var _ = fmt.Sprintf
