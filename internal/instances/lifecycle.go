package instances

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fadlee/gowa-manager/internal/monitoring"
	"github.com/fadlee/gowa-manager/internal/supervisor"
)

type PortChecker interface {
	IsPortAvailable(port int) bool
}

type VersionResolver interface {
	ResolveVersionPath(context.Context, string) (string, error)
}

type ProcessSupervisor interface {
	Start(context.Context, supervisor.StartConfig) (supervisor.ProcessSnapshot, error)
	Stop(context.Context, int64) (supervisor.ProcessSnapshot, error)
	Kill(context.Context, int64) (supervisor.ProcessSnapshot, error)
	Status(int64) (supervisor.ProcessSnapshot, bool)
}

type ProcessMonitor interface {
	Resources(context.Context, int64, int, string) (monitoring.Resources, bool)
	Clear(int64)
}

type InstanceDirResolver interface {
	InstanceDir(int64) (string, error)
}

type LifecycleOptions struct {
	Repository      Repository
	Filesystem      InstanceFilesystem
	PortAllocator   PortAllocator
	PortChecker     PortChecker
	VersionResolver VersionResolver
	Supervisor      ProcessSupervisor
	DeviceCache     DeviceCacheCleaner
	Monitor         ProcessMonitor
	MonitorTimeout  time.Duration
	Now             func() time.Time
	Sleep           func(context.Context, time.Duration) error
}

type LifecycleStatus struct {
	ID        int64                 `json:"id"`
	Name      string                `json:"name"`
	Status    string                `json:"status"`
	Port      *int                  `json:"port"`
	PID       *int                  `json:"pid"`
	Uptime    int64                 `json:"uptime,omitempty"`
	Resources *monitoring.Resources `json:"resources,omitempty"`
}

type LifecycleService struct {
	repo           Repository
	fs             InstanceFilesystem
	ports          PortAllocator
	checker        PortChecker
	versions       VersionResolver
	supervisor     ProcessSupervisor
	cache          DeviceCacheCleaner
	monitor        ProcessMonitor
	monitorTimeout time.Duration
	now            func() time.Time
	sleep          func(context.Context, time.Duration) error

	mu      sync.Mutex
	startMu map[int64]*startLock
}

type startLock struct {
	mu   sync.Mutex
	refs int
}

func NewLifecycleService(opts LifecycleOptions) *LifecycleService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	monitorTimeout := opts.MonitorTimeout
	if monitorTimeout <= 0 {
		monitorTimeout = 200 * time.Millisecond
	}
	return &LifecycleService{repo: opts.Repository, fs: opts.Filesystem, ports: opts.PortAllocator, checker: opts.PortChecker, versions: opts.VersionResolver, supervisor: opts.Supervisor, cache: opts.DeviceCache, monitor: opts.Monitor, monitorTimeout: monitorTimeout, now: now, sleep: sleep, startMu: make(map[int64]*startLock)}
}

func (s *LifecycleService) Start(ctx context.Context, id int64) (LifecycleStatus, error) {
	startMu := s.acquireStartLock(id)
	startMu.mu.Lock()
	defer s.releaseStartLock(id, startMu)

	instance, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return LifecycleStatus{}, err
	}
	if snapshot, ok := s.supervisor.Status(id); ok && (snapshot.State == supervisor.StateStarting || snapshot.State == supervisor.StateRunning) {
		return s.statusFrom(ctx, instance, snapshot, true), nil
	}
	path, err := s.versions.ResolveVersionPath(ctx, instance.GOWAVersion)
	if err != nil {
		return LifecycleStatus{}, s.persistFailed(ctx, id, err)
	}
	port, err := s.ensurePort(ctx, instance)
	if err != nil {
		return LifecycleStatus{}, s.persistFailed(ctx, id, err)
	}
	instance.Port = &port
	dir, err := s.fs.Ensure(ctx, id)
	if err != nil {
		return LifecycleStatus{}, s.persistFailed(ctx, id, err)
	}
	config := ParseConfig(instance.Config)
	snapshot, err := s.supervisor.Start(ctx, supervisor.StartConfig{InstanceID: id, Path: path, Args: ProcessArgs(config, port), Env: ParseEnvironmentVars(config, port, map[string]string{"GOWA_DATA_DIR": dir}), Dir: dir, StartedAt: s.now()})
	if err != nil {
		return LifecycleStatus{}, s.persistFailed(ctx, id, err)
	}
	if _, err := s.repo.UpdateStatus(ctx, id, "running", nil); err != nil {
		return LifecycleStatus{}, err
	}
	instance.Status = "running"
	instance.ErrorMessage = nil
	return s.statusFrom(ctx, instance, snapshot, true), nil
}

func (s *LifecycleService) Stop(ctx context.Context, id int64) (LifecycleStatus, error) {
	return s.stopWith(ctx, id, false)
}

func (s *LifecycleService) Kill(ctx context.Context, id int64) (LifecycleStatus, error) {
	return s.stopWith(ctx, id, true)
}

func (s *LifecycleService) Restart(ctx context.Context, id int64) (LifecycleStatus, error) {
	if _, err := s.Stop(ctx, id); err != nil {
		return LifecycleStatus{}, err
	}
	if err := s.sleep(ctx, time.Second); err != nil {
		return LifecycleStatus{}, err
	}
	return s.Start(ctx, id)
}

func (s *LifecycleService) Status(ctx context.Context, id int64) (LifecycleStatus, error) {
	instance, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return LifecycleStatus{}, err
	}
	if snapshot, ok := s.supervisor.Status(id); ok && (snapshot.State == supervisor.StateStarting || snapshot.State == supervisor.StateRunning || snapshot.State == supervisor.StateStopping) {
		return s.statusFrom(ctx, instance, snapshot, true), nil
	}
	return s.statusFrom(ctx, instance, supervisor.ProcessSnapshot{}, false), nil
}

func (s *LifecycleService) PersistSupervisorStatus(ctx context.Context, snapshot supervisor.ProcessSnapshot) error {
	status := string(snapshot.State)
	var message *string
	if snapshot.State == supervisor.StateFailed {
		safeMessage := safeSupervisorExitMessage(snapshot.ExitError)
		message = &safeMessage
	}
	_, err := s.repo.UpdateStatus(ctx, snapshot.InstanceID, status, message)
	if err == nil && (snapshot.State == supervisor.StateStopped || snapshot.State == supervisor.StateFailed) {
		s.clearRuntimeCaches(snapshot.InstanceID)
	}
	return err
}

func (s *LifecycleService) PersistSupervisorExit(snapshot supervisor.ProcessSnapshot) {
	if snapshot.ExitError != "" {
		snapshot.State = supervisor.StateFailed
	} else {
		snapshot.State = supervisor.StateStopped
	}
	_ = s.PersistSupervisorStatus(context.Background(), snapshot)
}

func safeSupervisorExitMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "process exited unexpectedly"
	}
	message = redactSecretValues(message)
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}

var secretNamePattern = regexp.MustCompile(`(?i)(token|secret|password|key|basic-auth)`)

func redactSecretValues(message string) string {
	fields := strings.Fields(message)
	redactNext := false
	for i, field := range fields {
		trimmed := strings.Trim(field, `"'`)
		lower := strings.ToLower(trimmed)
		if redactNext {
			fields[i] = "[redacted]"
			redactNext = false
			continue
		}
		if secretNamePattern.MatchString(lower) {
			fields[i] = redactSecretField(field)
			if !strings.ContainsAny(trimmed, "=:") || strings.HasSuffix(trimmed, "=") || strings.HasSuffix(trimmed, ":") {
				redactNext = true
			}
		}
	}
	return strings.Join(fields, " ")
}

func redactSecretField(field string) string {
	for _, sep := range []string{"=", ":"} {
		if idx := strings.Index(field, sep); idx >= 0 {
			return field[:idx+1] + "[redacted]"
		}
	}
	return "[redacted]"
}

func (s *LifecycleService) ensurePort(ctx context.Context, instance Instance) (int, error) {
	if instance.Port != nil && (s.checker == nil || s.checker.IsPortAvailable(*instance.Port)) {
		return *instance.Port, nil
	}
	if s.ports == nil {
		return 0, fmt.Errorf("instance port unavailable")
	}
	port, err := s.ports.Next(ctx)
	if err != nil {
		return 0, err
	}
	if err := s.repo.UpdatePort(ctx, instance.ID, &port); err != nil {
		return 0, err
	}
	return port, nil
}

func (s *LifecycleService) stopWith(ctx context.Context, id int64, force bool) (LifecycleStatus, error) {
	instance, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return LifecycleStatus{}, err
	}
	var snapshot supervisor.ProcessSnapshot
	if current, ok := s.supervisor.Status(id); ok && current.State != supervisor.StateStopped && current.State != supervisor.StateFailed {
		if force {
			snapshot, err = s.supervisor.Kill(ctx, id)
		} else {
			snapshot, err = s.supervisor.Stop(ctx, id)
		}
		if err != nil {
			return LifecycleStatus{}, err
		}
	} else {
		snapshot = current
		snapshot.State = supervisor.StateStopped
	}
	if _, err := s.repo.UpdateStatus(ctx, id, "stopped", nil); err != nil {
		return LifecycleStatus{}, err
	}
	s.clearRuntimeCaches(id)
	instance.Status = "stopped"
	instance.ErrorMessage = nil
	return s.statusFrom(ctx, instance, snapshot, false), nil
}

func (s *LifecycleService) persistFailed(ctx context.Context, id int64, err error) error {
	message := safeSupervisorExitMessage(err.Error())
	_, updateErr := s.repo.UpdateStatus(ctx, id, "failed", &message)
	if updateErr != nil {
		return fmt.Errorf("%w: %v", err, updateErr)
	}
	return errors.New(message)
}

func (s *LifecycleService) acquireStartLock(instanceID int64) *startLock {
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

func (s *LifecycleService) releaseStartLock(instanceID int64, startMu *startLock) {
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

func (s *LifecycleService) statusFrom(ctx context.Context, instance Instance, snapshot supervisor.ProcessSnapshot, managed bool) LifecycleStatus {
	status := LifecycleStatus{ID: instance.ID, Name: instance.Name, Status: instance.Status, Port: instance.Port}
	if !managed {
		return status
	}
	status.Status = string(snapshot.State)
	if snapshot.State == supervisor.StateRunning || snapshot.State == supervisor.StateStarting || snapshot.State == supervisor.StateStopping {
		pid := snapshot.PID
		status.PID = &pid
	}
	if !snapshot.StartedAt.IsZero() {
		status.Uptime = int64(s.now().Sub(snapshot.StartedAt).Seconds())
		if status.Uptime < 0 {
			status.Uptime = 0
		}
	}
	if snapshot.State == supervisor.StateRunning && snapshot.PID > 0 && s.monitor != nil {
		dataDir := ""
		if resolver, ok := s.fs.(InstanceDirResolver); ok {
			if dir, err := resolver.InstanceDir(instance.ID); err == nil {
				dataDir = dir
			}
		}
		monitorCtx, cancel := context.WithTimeout(ctx, s.monitorTimeout)
		defer cancel()
		if resources, ok := s.monitor.Resources(monitorCtx, instance.ID, snapshot.PID, dataDir); ok {
			status.Resources = &resources
		}
	}
	return status
}

func (s *LifecycleService) clearRuntimeCaches(id int64) {
	if s.cache != nil {
		s.cache.ClearCache(id)
	}
	if s.monitor != nil {
		s.monitor.Clear(id)
	}
}
