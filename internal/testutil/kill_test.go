package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestKillByExecutableOnlyKillsMatchingBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n\nimport \"time\"\n\nfunc main() { time.Sleep(time.Minute) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := func(name string) string {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		return filepath.Join(dir, name)
	}
	target, other := exe("target"), exe("other")
	for _, bin := range []string{target, other} {
		if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", bin, err, out)
		}
	}
	start := func(bin string) (*exec.Cmd, chan error) {
		cmd := exec.Command(bin)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		// The test body may already have received from done, so never
		// block on it here.
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return cmd, done
	}
	_, targetDone := start(target)
	_, otherDone := start(other)

	KillByExecutable(target)

	select {
	case <-targetDone:
	case <-time.After(5 * time.Second):
		t.Fatal("target process was not killed")
	}
	select {
	case <-otherDone:
		t.Fatal("unrelated process was killed")
	case <-time.After(200 * time.Millisecond):
	}
}
