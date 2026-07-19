package instances

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

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

type testLifecycleDeps struct {
	repo       *fakeLifecycleRepo
	fs         *fakeLifecycleFS
	ports      *fakeLifecyclePorts
	versions   *fakeLifecycleVersions
	supervisor *fakeLifecycleSupervisor
	cache      *fakeLifecycleCache
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
		now:        func() time.Time { return now },
	}
	lc := NewLifecycleService(LifecycleOptions{Repository: deps.repo, Filesystem: deps.fs, PortAllocator: deps.ports, PortChecker: deps.ports, VersionResolver: deps.versions, Supervisor: deps.supervisor, DeviceCache: deps.cache, Now: deps.now, Sleep: func(_ context.Context, d time.Duration) error { deps.sleepCalls++; deps.slept += d; return nil }})
	return lc, deps
}

func testInstance(id int64, status string, port int) Instance {
	return testInstanceWithConfig(id, status, port, `{"args":["rest","--port=PORT"]}`)
}

func testInstanceWithConfig(id int64, status string, port int, config string) Instance {
	return Instance{ID: id, Key: "key", Name: "test", Port: &port, Status: status, Config: config, GOWAVersion: "v1.0.0"}
}

type fakeLifecycleRepo struct{ instances map[int64]Instance }

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
	item, ok := r.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	return item, nil
}
func (r *fakeLifecycleRepo) UpdateStatus(_ context.Context, id int64, status string, errorMessage *string) (Instance, error) {
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
	item, ok := r.instances[id]
	if !ok {
		return ErrNotFound
	}
	item.Port = port
	r.instances[id] = item
	return nil
}

type fakeLifecycleFS struct {
	ensured int64
	dir     string
}

func (f *fakeLifecycleFS) Ensure(_ context.Context, id int64) (string, error) {
	f.ensured = id
	return f.dir, nil
}
func (f *fakeLifecycleFS) StageDelete(context.Context, int64) (Trash, error) { return Trash{}, nil }
func (f *fakeLifecycleFS) Restore(context.Context, Trash) error              { return nil }
func (f *fakeLifecycleFS) Purge(context.Context, Trash) error                { return nil }

type fakeLifecyclePorts struct {
	available map[int]bool
	next      int
}

func (p *fakeLifecyclePorts) IsPortAvailable(port int) bool     { return p.available[port] }
func (p *fakeLifecyclePorts) Next(context.Context) (int, error) { return p.next, nil }

type fakeLifecycleVersions struct {
	path string
	err  error
}

func (v *fakeLifecycleVersions) ResolveVersionPath(context.Context, string) (string, error) {
	return v.path, v.err
}

type fakeLifecycleSupervisor struct {
	status                           map[int64]supervisor.ProcessSnapshot
	lastStart                        supervisor.StartConfig
	startCalls, stopCalls, killCalls int
	err                              error
}

func (s *fakeLifecycleSupervisor) Start(_ context.Context, config supervisor.StartConfig) (supervisor.ProcessSnapshot, error) {
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
	s.stopCalls++
	delete(s.status, id)
	return supervisor.ProcessSnapshot{InstanceID: id, State: supervisor.StateStopped}, nil
}
func (s *fakeLifecycleSupervisor) Kill(_ context.Context, id int64) (supervisor.ProcessSnapshot, error) {
	s.killCalls++
	delete(s.status, id)
	return supervisor.ProcessSnapshot{InstanceID: id, State: supervisor.StateStopped}, nil
}
func (s *fakeLifecycleSupervisor) Status(id int64) (supervisor.ProcessSnapshot, bool) {
	snapshot, ok := s.status[id]
	return snapshot, ok
}

type fakeLifecycleCache struct{ cleared []int64 }

func (c *fakeLifecycleCache) ClearCache(id int64) { c.cleared = append(c.cleared, id) }
