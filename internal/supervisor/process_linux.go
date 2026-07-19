//go:build linux

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type platformProcessConfig struct {
	Path string
	Args []string
	Env  map[string]string
}

type linuxProcess struct {
	pid  int
	pgid int

	mu       sync.Mutex
	cmd      *exec.Cmd
	closed   bool
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

func startPlatformProcess(ctx context.Context, config platformProcessConfig) (*linuxProcess, error) {
	if config.Path == "" {
		return nil, errors.New("start process: missing executable path")
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("start process: %w", ctx.Err())
	default:
	}

	cmd := exec.Command(config.Path, config.Args...)
	cmd.Env = mergedEnvironment(config.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}
	proc := &linuxProcess{pid: cmd.Process.Pid, pgid: cmd.Process.Pid, cmd: cmd, waitDone: make(chan struct{})}
	proc.startWait()

	select {
	case <-ctx.Done():
		_ = proc.Kill()
		_ = proc.Close()
		return nil, fmt.Errorf("start process %d: %w", proc.pid, ctx.Err())
	default:
	}
	return proc, nil
}

func (p *linuxProcess) PID() int {
	if p == nil {
		return 0
	}
	return p.pid
}

func (p *linuxProcess) Wait(ctx context.Context) error {
	if p == nil {
		return os.ErrInvalid
	}
	p.startWait()
	select {
	case <-p.waitDone:
		return p.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *linuxProcess) Stop(ctx context.Context) error {
	if p == nil {
		return os.ErrInvalid
	}
	if err := p.signalGroup(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-p.waitDone:
		return p.waitErr
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
		return p.Kill()
	}
}

func (p *linuxProcess) Kill() error {
	if p == nil {
		return os.ErrInvalid
	}
	return p.signalGroup(syscall.SIGKILL)
}

func (p *linuxProcess) Close() error {
	if p == nil {
		return os.ErrInvalid
	}
	if p.isClosed() {
		return nil
	}
	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.waitDone:
		default:
		}
		if !p.processGroupHasLiveMembers() {
			p.markClosed()
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("cleanup timed out for process group %d", p.processGroupID())
}

func (p *linuxProcess) startWait() {
	p.waitOnce.Do(func() {
		go func() {
			cmd := p.currentCmd()
			if cmd == nil {
				p.waitErr = os.ErrProcessDone
				close(p.waitDone)
				return
			}
			if err := cmd.Wait(); err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					p.waitErr = fmt.Errorf("wait process %d: %w", p.pid, err)
				}
			}
			p.mu.Lock()
			p.cmd = nil
			p.mu.Unlock()
			close(p.waitDone)
		}()
	})
}

func (p *linuxProcess) currentCmd() *exec.Cmd {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd
}

func (p *linuxProcess) processGroupID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0
	}
	return p.pgid
}

func (p *linuxProcess) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *linuxProcess) markClosed() {
	p.mu.Lock()
	p.closed = true
	p.pgid = 0
	p.mu.Unlock()
}

func (p *linuxProcess) signalGroup(signal syscall.Signal) error {
	pgid := p.processGroupID()
	if pgid == 0 {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(-pgid, signal); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return fmt.Errorf("signal process group %d for process %d: %w", pgid, p.pid, err)
	}
	return nil
}

func (p *linuxProcess) processGroupHasLiveMembers() bool {
	pgid := p.processGroupID()
	if pgid == 0 {
		return false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		err := syscall.Kill(-pgid, 0)
		return err == nil || errors.Is(err, syscall.EPERM)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(stat))
		if len(fields) < 5 || fields[2] == "Z" {
			continue
		}
		memberPGID, err := strconv.Atoi(fields[4])
		if err == nil && memberPGID == pgid {
			return true
		}
	}
	return false
}

func mergedEnvironment(env map[string]string) []string {
	return mergeEnvironment(os.Environ(), env)
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[key] = entry
	}
	for key, value := range overrides {
		values[key] = key + "=" + value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	merged := make([]string, 0, len(keys))
	for _, key := range keys {
		merged = append(merged, values[key])
	}
	return merged
}
