package testutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"testing"
	"time"
)

type Process struct {
	Cmd     *exec.Cmd
	DataDir string
	Port    int
	Stdout  bytes.Buffer
	Stderr  bytes.Buffer
}

func StartProcess(t *testing.T, command string, args ...string) *Process {
	t.Helper()
	dataDir := t.TempDir()
	port := FreePort(t)
	cmdArgs := append(args, "--data-dir", dataDir, "--port", fmt.Sprint(port))
	cmd := exec.Command(command, cmdArgs...)
	proc := &Process{Cmd: cmd, DataDir: dataDir, Port: port}
	cmd.Stdout = &proc.Stdout
	cmd.Stderr = &proc.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	t.Cleanup(func() { proc.Terminate() })
	return proc
}

func (p *Process) WaitForHealth(t *testing.T, deadline time.Duration) {
	t.Helper()
	client := http.Client{Timeout: 500 * time.Millisecond}
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", p.Port))
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process did not become healthy on port %d", p.Port)
}

func (p *Process) Terminate() {
	if p == nil || p.Cmd == nil || p.Cmd.Process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = p.Cmd.Process.Kill()
		_ = p.Cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
