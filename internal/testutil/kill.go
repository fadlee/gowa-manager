package testutil

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

// KillByExecutable kills every process whose executable is exactly path.
// Tests use it to clean up children they spawned from a binary in their own
// temp dir. Never match by name or command-line substring (pkill -f, taskkill
// /IM): that also kills unrelated processes on the host, such as a real GOWA
// instance, other test packages, or the shell running the tests.
// Best-effort; errors are ignored.
func KillByExecutable(path string) {
	want := canonicalPath(path)
	procs, err := process.Processes()
	if err != nil {
		return
	}
	for _, p := range procs {
		exe, err := p.Exe()
		if err != nil || canonicalPath(exe) != want {
			continue
		}
		_ = p.Kill()
	}
}

func canonicalPath(path string) string {
	// Linux reports the executable of a deleted binary with this suffix.
	path = strings.TrimSuffix(path, " (deleted)")
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return filepath.Clean(path)
}
