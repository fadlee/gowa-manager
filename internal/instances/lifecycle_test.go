package instances

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/monitoring"
	"github.com/fadlee/gowa-manager/internal/supervisor"
)

func TestLifecycleStartReturnsMissingInstance(t *testing.T) {
	lc, _ := newTestLifecycle(t)

	_, err := lc.Start(context.Background(), 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Start missing instance error = %v, want %v", err, ErrNotFound)
	}
}

func TestLifecycleStartReturnsAlreadyRunningActualProcess(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "running", 3000)
	startedAt := deps.now().Add(-2 * time.Second)
	deps.supervisor.status = map[int64]supervisor.ProcessSnapshot{1: {InstanceID: 1, State: supervisor.StateRunning, PID: 1234, StartedAt: startedAt}}

	status, err := lc.Start(context.Background(), 1)
	if err != nil {
		t.Fatalf("Start running instance error = %v", err)
	}
	if deps.supervisor.startCalls != 0 {
		t.Fatalf("Start calls = %d, want 0", deps.supervisor.startCalls)
	}
	if status.Status != "running" || status.PID == nil || *status.PID != 1234 || status.Uptime != 2 {
		t.Fatalf("status = %+v, want running pid 1234 uptime 2", status)
	}
}

func TestLifecycleStartPersistsFailedStatusWhenVersionMissing(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "stopped", 3000)
	deps.versions.err = errors.New("missing version")

	_, err := lc.Start(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "missing version") {
		t.Fatalf("Start missing version error = %v, want missing version", err)
	}
	if got := deps.repo.instances[1].Status; got != "failed" {
		t.Fatalf("persisted status = %q, want failed", got)
	}
	if deps.repo.instances[1].ErrorMessage == nil || !strings.Contains(*deps.repo.instances[1].ErrorMessage, "missing version") {
		t.Fatalf("persisted error = %v, want missing version", deps.repo.instances[1].ErrorMessage)
	}
}

func TestLifecycleStartReallocatesUnavailablePort(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "stopped", 3000)
	deps.ports.available[3000] = false
	deps.ports.next = 3001

	status, err := lc.Start(context.Background(), 1)
	if err != nil {
		t.Fatalf("Start error = %v", err)
	}
	if status.Port == nil || *status.Port != 3001 {
		t.Fatalf("status port = %v, want 3001", status.Port)
	}
	if deps.repo.instances[1].Port == nil || *deps.repo.instances[1].Port != 3001 {
		t.Fatalf("persisted port = %v, want 3001", deps.repo.instances[1].Port)
	}
	if got := deps.supervisor.lastStart.Args; !reflect.DeepEqual(got, []string{"rest", "--port=3001"}) {
		t.Fatalf("args = %#v, want port replaced with 3001", got)
	}
}

func TestLifecycleStartConcurrentUnavailablePortUsesRunningProcessPort(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "stopped", 3000)
	deps.ports.available[3000] = false
	deps.ports.nextPorts = []int{3001, 3002}
	deps.supervisor.blockStart = make(chan struct{})
	deps.supervisor.startEntered = make(chan struct{}, 2)

	var wg sync.WaitGroup
	statuses := make([]LifecycleStatus, 2)
	errs := make([]error, 2)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i], errs[i] = lc.Start(context.Background(), 1)
		}(i)
	}

	<-deps.supervisor.startEntered
	close(deps.supervisor.blockStart)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Start[%d] error = %v", i, err)
		}
		if statuses[i].Port == nil || *statuses[i].Port != 3001 {
			t.Fatalf("Start[%d] port = %v, want 3001", i, statuses[i].Port)
		}
	}
	if got := deps.supervisor.StartCalls(); got != 1 {
		t.Fatalf("supervisor start calls = %d, want 1", got)
	}
	if deps.repo.instances[1].Port == nil || *deps.repo.instances[1].Port != 3001 {
		t.Fatalf("persisted port = %v, want running process port 3001", deps.repo.instances[1].Port)
	}
	if got := deps.supervisor.LastStart().Args; !reflect.DeepEqual(got, []string{"rest", "--port=3001"}) {
		t.Fatalf("running args = %#v, want port 3001", got)
	}
}

func TestLifecycleStartEnsuresDirectoryAndProcessesArgsEnv(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstanceWithConfig(1, "stopped", 3000, `{"args":["rest","--port=PORT"],"env":{"SECRET":"value"},"flags":{"debug":true}}`)

	_, err := lc.Start(context.Background(), 1)
	if err != nil {
		t.Fatalf("Start error = %v", err)
	}
	if deps.fs.ensured != 1 {
		t.Fatalf("Ensure instance = %d, want 1", deps.fs.ensured)
	}
	if deps.supervisor.lastStart.Path != `C:\gowa\gowa.exe` {
		t.Fatalf("path = %q", deps.supervisor.lastStart.Path)
	}
	if !reflect.DeepEqual(deps.supervisor.lastStart.Args, []string{"rest", "--port=3000", "--debug=true"}) {
		t.Fatalf("args = %#v", deps.supervisor.lastStart.Args)
	}
	if deps.supervisor.lastStart.Env["PORT"] != "3000" || deps.supervisor.lastStart.Env["GOWA_DATA_DIR"] != `C:\data\instances\1` || deps.supervisor.lastStart.Env["SECRET"] != "value" {
		t.Fatalf("env = %#v", deps.supervisor.lastStart.Env)
	}
}

func TestLifecycleStartPersistsRunningAndFailedStatus(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "stopped", 3000)

	if _, err := lc.Start(context.Background(), 1); err != nil {
		t.Fatalf("Start error = %v", err)
	}
	if got := deps.repo.instances[1].Status; got != "running" {
		t.Fatalf("status = %q, want running", got)
	}
	if deps.repo.instances[1].ErrorMessage != nil {
		t.Fatalf("error = %v, want nil", deps.repo.instances[1].ErrorMessage)
	}

	deps.repo.instances[2] = testInstance(2, "stopped", 3002)
	deps.supervisor.err = errors.New("boom")
	_, err := lc.Start(context.Background(), 2)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Start error = %v, want boom", err)
	}
	if got := deps.repo.instances[2].Status; got != "failed" {
		t.Fatalf("status = %q, want failed", got)
	}
}

func TestLifecycleStartFailurePersistsSanitizedError(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstanceWithConfig(1, "stopped", 3000, `{"args":["rest","--basic-auth","admin:hunter2","--webhook-secret=super-secret","--token","abc123"],"env":{"PASSWORD":"hunter2","API_TOKEN":"abc123"}}`)
	deps.supervisor.err = errors.New("start failed args --basic-auth admin:hunter2 --webhook-secret=super-secret --token abc123 password: hunter2 PASSWORD=hunter2 API_TOKEN=abc123")

	_, err := lc.Start(context.Background(), 1)
	if err == nil {
		t.Fatalf("Start error = nil, want failure")
	}
	status, statusErr := lc.Status(context.Background(), 1)
	if statusErr != nil {
		t.Fatalf("Status error = %v", statusErr)
	}
	message := ""
	if deps.repo.instances[1].ErrorMessage != nil {
		message = *deps.repo.instances[1].ErrorMessage
	}
	for _, leaked := range []string{"hunter2", "super-secret", "abc123", "admin:hunter2"} {
		if strings.Contains(message, leaked) || strings.Contains(err.Error(), leaked) {
			t.Fatalf("secret %q leaked in persisted/returned error: persisted=%q returned=%q", leaked, message, err.Error())
		}
	}
	if status.Status != "failed" {
		t.Fatalf("returned status = %q, want failed", status.Status)
	}
}

func TestSafeSupervisorExitMessageRedactsSeparatedSecretValues(t *testing.T) {
	message := safeSupervisorExitMessage("failed --password hunter2 --token abc123 password: swordfish API_TOKEN=abc123 WEBHOOK_SECRET=super-secret key value")
	for _, leaked := range []string{"hunter2", "abc123", "swordfish", "super-secret", "value"} {
		if strings.Contains(message, leaked) {
			t.Fatalf("message leaks %q: %q", leaked, message)
		}
	}
	if !strings.Contains(message, "failed") {
		t.Fatalf("message = %q, want non-secret context", message)
	}
}

func TestLifecycleRestartUsesInjectedSleeper(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "running", 3000)
	deps.supervisor.status = map[int64]supervisor.ProcessSnapshot{1: {InstanceID: 1, State: supervisor.StateRunning, PID: 10, StartedAt: deps.now()}}

	if _, err := lc.Restart(context.Background(), 1); err != nil {
		t.Fatalf("Restart error = %v", err)
	}
	if deps.sleepCalls != 1 || deps.slept != time.Second {
		t.Fatalf("sleep calls/duration = %d/%s, want 1/1s", deps.sleepCalls, deps.slept)
	}
	if deps.supervisor.stopCalls != 1 || deps.supervisor.startCalls != 1 {
		t.Fatalf("stop/start calls = %d/%d, want 1/1", deps.supervisor.stopCalls, deps.supervisor.startCalls)
	}
}

func TestLifecycleStopAndKillClearDeviceCache(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "running", 3000)
	deps.supervisor.status = map[int64]supervisor.ProcessSnapshot{1: {InstanceID: 1, State: supervisor.StateRunning, PID: 10, StartedAt: deps.now()}}

	if _, err := lc.Stop(context.Background(), 1); err != nil {
		t.Fatalf("Stop error = %v", err)
	}
	deps.supervisor.status[1] = supervisor.ProcessSnapshot{InstanceID: 1, State: supervisor.StateRunning, PID: 11, StartedAt: deps.now()}
	if _, err := lc.Kill(context.Background(), 1); err != nil {
		t.Fatalf("Kill error = %v", err)
	}
	if !reflect.DeepEqual(deps.cache.cleared, []int64{1, 1}) {
		t.Fatalf("cleared cache = %#v, want [1 1]", deps.cache.cleared)
	}
	if got := deps.repo.instances[1].Status; got != "stopped" {
		t.Fatalf("status = %q, want stopped", got)
	}
}

func TestLifecycleStatusUsesRegistryFieldsAndLastKnownStatus(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "failed", 3000)
	deps.repo.instances[2] = testInstance(2, "running", 3002)
	deps.supervisor.status = map[int64]supervisor.ProcessSnapshot{2: {InstanceID: 2, State: supervisor.StateRunning, PID: 4321, StartedAt: deps.now().Add(-3 * time.Second)}}

	failed, err := lc.Status(context.Background(), 1)
	if err != nil {
		t.Fatalf("Status failed instance error = %v", err)
	}
	if failed.Status != "failed" || failed.PID != nil || failed.Uptime != 0 {
		t.Fatalf("failed status = %+v", failed)
	}
	running, err := lc.Status(context.Background(), 2)
	if err != nil {
		t.Fatalf("Status running instance error = %v", err)
	}
	if running.Status != "running" || running.PID == nil || *running.PID != 4321 || running.Uptime != 3 {
		t.Fatalf("running status = %+v, want pid 4321 uptime 3", running)
	}
}

func TestLifecycleStatusIncludesMonitorResourcesForManagedRunningPID(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "running", 3000)
	deps.monitor.resources = monitoring.Resources{CPUPercent: 12.5, MemoryMB: 128, MemoryPercent: 25}
	deps.monitor.ok = true
	deps.supervisor.status = map[int64]supervisor.ProcessSnapshot{1: {InstanceID: 1, State: supervisor.StateRunning, PID: 4321, StartedAt: deps.now()}}

	status, err := lc.Status(context.Background(), 1)
	if err != nil {
		t.Fatalf("Status error = %v", err)
	}
	if status.Resources == nil || status.Resources.CPUPercent != 12.5 || status.Resources.MemoryMB != 128 || status.Resources.MemoryPercent != 25 {
		t.Fatalf("resources = %+v, want monitor resources", status.Resources)
	}
	if deps.monitor.pid != 4321 || deps.monitor.instanceID != 1 {
		t.Fatalf("monitor call = instance %d pid %d, want instance 1 pid 4321", deps.monitor.instanceID, deps.monitor.pid)
	}
}

func TestLifecycleStatusToleratesMonitorFailureAndSkipsNonRunningPID(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "running", 3000)
	deps.repo.instances[2] = testInstance(2, "stopped", 3002)
	deps.monitor.ok = false
	deps.supervisor.status = map[int64]supervisor.ProcessSnapshot{
		1: {InstanceID: 1, State: supervisor.StateRunning, PID: 4321, StartedAt: deps.now()},
		2: {InstanceID: 2, State: supervisor.StateStopping, PID: 55, StartedAt: deps.now()},
	}

	status, err := lc.Status(context.Background(), 1)
	if err != nil {
		t.Fatalf("Status monitor failure error = %v", err)
	}
	if status.Resources != nil {
		t.Fatalf("resources = %+v, want omitted on monitor failure", status.Resources)
	}
	deps.monitor.calls = 0
	status, err = lc.Status(context.Background(), 2)
	if err != nil {
		t.Fatalf("Status stopping error = %v", err)
	}
	if status.Resources != nil || deps.monitor.calls != 0 {
		t.Fatalf("stopping resources/calls = %+v/%d, want no sampling", status.Resources, deps.monitor.calls)
	}
}

func TestLifecycleStopKillAndExitClearMonitor(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "running", 3000)
	deps.supervisor.status = map[int64]supervisor.ProcessSnapshot{1: {InstanceID: 1, State: supervisor.StateRunning, PID: 10, StartedAt: deps.now()}}

	if _, err := lc.Stop(context.Background(), 1); err != nil {
		t.Fatalf("Stop error = %v", err)
	}
	deps.supervisor.status[1] = supervisor.ProcessSnapshot{InstanceID: 1, State: supervisor.StateRunning, PID: 11, StartedAt: deps.now()}
	if _, err := lc.Kill(context.Background(), 1); err != nil {
		t.Fatalf("Kill error = %v", err)
	}
	lc.PersistSupervisorExit(supervisor.ProcessSnapshot{InstanceID: 1, State: supervisor.StateRunning, PID: 11})
	if !reflect.DeepEqual(deps.monitor.cleared, []int64{1, 1, 1}) {
		t.Fatalf("monitor cleared = %#v, want [1 1 1]", deps.monitor.cleared)
	}
}

func TestLifecycleSupervisorExitCallbackPersistsFailedAndClearsCache(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "running", 3000)
	secret := "--token=super-secret-value"

	lc.PersistSupervisorExit(supervisor.ProcessSnapshot{InstanceID: 1, State: supervisor.StateRunning, PID: 42, ExitError: "process exited with status 1 " + secret})

	instance := deps.repo.instances[1]
	if instance.Status != "failed" {
		t.Fatalf("status = %q, want failed", instance.Status)
	}
	if instance.ErrorMessage == nil || !strings.Contains(*instance.ErrorMessage, "process exited with status 1") {
		t.Fatalf("error message = %v, want safe exit error", instance.ErrorMessage)
	}
	if instance.ErrorMessage != nil && strings.Contains(*instance.ErrorMessage, secret) {
		t.Fatalf("error message exposes secret: %q", *instance.ErrorMessage)
	}
	if !reflect.DeepEqual(deps.cache.cleared, []int64{1}) {
		t.Fatalf("cleared cache = %#v, want [1]", deps.cache.cleared)
	}
}

func TestLifecycleSupervisorExitCallbackPersistsStoppedAndClearsCache(t *testing.T) {
	lc, deps := newTestLifecycle(t)
	deps.repo.instances[1] = testInstance(1, "running", 3000)

	lc.PersistSupervisorExit(supervisor.ProcessSnapshot{InstanceID: 1, State: supervisor.StateRunning, PID: 42})

	instance := deps.repo.instances[1]
	if instance.Status != "stopped" {
		t.Fatalf("status = %q, want stopped", instance.Status)
	}
	if instance.ErrorMessage != nil {
		t.Fatalf("error message = %v, want nil", instance.ErrorMessage)
	}
	if !reflect.DeepEqual(deps.cache.cleared, []int64{1}) {
		t.Fatalf("cleared cache = %#v, want [1]", deps.cache.cleared)
	}
}

type testLifecycleDeps struct {
	repo       *fakeLifecycleRepo
	fs         *fakeLifecycleFS
	ports      *fakeLifecyclePorts
	versions   *fakeLifecycleVersions
	supervisor *fakeLifecycleSupervisor
	cache      *fakeLifecycleCache
	monitor    *fakeLifecycleMonitor
	now        func() time.Time
	slept      time.Duration
	sleepCalls int
}

func newTestLifecycle(t *testing.T) (*LifecycleService, *testLifecycleDeps) {
	t.Helper()
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	deps := &testLifecycleDeps{
		repo:       &fakeLifecycleRepo{instances: map[int64]Instance{}},
		fs:         &fakeLifecycleFS{dir: `C:\data\instances\1`},
		ports:      &fakeLifecyclePorts{available: map[int]bool{3000: true, 3001: true, 3002: true}},
		versions:   &fakeLifecycleVersions{path: `C:\gowa\gowa.exe`},
		supervisor: &fakeLifecycleSupervisor{status: map[int64]supervisor.ProcessSnapshot{}},
		cache:      &fakeLifecycleCache{},
		monitor:    &fakeLifecycleMonitor{},
		now:        func() time.Time { return now },
	}
	lc := NewLifecycleService(LifecycleOptions{Repository: deps.repo, Filesystem: deps.fs, PortAllocator: deps.ports, PortChecker: deps.ports, VersionResolver: deps.versions, Supervisor: deps.supervisor, DeviceCache: deps.cache, Monitor: deps.monitor, Now: deps.now, Sleep: func(_ context.Context, d time.Duration) error { deps.sleepCalls++; deps.slept += d; return nil }})
	return lc, deps
}

func testInstance(id int64, status string, port int) Instance {
	return testInstanceWithConfig(id, status, port, `{"args":["rest","--port=PORT"]}`)
}

func testInstanceWithConfig(id int64, status string, port int, config string) Instance {
	return Instance{ID: id, Key: "key", Name: "test", Port: &port, Status: status, Config: config, GOWAVersion: "v1.0.0"}
}

type fakeLifecycleRepo struct {
	mu        sync.Mutex
	instances map[int64]Instance
}

func (r *fakeLifecycleRepo) List(context.Context) ([]Instance, error) { return nil, nil }
func (r *fakeLifecycleRepo) FindByKey(context.Context, string) (Instance, error) {
	return Instance{}, ErrNotFound
}
func (r *fakeLifecycleRepo) Create(context.Context, CreateInput) (Instance, error) {
	return Instance{}, nil
}
func (r *fakeLifecycleRepo) Update(context.Context, UpdateInput) (Instance, error) {
	return Instance{}, nil
}
func (r *fakeLifecycleRepo) Delete(context.Context, int64) error { return nil }
func (r *fakeLifecycleRepo) ClearError(ctx context.Context, id int64) (Instance, error) {
	return r.UpdateStatus(ctx, id, r.instances[id].Status, nil)
}
func (r *fakeLifecycleRepo) FindByID(_ context.Context, id int64) (Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	return item, nil
}
func (r *fakeLifecycleRepo) UpdateStatus(_ context.Context, id int64, status string, errorMessage *string) (Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	item.Status = status
	item.ErrorMessage = errorMessage
	r.instances[id] = item
	return item, nil
}
func (r *fakeLifecycleRepo) UpdatePort(_ context.Context, id int64, port *int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.instances[id]
	if !ok {
		return ErrNotFound
	}
	item.Port = port
	r.instances[id] = item
	return nil
}

type fakeLifecycleFS struct {
	mu      sync.Mutex
	ensured int64
	dir     string
}

func (f *fakeLifecycleFS) Ensure(_ context.Context, id int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensured = id
	return f.dir, nil
}
func (f *fakeLifecycleFS) InstanceDir(int64) (string, error)                 { return f.dir, nil }
func (f *fakeLifecycleFS) StageDelete(context.Context, int64) (Trash, error) { return Trash{}, nil }
func (f *fakeLifecycleFS) Restore(context.Context, Trash) error              { return nil }
func (f *fakeLifecycleFS) Purge(context.Context, Trash) error                { return nil }

type fakeLifecyclePorts struct {
	mu        sync.Mutex
	available map[int]bool
	next      int
	nextPorts []int
}

func (p *fakeLifecyclePorts) IsPortAvailable(port int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.available[port]
}
func (p *fakeLifecyclePorts) Next(context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.nextPorts) > 0 {
		port := p.nextPorts[0]
		p.nextPorts = p.nextPorts[1:]
		return port, nil
	}
	return p.next, nil
}

type fakeLifecycleVersions struct {
	path string
	err  error
}

func (v *fakeLifecycleVersions) ResolveVersionPath(context.Context, string) (string, error) {
	return v.path, v.err
}

type fakeLifecycleSupervisor struct {
	mu                               sync.Mutex
	status                           map[int64]supervisor.ProcessSnapshot
	lastStart                        supervisor.StartConfig
	startCalls, stopCalls, killCalls int
	err                              error
	blockStart                       chan struct{}
	startEntered                     chan struct{}
}

func (s *fakeLifecycleSupervisor) Start(_ context.Context, config supervisor.StartConfig) (supervisor.ProcessSnapshot, error) {
	if s.startEntered != nil {
		s.startEntered <- struct{}{}
	}
	if s.blockStart != nil {
		<-s.blockStart
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startCalls++
	s.lastStart = config
	if s.err != nil {
		return supervisor.ProcessSnapshot{}, s.err
	}
	snapshot := supervisor.ProcessSnapshot{InstanceID: config.InstanceID, State: supervisor.StateRunning, PID: 99, StartedAt: config.StartedAt}
	s.status[config.InstanceID] = snapshot
	return snapshot, nil
}
func (s *fakeLifecycleSupervisor) Stop(_ context.Context, id int64) (supervisor.ProcessSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopCalls++
	delete(s.status, id)
	return supervisor.ProcessSnapshot{InstanceID: id, State: supervisor.StateStopped}, nil
}
func (s *fakeLifecycleSupervisor) Kill(_ context.Context, id int64) (supervisor.ProcessSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.killCalls++
	delete(s.status, id)
	return supervisor.ProcessSnapshot{InstanceID: id, State: supervisor.StateStopped}, nil
}
func (s *fakeLifecycleSupervisor) Status(id int64) (supervisor.ProcessSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.status[id]
	return snapshot, ok
}

func (s *fakeLifecycleSupervisor) StartCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startCalls
}

func (s *fakeLifecycleSupervisor) LastStart() supervisor.StartConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastStart
}

type fakeLifecycleCache struct{ cleared []int64 }

func (c *fakeLifecycleCache) ClearCache(id int64) { c.cleared = append(c.cleared, id) }

type fakeLifecycleMonitor struct {
	resources  monitoring.Resources
	ok         bool
	calls      int
	instanceID int64
	pid        int
	cleared    []int64
}

func (m *fakeLifecycleMonitor) Resources(_ context.Context, instanceID int64, pid int, _ string) (monitoring.Resources, bool) {
	m.calls++
	m.instanceID = instanceID
	m.pid = pid
	return m.resources, m.ok
}

func (m *fakeLifecycleMonitor) Clear(instanceID int64) { m.cleared = append(m.cleared, instanceID) }
