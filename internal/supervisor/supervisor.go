package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrAlreadyRunning = errors.New("supervisor process already running")
	ErrNotRunning     = errors.New("supervisor process not running")
	ErrStartTimeout   = errors.New("supervisor start readiness timeout")
	ErrProcessExited  = errors.New("supervisor process exited")
	ErrStartFailed    = errors.New("supervisor start failed")
)

type Process interface {
	PID() int
	Wait(context.Context) error
	Stop(context.Context) error
	Kill() error
	Close() error
}

type StartConfig struct {
	InstanceID   int64
	Path         string
	Args         []string
	Env          map[string]string
	Dir          string
	ReadyTimeout time.Duration
	StopTimeout  time.Duration
	StartedAt    time.Time
}

type ProcessConfig struct {
	InstanceID int64
	Path       string
	Args       []string
	Env        map[string]string
	Dir        string
}

type Platform interface {
	Start(context.Context, ProcessConfig) (Process, error)
}

type Starter func(context.Context, StartConfig) (Process, error)
type ReadinessProbe func(context.Context, ProcessSnapshot) error
type StatusCallback func(context.Context, ProcessSnapshot) error
type ExitCallback func(ProcessSnapshot)

type SupervisorConfig struct {
	Registry       *Registry
	Platform       Platform
	ReadinessProbe ReadinessProbe
	StatusCallback StatusCallback
	ExitCallback   ExitCallback
	Now            func() time.Time
}

type Supervisor struct {
	registry *Registry
	platform Platform
	ready    ReadinessProbe
	onStatus StatusCallback
	onExit   ExitCallback
	now      func() time.Time

	mu           sync.Mutex
	startMu      map[int64]*startLock
	processes    map[processKey]Process
	stopTimeouts map[processKey]time.Duration
}

type startLock struct {
	mu   sync.Mutex
	refs int
}

type processKey struct {
	instanceID int64
	generation int64
}

func New(config SupervisorConfig) *Supervisor {
	registry := config.Registry
	if registry == nil {
		registry = NewRegistry()
	}
	platform := config.Platform
	if platform == nil {
		platform = defaultPlatform{}
	}
	ready := config.ReadinessProbe
	if ready == nil {
		ready = func(context.Context, ProcessSnapshot) error { return nil }
	}
	onStatus := config.StatusCallback
	if onStatus == nil {
		onStatus = func(context.Context, ProcessSnapshot) error { return nil }
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Supervisor{registry: registry, platform: platform, ready: ready, onStatus: onStatus, onExit: config.ExitCallback, now: now, startMu: make(map[int64]*startLock), processes: make(map[processKey]Process), stopTimeouts: make(map[processKey]time.Duration)}
}

func (s *Supervisor) Start(ctx context.Context, config StartConfig) (ProcessSnapshot, error) {
	if snapshot, ok := s.registry.Get(config.InstanceID); ok && (snapshot.State == StateStarting || snapshot.State == StateRunning) {
		return snapshot, nil
	}
	startMu := s.acquireStartLock(config.InstanceID)
	startMu.mu.Lock()
	defer s.releaseStartLock(config.InstanceID, startMu)
	if snapshot, ok := s.registry.Get(config.InstanceID); ok && (snapshot.State == StateStarting || snapshot.State == StateRunning) {
		return snapshot, nil
	}
	startedAt := config.StartedAt
	if startedAt.IsZero() {
		startedAt = s.now()
	}
	proc, err := s.platform.Start(ctx, ProcessConfig{InstanceID: config.InstanceID, Path: config.Path, Args: config.Args, Env: config.Env, Dir: config.Dir})
	if err != nil {
		return ProcessSnapshot{}, fmt.Errorf("%w: %v", ErrStartFailed, err)
	}
	snapshot, err := s.registry.Register(config.InstanceID, ProcessSnapshot{State: StateStarting, PID: proc.PID(), StartedAt: startedAt})
	if errors.Is(err, ErrAlreadyRunning) {
		s.cleanupProcess(proc)
		return snapshot, nil
	}
	if err != nil {
		s.cleanupProcess(proc)
		return ProcessSnapshot{}, err
	}
	if err := s.onStatus(ctx, snapshot); err != nil {
		_ = s.registry.Remove(config.InstanceID)
		s.cleanupProcess(proc)
		return ProcessSnapshot{}, err
	}
	waitDone := make(chan error, 1)
	exitDone := make(chan error, 1)
	go func() {
		err := proc.Wait(context.Background())
		waitDone <- err
		exitDone <- err
	}()
	s.storeProcess(config.InstanceID, snapshot.Generation, proc, config.StopTimeout)
	go s.handleExit(config.InstanceID, snapshot.Generation, snapshot, proc, exitDone)

	readyTimeout := config.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = 30 * time.Second
	}
	readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	readyErr := make(chan error, 1)
	go func() { readyErr <- s.ready(readyCtx, snapshot) }()

	select {
	case err := <-waitDone:
		return ProcessSnapshot{}, fmt.Errorf("%w: %v", ErrProcessExited, err)
	case err := <-readyErr:
		if err != nil {
			_ = s.registry.Remove(config.InstanceID)
			if proc, _ := s.takeProcess(config.InstanceID, snapshot.Generation); proc != nil {
				s.cleanupProcess(proc)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return ProcessSnapshot{}, ErrStartTimeout
			}
			return ProcessSnapshot{}, err
		}
	case <-ctx.Done():
		_ = s.registry.Remove(config.InstanceID)
		if proc, _ := s.takeProcess(config.InstanceID, snapshot.Generation); proc != nil {
			s.cleanupProcess(proc)
		}
		return ProcessSnapshot{}, ctx.Err()
	}
	earlyExitGrace := time.NewTimer(10 * time.Millisecond)
	select {
	case err := <-waitDone:
		if !earlyExitGrace.Stop() {
			<-earlyExitGrace.C
		}
		return ProcessSnapshot{}, fmt.Errorf("%w: %v", ErrProcessExited, err)
	case <-earlyExitGrace.C:
	}

	snapshot.State = StateRunning
	if err := s.onStatus(ctx, snapshot); err != nil {
		_ = s.registry.Remove(config.InstanceID)
		if proc, _ := s.takeProcess(config.InstanceID, snapshot.Generation); proc != nil {
			s.cleanupProcess(proc)
		}
		return ProcessSnapshot{}, err
	}
	if err := s.registry.Transition(config.InstanceID, snapshot.Generation, StateStarting, snapshot); err != nil {
		return ProcessSnapshot{}, fmt.Errorf("%w: %v", ErrProcessExited, err)
	}
	return snapshot, nil
}

func (s *Supervisor) Stop(ctx context.Context, instanceID int64) (ProcessSnapshot, error) {
	return s.stopWith(ctx, instanceID, false)
}

func (s *Supervisor) Kill(ctx context.Context, instanceID int64) (ProcessSnapshot, error) {
	return s.stopWith(ctx, instanceID, true)
}

func (s *Supervisor) Status(instanceID int64) (ProcessSnapshot, bool) {
	return s.registry.Get(instanceID)
}

func (s *Supervisor) stopWith(ctx context.Context, instanceID int64, force bool) (ProcessSnapshot, error) {
	current, ok := s.registry.Get(instanceID)
	if !ok || current.State == StateStopped || current.State == StateFailed {
		return ProcessSnapshot{}, ErrNotRunning
	}
	stopped, err := s.registry.WithOperation(instanceID, func(generation int64) (ProcessSnapshot, error) {
		current, ok := s.registry.Get(instanceID)
		if !ok || current.State == StateStopped || current.State == StateFailed {
			return ProcessSnapshot{}, ErrNotRunning
		}
		proc := s.getProcess(current.InstanceID, current.Generation)
		if proc == nil {
			return ProcessSnapshot{}, ErrNotRunning
		}
		stopping := current
		stopping.State = StateStopping
		if err := s.onStatus(ctx, stopping); err != nil {
			return ProcessSnapshot{}, err
		}
		proc, stopTimeout := s.takeProcess(current.InstanceID, current.Generation)
		if proc == nil {
			return ProcessSnapshot{}, ErrNotRunning
		}
		var err error
		if force {
			err = proc.Kill()
		} else {
			stopCtx := ctx
			var cancel context.CancelFunc
			if stopTimeout > 0 {
				stopCtx, cancel = context.WithTimeout(ctx, stopTimeout)
			}
			err = proc.Stop(stopCtx)
			if cancel != nil {
				cancel()
			}
		}
		if closeErr := proc.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			s.storeProcess(current.InstanceID, current.Generation, proc, stopTimeout)
			return ProcessSnapshot{}, err
		}
		stopped := current
		stopped.Generation = generation
		stopped.State = StateStopped
		return stopped, nil
	})
	if err != nil {
		return ProcessSnapshot{}, err
	}
	if err := s.onStatus(ctx, stopped); err != nil {
		return stopped, err
	}
	return stopped, nil
}

func (s *Supervisor) storeProcess(instanceID, generation int64, proc Process, stopTimeout time.Duration) {
	s.mu.Lock()
	key := processKey{instanceID: instanceID, generation: generation}
	s.processes[key] = proc
	s.stopTimeouts[key] = stopTimeout
	s.mu.Unlock()
}

func (s *Supervisor) acquireStartLock(instanceID int64) *startLock {
	s.mu.Lock()
	defer s.mu.Unlock()
	startMu := s.startMu[instanceID]
	if startMu == nil {
		startMu = &startLock{}
		s.startMu[instanceID] = startMu
	}
	startMu.refs++
	return startMu
}

func (s *Supervisor) releaseStartLock(instanceID int64, startMu *startLock) {
	startMu.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startMu[instanceID] != startMu {
		return
	}
	startMu.refs--
	if startMu.refs == 0 {
		delete(s.startMu, instanceID)
	}
}

func (s *Supervisor) getProcess(instanceID, generation int64) Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processes[processKey{instanceID: instanceID, generation: generation}]
}

func (s *Supervisor) takeProcess(instanceID, generation int64) (Process, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := processKey{instanceID: instanceID, generation: generation}
	proc := s.processes[key]
	stopTimeout := s.stopTimeouts[key]
	delete(s.processes, key)
	delete(s.stopTimeouts, key)
	return proc, stopTimeout
}

func (s *Supervisor) cleanupProcess(proc Process) {
	_ = proc.Kill()
	_ = proc.Close()
}

func (s *Supervisor) handleExit(instanceID, generation int64, snapshot ProcessSnapshot, proc Process, waitDone <-chan error) {
	waitErr := <-waitDone
	if proc, _ := s.takeProcess(instanceID, generation); proc == nil {
		return
	}
	exited := snapshot
	exited.State = StateStopped
	if waitErr != nil {
		exited.State = StateFailed
		exited.ExitError = waitErr.Error()
	}
	if err := s.registry.MarkExited(instanceID, generation, exited.State); errors.Is(err, ErrStaleGeneration) {
		return
	}
	if s.onExit != nil {
		s.onExit(exited)
	}
	_ = proc.Close()
}

type defaultPlatform struct{}

func (defaultPlatform) Start(ctx context.Context, config ProcessConfig) (Process, error) {
	return startPlatformProcess(ctx, platformProcessConfig{Path: config.Path, Args: config.Args, Env: config.Env})
}
