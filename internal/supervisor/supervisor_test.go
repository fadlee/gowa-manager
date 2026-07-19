package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSupervisorSuccessfulStart(t *testing.T) {
	proc := newFakeProcess(1001)
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })

	snapshot, err := s.Start(context.Background(), StartConfig{InstanceID: 1, Path: "fakegowa", ReadyTimeout: time.Second})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if snapshot.State != StateRunning || snapshot.PID != 1001 || snapshot.Generation != 1 {
		t.Fatalf("Start() snapshot = %+v, want running pid 1001 generation 1", snapshot)
	}
	if got := s.startCalls(); got != 1 {
		t.Fatalf("starter calls = %d, want 1", got)
	}
}

func TestSupervisorDuplicateStartReturnsCurrentStateWithoutDuplicateProcess(t *testing.T) {
	proc := newFakeProcess(1001)
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })

	first, err := s.Start(context.Background(), StartConfig{InstanceID: 2, Path: "fakegowa", ReadyTimeout: time.Second})
	if err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	second, err := s.Start(context.Background(), StartConfig{InstanceID: 2, Path: "fakegowa", ReadyTimeout: time.Second})
	if err != nil {
		t.Fatalf("duplicate Start() error = %v", err)
	}
	if second != first {
		t.Fatalf("duplicate Start() snapshot = %+v, want current %+v", second, first)
	}
	if got := s.startCalls(); got != 1 {
		t.Fatalf("starter calls = %d, want 1", got)
	}
}

func TestSupervisorStatusObservesStartingWhileReadinessPending(t *testing.T) {
	proc := newFakeProcess(1101)
	readyStarted := make(chan struct{})
	releaseReady := make(chan struct{})
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.ready = func(ctx context.Context, snapshot ProcessSnapshot) error {
		close(readyStarted)
		select {
		case <-releaseReady:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	startDone := make(chan error, 1)
	go func() {
		_, err := s.Start(context.Background(), StartConfig{InstanceID: 12, Path: "fakegowa", ReadyTimeout: time.Second})
		startDone <- err
	}()
	<-readyStarted

	snapshot, ok := s.Status(12)
	if !ok || snapshot.State != StateStarting || snapshot.PID != 1101 || snapshot.Generation != 1 {
		t.Fatalf("Status() = %+v ok %v, want starting pid 1101 generation 1", snapshot, ok)
	}
	close(releaseReady)
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestSupervisorDuplicateStartDuringPendingReadinessReturnsStartingWithoutDuplicateProcess(t *testing.T) {
	proc := newFakeProcess(1102)
	readyStarted := make(chan struct{})
	releaseReady := make(chan struct{})
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.ready = func(ctx context.Context, snapshot ProcessSnapshot) error {
		close(readyStarted)
		select {
		case <-releaseReady:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	startDone := make(chan error, 1)
	go func() {
		_, err := s.Start(context.Background(), StartConfig{InstanceID: 13, Path: "fakegowa", ReadyTimeout: time.Second})
		startDone <- err
	}()
	<-readyStarted

	snapshot, err := s.Start(context.Background(), StartConfig{InstanceID: 13, Path: "fakegowa", ReadyTimeout: time.Second})
	if err != nil {
		t.Fatalf("duplicate Start() error = %v", err)
	}
	if snapshot.State != StateStarting || snapshot.PID != 1102 || snapshot.Generation != 1 {
		t.Fatalf("duplicate Start() snapshot = %+v, want current starting", snapshot)
	}
	if got := s.startCalls(); got != 1 {
		t.Fatalf("starter calls = %d, want 1", got)
	}
	close(releaseReady)
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestSupervisorStopDuringPendingReadinessTerminatesProcess(t *testing.T) {
	proc := newFakeProcess(1103)
	readyStarted := make(chan struct{})
	releaseReady := make(chan struct{})
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.ready = func(ctx context.Context, snapshot ProcessSnapshot) error {
		close(readyStarted)
		select {
		case <-releaseReady:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	startDone := make(chan error, 1)
	go func() {
		_, err := s.Start(context.Background(), StartConfig{InstanceID: 14, Path: "fakegowa", ReadyTimeout: time.Second})
		startDone <- err
	}()
	<-readyStarted

	snapshot, err := s.Stop(context.Background(), 14)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if snapshot.State != StateStopped || !proc.stopped() || proc.killed() {
		t.Fatalf("Stop() snapshot=%+v stopped=%v killed=%v, want stopped gracefully", snapshot, proc.stopped(), proc.killed())
	}
	close(releaseReady)
	if err := <-startDone; !errors.Is(err, ErrProcessExited) {
		t.Fatalf("pending Start() error = %v, want ErrProcessExited", err)
	}
}

func TestSupervisorKillDuringPendingReadinessTerminatesProcess(t *testing.T) {
	proc := newFakeProcess(1104)
	readyStarted := make(chan struct{})
	releaseReady := make(chan struct{})
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.ready = func(ctx context.Context, snapshot ProcessSnapshot) error {
		close(readyStarted)
		select {
		case <-releaseReady:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	startDone := make(chan error, 1)
	go func() {
		_, err := s.Start(context.Background(), StartConfig{InstanceID: 15, Path: "fakegowa", ReadyTimeout: time.Second})
		startDone <- err
	}()
	<-readyStarted

	snapshot, err := s.Kill(context.Background(), 15)
	if err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if snapshot.State != StateStopped || !proc.killed() {
		t.Fatalf("Kill() snapshot=%+v killed=%v, want stopped and killed", snapshot, proc.killed())
	}
	close(releaseReady)
	if err := <-startDone; !errors.Is(err, ErrProcessExited) {
		t.Fatalf("pending Start() error = %v, want ErrProcessExited", err)
	}
}

func TestSupervisorExitDuringPendingReadinessUpdatesMatchingGeneration(t *testing.T) {
	proc := newFakeProcess(1105)
	readyStarted := make(chan struct{})
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.ready = func(ctx context.Context, snapshot ProcessSnapshot) error {
		close(readyStarted)
		<-ctx.Done()
		return ctx.Err()
	}
	startDone := make(chan error, 1)
	go func() {
		_, err := s.Start(context.Background(), StartConfig{InstanceID: 16, Path: "fakegowa", ReadyTimeout: time.Second})
		startDone <- err
	}()
	<-readyStarted

	proc.exit(errors.New("boom"))
	if err := <-startDone; !errors.Is(err, ErrProcessExited) {
		t.Fatalf("Start() error = %v, want ErrProcessExited", err)
	}
	s.waitForExitCallbacks(t, 1)
	snapshot, ok := s.Status(16)
	if !ok || snapshot.State != StateStopped || snapshot.Generation != 1 {
		t.Fatalf("Status() = %+v ok %v, want stopped generation 1", snapshot, ok)
	}
}

func TestSupervisorImmediateCrash(t *testing.T) {
	proc := newFakeProcess(1002)
	proc.exit(errors.New("boom"))
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.ready = func(context.Context, ProcessSnapshot) error { return nil }

	_, err := s.Start(context.Background(), StartConfig{InstanceID: 3, Path: "fakegowa", ReadyTimeout: time.Second})
	if !errors.Is(err, ErrProcessExited) {
		t.Fatalf("Start() error = %v, want ErrProcessExited", err)
	}
}

func TestSupervisorReadinessTimeout(t *testing.T) {
	proc := newFakeProcess(1003)
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.ready = func(ctx context.Context, snapshot ProcessSnapshot) error { <-ctx.Done(); return ctx.Err() }

	_, err := s.Start(context.Background(), StartConfig{InstanceID: 4, Path: "fakegowa", ReadyTimeout: time.Millisecond})
	if !errors.Is(err, ErrStartTimeout) {
		t.Fatalf("Start() error = %v, want ErrStartTimeout", err)
	}
	if !proc.killed() || !proc.closed() {
		t.Fatalf("process cleanup killed=%v closed=%v, want both true", proc.killed(), proc.closed())
	}
}

func TestSupervisorStop(t *testing.T) {
	proc := newFakeProcess(1004)
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	if _, err := s.Start(context.Background(), StartConfig{InstanceID: 5, Path: "fakegowa", ReadyTimeout: time.Second}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	snapshot, err := s.Stop(context.Background(), 5)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if snapshot.State != StateStopped {
		t.Fatalf("Stop() snapshot = %+v, want stopped", snapshot)
	}
	if !proc.stopped() || proc.killed() {
		t.Fatalf("process stopped=%v killed=%v, want graceful stop only", proc.stopped(), proc.killed())
	}
}

func TestSupervisorForceKill(t *testing.T) {
	proc := newFakeProcess(1005)
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	if _, err := s.Start(context.Background(), StartConfig{InstanceID: 6, Path: "fakegowa", ReadyTimeout: time.Second}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	snapshot, err := s.Kill(context.Background(), 6)
	if err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if snapshot.State != StateStopped || !proc.killed() {
		t.Fatalf("Kill() snapshot=%+v killed=%v, want stopped and killed", snapshot, proc.killed())
	}
}

func TestSupervisorRestartGenerationIgnoresStaleExit(t *testing.T) {
	first := newFakeProcess(1006)
	second := newFakeProcess(1007)
	procs := []*fakeProcess{first, second}
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) {
		p := procs[0]
		procs = procs[1:]
		return p, nil
	})
	if _, err := s.Start(context.Background(), StartConfig{InstanceID: 7, Path: "fakegowa", ReadyTimeout: time.Second}); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if _, err := s.Kill(context.Background(), 7); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	secondSnapshot, err := s.Start(context.Background(), StartConfig{InstanceID: 7, Path: "fakegowa", ReadyTimeout: time.Second})
	if err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	first.exit(nil)
	time.Sleep(10 * time.Millisecond)

	snapshot, ok := s.Status(7)
	if !ok || snapshot.Generation != secondSnapshot.Generation || snapshot.PID != 1007 || snapshot.State != StateRunning {
		t.Fatalf("Status() = %+v ok %v, want second running generation", snapshot, ok)
	}
}

func TestSupervisorContextCancellation(t *testing.T) {
	proc := newFakeProcess(1008)
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.ready = func(ctx context.Context, snapshot ProcessSnapshot) error { <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Start(ctx, StartConfig{InstanceID: 8, Path: "fakegowa", ReadyTimeout: time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context.Canceled", err)
	}
	if !proc.killed() || !proc.closed() {
		t.Fatalf("process cleanup killed=%v closed=%v, want both true", proc.killed(), proc.closed())
	}
}

func TestSupervisorStartCleanupAfterStatusCallbackFailure(t *testing.T) {
	proc := newFakeProcess(1009)
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	s.onStatus = func(context.Context, ProcessSnapshot) error { return errors.New("db unavailable") }

	_, err := s.Start(context.Background(), StartConfig{InstanceID: 9, Path: "fakegowa", ReadyTimeout: time.Second})
	if err == nil {
		t.Fatal("Start() error = nil, want callback failure")
	}
	if !proc.killed() || !proc.closed() {
		t.Fatalf("process cleanup killed=%v closed=%v, want both true", proc.killed(), proc.closed())
	}
	if _, ok := s.Status(9); ok {
		t.Fatal("Status() ok = true after failed start cleanup")
	}
}

func TestSupervisorExitCallbackOnce(t *testing.T) {
	proc := newFakeProcess(1010)
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return proc, nil })
	if _, err := s.Start(context.Background(), StartConfig{InstanceID: 10, Path: "fakegowa", ReadyTimeout: time.Second}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	proc.exit(nil)
	proc.exit(nil)
	s.waitForExitCallbacks(t, 1)
	time.Sleep(10 * time.Millisecond)
	if got := s.exitCalls(); got != 1 {
		t.Fatalf("exit callback calls = %d, want 1", got)
	}
}

func TestSupervisorConcurrentStartStopRaces(t *testing.T) {
	s := newTestSupervisor(t, func(context.Context, StartConfig) (Process, error) { return newFakeProcess(2000), nil })
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.Start(context.Background(), StartConfig{InstanceID: 11, Path: "fakegowa", ReadyTimeout: time.Second})
		}()
		go func() {
			defer wg.Done()
			_, _ = s.Stop(context.Background(), 11)
		}()
	}
	wg.Wait()
}

type fakeProcess struct {
	pid int

	mu         sync.Mutex
	stopCount  int
	killCount  int
	closeCount int
	exitOnce   sync.Once
	waitDone   chan struct{}
	waitErr    error
}

func newFakeProcess(pid int) *fakeProcess {
	return &fakeProcess{pid: pid, waitDone: make(chan struct{})}
}
func (p *fakeProcess) PID() int { return p.pid }
func (p *fakeProcess) Wait(ctx context.Context) error {
	select {
	case <-p.waitDone:
		return p.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *fakeProcess) Stop(ctx context.Context) error {
	p.mu.Lock()
	p.stopCount++
	p.mu.Unlock()
	p.exit(nil)
	return nil
}
func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	p.killCount++
	p.mu.Unlock()
	p.exit(nil)
	return nil
}
func (p *fakeProcess) Close() error   { p.mu.Lock(); p.closeCount++; p.mu.Unlock(); return nil }
func (p *fakeProcess) exit(err error) { p.exitOnce.Do(func() { p.waitErr = err; close(p.waitDone) }) }
func (p *fakeProcess) stopped() bool  { p.mu.Lock(); defer p.mu.Unlock(); return p.stopCount > 0 }
func (p *fakeProcess) killed() bool   { p.mu.Lock(); defer p.mu.Unlock(); return p.killCount > 0 }
func (p *fakeProcess) closed() bool   { p.mu.Lock(); defer p.mu.Unlock(); return p.closeCount > 0 }

type testSupervisor struct {
	*Supervisor
	mu       sync.Mutex
	starts   int
	exits    int
	exitCond *sync.Cond
	ready    ReadinessProbe
	onStatus StatusCallback
}

func newTestSupervisor(t *testing.T, starter Starter) *testSupervisor {
	t.Helper()
	ts := &testSupervisor{}
	ts.exitCond = sync.NewCond(&ts.mu)
	ts.ready = func(context.Context, ProcessSnapshot) error { return nil }
	ts.onStatus = func(context.Context, ProcessSnapshot) error { return nil }
	ts.Supervisor = New(SupervisorConfig{
		Registry: NewRegistry(),
		Starter: func(ctx context.Context, config StartConfig) (Process, error) {
			ts.mu.Lock()
			ts.starts++
			ts.mu.Unlock()
			return starter(ctx, config)
		},
		ReadinessProbe: func(ctx context.Context, snapshot ProcessSnapshot) error { return ts.ready(ctx, snapshot) },
		StatusCallback: func(ctx context.Context, snapshot ProcessSnapshot) error { return ts.onStatus(ctx, snapshot) },
		ExitCallback:   func(snapshot ProcessSnapshot) { ts.mu.Lock(); ts.exits++; ts.exitCond.Broadcast(); ts.mu.Unlock() },
	})
	return ts
}

func (s *testSupervisor) startCalls() int { s.mu.Lock(); defer s.mu.Unlock(); return s.starts }
func (s *testSupervisor) exitCalls() int  { s.mu.Lock(); defer s.mu.Unlock(); return s.exits }
func (s *testSupervisor) waitForExitCallbacks(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.exitCalls() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("exit callbacks = %d, want at least %d", s.exitCalls(), want)
}
