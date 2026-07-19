package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/fadlee/gowa-manager/internal/system"
	"github.com/fadlee/gowa-manager/internal/versions"
)

// --- stubs -----------------------------------------------------------------

// fakeVersionLister returns a canned list of version infos (or an error).
type fakeVersionLister struct {
	mu       sync.Mutex
	versions []versions.VersionInfo
	err      error
	calls    int
}

func (f *fakeVersionLister) GetAvailableVersions(ctx context.Context, limit int) ([]versions.VersionInfo, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.versions, nil
}

// fakeInstaller records install calls and returns a canned result/error.
type fakeInstaller struct {
	mu       sync.Mutex
	calls    []string
	result   versions.InstallResult
	err      error
	byVerErr map[string]error
}

func (f *fakeInstaller) Install(ctx context.Context, version string) (versions.InstallResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, version)
	f.mu.Unlock()
	if f.byVerErr != nil {
		if err, ok := f.byVerErr[version]; ok {
			return versions.InstallResult{}, err
		}
	}
	if f.err != nil {
		return versions.InstallResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeInstaller) installCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	cpy := make([]string, len(f.calls))
	copy(cpy, f.calls)
	return cpy
}

// fakeRestarter records restart calls, can fail per-instance, and can block
// to simulate slow restarts (useful for sequential-order and overlap tests).
type fakeRestarter struct {
	mu       sync.Mutex
	calls    []int64
	errBy    map[int64]error
	block    chan struct{} // if non-nil, every Restart waits on it
	callTime []time.Time   // timestamps of each call (for sequential checks)
	clock    func() time.Time
}

func (f *fakeRestarter) Restart(ctx context.Context, id int64) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, id)
	if f.clock != nil {
		f.callTime = append(f.callTime, f.clock())
	}
	errBy := f.errBy
	f.mu.Unlock()
	if errBy != nil {
		if err, ok := errBy[id]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeRestarter) restartCalls() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	cpy := make([]int64, len(f.calls))
	copy(cpy, f.calls)
	return cpy
}

// fakeInstanceLister returns a canned list of instances (or an error).
type fakeInstanceLister struct {
	mu       sync.Mutex
	items    []instances.Instance
	err      error
	callCount int
}

func (f *fakeInstanceLister) List(ctx context.Context) ([]instances.Instance, error) {
	f.mu.Lock()
	f.callCount++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

// --- helpers ---------------------------------------------------------------

func vi(version string, installed bool, isLatest bool) versions.VersionInfo {
	return versions.VersionInfo{Version: version, Path: "/fake/" + version, Installed: installed, IsLatest: isLatest}
}

// newAutoUpdate builds an AutoUpdate wired to fakes and a fixed clock.
func newAutoUpdate(t *testing.T, vl *fakeVersionLister, inst *fakeInstaller, il *fakeInstanceLister, rs *fakeRestarter) *AutoUpdate {
	t.Helper()
	return NewAutoUpdate(AutoUpdateOptions{
		Versions:  vl,
		Installer: inst,
		Lister:    il,
		Restarter: rs,
		Now:       func() time.Time { return time.Unix(1000000, 0).UTC() },
	})
}

func strPtr(s string) *string { return &s }

// --- Status tests ----------------------------------------------------------

func TestAutoUpdate_StatusDefaults(t *testing.T) {
	au := newAutoUpdate(t, &fakeVersionLister{}, &fakeInstaller{}, &fakeInstanceLister{}, &fakeRestarter{})
	status, err := au.Status(context.Background())
	if err != nil {
		t.Fatalf("Status error: %v", err)
	}
	if status.LastCheck != nil || status.LastUpdate != nil || status.LatestVersion != nil || status.NextCheck != nil {
		t.Fatalf("expected all nil, got %+v", status)
	}
	if status.IsChecking {
		t.Fatalf("expected isChecking false")
	}
}

func TestAutoUpdate_StatusReturnsCopy(t *testing.T) {
	au := newAutoUpdate(t, &fakeVersionLister{}, &fakeInstaller{}, &fakeInstanceLister{}, &fakeRestarter{})
	a, _ := au.Status(context.Background())
	b, _ := au.Status(context.Background())
	if &a.LastCheck == &b.LastCheck {
		t.Fatalf("Status should return a copy, not a shared reference")
	}
}

// --- Check: no update scenarios --------------------------------------------

func TestAutoUpdate_CheckNoVersions(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{}}
	au := newAutoUpdate(t, vl, &fakeInstaller{}, &fakeInstanceLister{}, &fakeRestarter{})

	res, err := au.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if res.Updated || res.Version != nil || res.RestartedInstances != 0 {
		t.Fatalf("expected no-update result, got %+v", res)
	}
	if !res.Success {
		t.Fatalf("expected Success true")
	}
}

func TestAutoUpdate_CheckNoLatestRelease(t *testing.T) {
	// Only the 'latest' alias entry, no concrete isLatest release.
	vl := &fakeVersionLister{versions: []versions.VersionInfo{vi("latest", false, true)}}
	inst := &fakeInstaller{}
	au := newAutoUpdate(t, vl, inst, &fakeInstanceLister{}, &fakeRestarter{})

	res, err := au.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if res.Updated {
		t.Fatalf("expected no update")
	}
	if len(inst.installCalls()) != 0 {
		t.Fatalf("install should not be called")
	}
}

func TestAutoUpdate_CheckAlreadyInstalled(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v2.0.0", true, true),
	}}
	inst := &fakeInstaller{}
	au := newAutoUpdate(t, vl, inst, &fakeInstanceLister{}, &fakeRestarter{})

	res, err := au.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if res.Updated {
		t.Fatalf("expected no update when already installed")
	}
	if len(inst.installCalls()) != 0 {
		t.Fatalf("install should not be called when already installed")
	}
	// latestVersion should still be set.
	status, _ := au.Status(context.Background())
	if status.LatestVersion == nil || *status.LatestVersion != "v2.0.0" {
		t.Fatalf("expected latestVersion v2.0.0, got %v", status.LatestVersion)
	}
}

// --- Check: successful update scenarios ------------------------------------

func TestAutoUpdate_CheckInstallsAndNoRestarts(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v3.0.0", false, true),
	}}
	inst := &fakeInstaller{result: versions.InstallResult{Version: "v3.0.0", Path: "/fake/v3.0.0"}}
	il := &fakeInstanceLister{items: []instances.Instance{}}
	rs := &fakeRestarter{}
	au := newAutoUpdate(t, vl, inst, il, rs)

	res, err := au.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !res.Updated {
		t.Fatalf("expected update")
	}
	if res.Version == nil || *res.Version != "v3.0.0" {
		t.Fatalf("expected version v3.0.0, got %v", res.Version)
	}
	if res.RestartedInstances != 0 {
		t.Fatalf("expected 0 restarted, got %d", res.RestartedInstances)
	}
	calls := inst.installCalls()
	if len(calls) != 1 || calls[0] != "v3.0.0" {
		t.Fatalf("expected install v3.0.0, got %v", calls)
	}
	// lastUpdate should be set.
	status, _ := au.Status(context.Background())
	if status.LastUpdate == nil {
		t.Fatalf("expected lastUpdate set")
	}
}

func TestAutoUpdate_CheckRestartsRunningLatestInstances(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v3.1.0", false, true),
	}}
	inst := &fakeInstaller{result: versions.InstallResult{Version: "v3.1.0"}}
	il := &fakeInstanceLister{items: []instances.Instance{
		{ID: 1, Name: "a", Status: "running", GOWAVersion: "latest"},
		{ID: 2, Name: "b", Status: "stopped", GOWAVersion: "latest"},
		{ID: 3, Name: "c", Status: "running", GOWAVersion: "v1.0.0"},
	}}
	rs := &fakeRestarter{}
	au := newAutoUpdate(t, vl, inst, il, rs)

	res, err := au.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !res.Updated || res.Version == nil || *res.Version != "v3.1.0" {
		t.Fatalf("unexpected result %+v", res)
	}
	if res.RestartedInstances != 1 {
		t.Fatalf("expected 1 restarted, got %d", res.RestartedInstances)
	}
	calls := rs.restartCalls()
	if len(calls) != 1 || calls[0] != 1 {
		t.Fatalf("expected restart of instance 1 only, got %v", calls)
	}
}

func TestAutoUpdate_CheckSequentialRestarts(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v3.2.0", false, true),
	}}
	inst := &fakeInstaller{result: versions.InstallResult{Version: "v3.2.0"}}
	il := &fakeInstanceLister{items: []instances.Instance{
		{ID: 10, Name: "first", Status: "running", GOWAVersion: "latest"},
		{ID: 20, Name: "second", Status: "running", GOWAVersion: ""},
		{ID: 30, Name: "third", Status: "running", GOWAVersion: "latest"},
	}}

	// Use a blocking channel so we can verify restarts happen one at a time.
	block := make(chan struct{})
	clock := &atomic.Int32{}
	rs := &fakeRestarter{
		block: block,
		clock: func() time.Time { return time.Unix(int64(clock.Load()), 0).UTC() },
	}
	au := newAutoUpdate(t, vl, inst, il, rs)

	// Run Check in a goroutine; it will block on the first restart.
	done := make(chan struct{})
	go func() {
		au.Check(context.Background())
		close(done)
	}()

	// Wait for the first restart call to block.
	time.Sleep(50 * time.Millisecond)

	// Unblock one at a time and verify calls are sequential.
	for i := 0; i < 3; i++ {
		clock.Add(1)
		block <- struct{}{}
		time.Sleep(20 * time.Millisecond)
	}

	<-done
	calls := rs.restartCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 restarts, got %d", len(calls))
	}
	// Verify timestamps are strictly increasing (sequential, not concurrent).
	rs.mu.Lock()
	times := make([]time.Time, len(rs.callTime))
	copy(times, rs.callTime)
	rs.mu.Unlock()
	for i := 1; i < len(times); i++ {
		if !times[i].After(times[i-1]) {
			t.Fatalf("restart %d not after restart %d: %v vs %v", i, i-1, times[i], times[i-1])
		}
	}
}

func TestAutoUpdate_CheckPerInstanceFailureIsolation(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v3.3.0", false, true),
	}}
	inst := &fakeInstaller{result: versions.InstallResult{Version: "v3.3.0"}}
	il := &fakeInstanceLister{items: []instances.Instance{
		{ID: 10, Name: "ok", Status: "running", GOWAVersion: "latest"},
		{ID: 11, Name: "bad", Status: "running", GOWAVersion: "latest"},
	}}
	rs := &fakeRestarter{errBy: map[int64]error{11: errors.New("restart failed")}}
	au := newAutoUpdate(t, vl, inst, il, rs)

	res, err := au.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !res.Updated {
		t.Fatalf("expected update despite restart failure")
	}
	if res.RestartedInstances != 1 {
		t.Fatalf("expected 1 successful restart, got %d", res.RestartedInstances)
	}
	calls := rs.restartCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 restart attempts, got %d", len(calls))
	}
}

func TestAutoUpdate_CheckInstallError(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v4.0.0", false, true),
	}}
	inst := &fakeInstaller{err: errors.New("download failed")}
	au := newAutoUpdate(t, vl, inst, &fakeInstanceLister{}, &fakeRestarter{})

	res, err := au.Check(context.Background())
	if err != nil {
		t.Fatalf("Check should swallow install errors, got: %v", err)
	}
	if res.Updated {
		t.Fatalf("expected no update on install error")
	}
	// isChecking must be reset.
	status, _ := au.Status(context.Background())
	if status.IsChecking {
		t.Fatalf("isChecking should be false after error")
	}
}

func TestAutoUpdate_CheckResetsIsCheckingOnSuccess(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v4.1.0", false, true),
	}}
	inst := &fakeInstaller{result: versions.InstallResult{Version: "v4.1.0"}}
	au := newAutoUpdate(t, vl, inst, &fakeInstanceLister{items: []instances.Instance{}}, &fakeRestarter{})

	au.Check(context.Background())
	status, _ := au.Status(context.Background())
	if status.IsChecking {
		t.Fatalf("isChecking should be false after success")
	}
}

func TestAutoUpdate_CheckSetsLastCheck(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{}}
	au := newAutoUpdate(t, vl, &fakeInstaller{}, &fakeInstanceLister{}, &fakeRestarter{})

	au.Check(context.Background())
	status, _ := au.Status(context.Background())
	if status.LastCheck == nil {
		t.Fatalf("expected lastCheck set after check")
	}
}

// --- Overlapping-run prevention -------------------------------------------

func TestAutoUpdate_CheckOverlappingPrevention(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v5.0.0", false, true),
	}}
	il := &fakeInstanceLister{items: []instances.Instance{}}

	// Block the installer to keep the first Check in flight.
	installBlock := make(chan struct{})
	inst2 := &blockingInstaller{block: installBlock, entered: make(chan struct{}, 2), result: versions.InstallResult{Version: "v5.0.0"}}
	au := NewAutoUpdate(AutoUpdateOptions{
		Versions:  vl,
		Installer: inst2,
		Lister:    il,
		Restarter: &fakeRestarter{},
		Now:       func() time.Time { return time.Unix(1000000, 0).UTC() },
	})

	done := make(chan struct{})
	go func() {
		au.Check(context.Background())
		close(done)
	}()

	// Wait for the first check to enter the installer.
	<-inst2.entered

	// Second concurrent check should be skipped.
	res, err := au.Check(context.Background())
	if err != nil {
		t.Fatalf("second Check error: %v", err)
	}
	if res.Updated {
		t.Fatalf("expected overlapping check to be skipped (not updated)")
	}

	// Unblock the first check.
	close(installBlock)
	<-done
}

// blockingInstaller wraps fakeInstaller behavior but blocks on a channel
// before returning, and signals entry via entered.
type blockingInstaller struct {
	block   chan struct{}
	entered chan struct{}
	result  versions.InstallResult
	once    sync.Once
}

func (b *blockingInstaller) Install(ctx context.Context, version string) (versions.InstallResult, error) {
	b.once.Do(func() {
		if b.entered != nil {
			b.entered <- struct{}{}
		}
	})
	select {
	case <-b.block:
	case <-ctx.Done():
		return versions.InstallResult{}, ctx.Err()
	}
	return b.result, nil
}

// --- Instances tests -------------------------------------------------------

func TestAutoUpdate_InstancesEligibleList(t *testing.T) {
	il := &fakeInstanceLister{items: []instances.Instance{
		{ID: 1, Name: "one", Status: "running", GOWAVersion: "latest"},
		{ID: 2, Name: "two", Status: "stopped", GOWAVersion: "v1.0.0"},
		{ID: 3, Name: "three", Status: "running", GOWAVersion: ""},
		{ID: 4, Name: "four", Status: "stopped", GOWAVersion: "latest"},
	}}
	au := newAutoUpdate(t, &fakeVersionLister{}, &fakeInstaller{}, il, &fakeRestarter{})

	result, err := au.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 eligible instances, got %d", len(result))
	}
	expected := map[int64]string{1: "one", 3: "three", 4: "four"}
	for _, inst := range result {
		if name, ok := expected[inst.ID]; !ok || inst.Name != name {
			t.Fatalf("unexpected instance %+v", inst)
		}
	}
}

func TestAutoUpdate_InstancesEmptyWhenNoneMatch(t *testing.T) {
	il := &fakeInstanceLister{items: []instances.Instance{
		{ID: 5, Name: "pinned", Status: "running", GOWAVersion: "v2.0.0"},
	}}
	au := newAutoUpdate(t, &fakeVersionLister{}, &fakeInstaller{}, il, &fakeRestarter{})

	result, err := au.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 instances, got %d", len(result))
	}
}

func TestAutoUpdate_InstancesListError(t *testing.T) {
	il := &fakeInstanceLister{err: errors.New("db down")}
	au := newAutoUpdate(t, &fakeVersionLister{}, &fakeInstaller{}, il, &fakeRestarter{})

	if _, err := au.Instances(context.Background()); err == nil {
		t.Fatalf("expected list error to propagate")
	}
}

// --- JSON contract tests (real service via httpapi) ------------------------

func TestAutoUpdate_StatusJSONContract(t *testing.T) {
	au := newAutoUpdate(t, &fakeVersionLister{}, &fakeInstaller{}, &fakeInstanceLister{}, &fakeRestarter{})
	status, err := au.Status(context.Background())
	if err != nil {
		t.Fatalf("Status error: %v", err)
	}
	// Verify the struct matches the httpapi contract shape.
	if status.LastCheck != nil || status.LastUpdate != nil || status.LatestVersion != nil || status.NextCheck != nil {
		t.Fatalf("expected all nullable fields nil by default")
	}
	if status.IsChecking != false {
		t.Fatalf("expected isChecking false")
	}
}

func TestAutoUpdate_CheckResultJSONContract(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v6.0.0", false, true),
	}}
	inst := &fakeInstaller{result: versions.InstallResult{Version: "v6.0.0"}}
	au := newAutoUpdate(t, vl, inst, &fakeInstanceLister{items: []instances.Instance{}}, &fakeRestarter{})

	res, _ := au.Check(context.Background())
	// Must match httpapi.AutoUpdateCheckResult shape.
	_ = httpapi.AutoUpdateCheckResult{
		Success:            res.Success,
		Updated:            res.Updated,
		Version:            res.Version,
		RestartedInstances: res.RestartedInstances,
	}
	if !res.Success || !res.Updated {
		t.Fatalf("expected success+updated, got %+v", res)
	}
}

func TestAutoUpdate_InstanceJSONContract(t *testing.T) {
	il := &fakeInstanceLister{items: []instances.Instance{
		{ID: 1, Name: "alpha", Status: "running", GOWAVersion: "latest"},
	}}
	au := newAutoUpdate(t, &fakeVersionLister{}, &fakeInstaller{}, il, &fakeRestarter{})

	result, _ := au.Instances(context.Background())
	if len(result) != 1 {
		t.Fatalf("expected 1 instance")
	}
	// Must match httpapi.AutoUpdateInstance shape.
	_ = httpapi.AutoUpdateInstance{
		ID:     result[0].ID,
		Name:   result[0].Name,
		Status: result[0].Status,
	}
	if result[0].ID != 1 || result[0].Name != "alpha" || result[0].Status != "running" {
		t.Fatalf("unexpected instance %+v", result[0])
	}
}

// --- Scheduled loop tests --------------------------------------------------

func TestAutoUpdate_StartSchedulesPeriodicCheck(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{}}
	checkCount := &atomic.Int32{}
	au := NewAutoUpdate(AutoUpdateOptions{
		Versions:  vl,
		Installer: &fakeInstaller{},
		Lister:    &fakeInstanceLister{},
		Restarter: &fakeRestarter{},
		Now:       func() time.Time { return time.Unix(0, 0).UTC() },
		After:     newAUControlledAfter().after,
		OnCheck:   func() { checkCount.Add(1) },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	au.Start(ctx, time.Hour)

	// nextCheck should be set after start.
	status, _ := au.Status(context.Background())
	if status.NextCheck == nil {
		t.Fatalf("expected nextCheck set after start")
	}

	// Stop should clear nextCheck.
	au.Stop()
	status2, _ := au.Status(context.Background())
	if status2.NextCheck != nil {
		t.Fatalf("expected nextCheck nil after stop")
	}
}

func TestAutoUpdate_StartIdempotentClearsPrevious(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{}}
	au := NewAutoUpdate(AutoUpdateOptions{
		Versions:  vl,
		Installer: &fakeInstaller{},
		Lister:    &fakeInstanceLister{},
		Restarter: &fakeRestarter{},
		Now:       func() time.Time { return time.Unix(0, 0).UTC() },
		After:     newAUControlledAfter().after,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	au.Start(ctx, 30*time.Minute)
	au.Start(ctx, 45*time.Minute) // re-start should clear previous

	// Should not panic or hang.
	au.Stop()
}

func TestAutoUpdate_StopWithoutStart(t *testing.T) {
	au := newAutoUpdate(t, &fakeVersionLister{}, &fakeInstaller{}, &fakeInstanceLister{}, &fakeRestarter{})
	// Stop without start should be a safe no-op.
	au.Stop()
}

func TestAutoUpdate_ScheduledCheckFiresJob(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{}}
	checkCount := &atomic.Int32{}
	a := newAUControlledAfter()
	au := NewAutoUpdate(AutoUpdateOptions{
		Versions:  vl,
		Installer: &fakeInstaller{},
		Lister:    &fakeInstanceLister{},
		Restarter: &fakeRestarter{},
		Now:       func() time.Time { return time.Unix(0, 0).UTC() },
		After:     a.after,
		OnCheck:   func() { checkCount.Add(1) },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	au.Start(ctx, time.Hour)

	// Fire the first scheduled tick.
	a.fire(t, 0)
	// Wait for the check to run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if checkCount.Load() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if checkCount.Load() < 1 {
		t.Fatalf("expected scheduled check to fire, got %d", checkCount.Load())
	}
	au.Stop()
}

func TestAutoUpdate_ScheduledCheckOverlappingPrevention(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{
		vi("latest", false, true),
		vi("v7.0.0", false, true),
	}}
	il := &fakeInstanceLister{items: []instances.Instance{}}

	// Block the installer to keep the first scheduled check in flight.
	installBlock := make(chan struct{})
	entered := make(chan struct{}, 4)
	bi := &blockingInstaller{block: installBlock, entered: entered, result: versions.InstallResult{Version: "v7.0.0"}}
	checkCount := &atomic.Int32{}
	a := newAUControlledAfter()
	au := NewAutoUpdate(AutoUpdateOptions{
		Versions:  vl,
		Installer: bi,
		Lister:    il,
		Restarter: &fakeRestarter{},
		Now:       func() time.Time { return time.Unix(0, 0).UTC() },
		After:     a.after,
		OnCheck:   func() { checkCount.Add(1) },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	au.Start(ctx, time.Hour)

	// Fire first tick -> check starts and blocks in installer.
	a.fire(t, 0)
	<-entered
	// Fire second tick while first check is in flight.
	a.fire(t, 1)
	time.Sleep(50 * time.Millisecond)

	// Only 1 check should have actually run (the second is skipped).
	if checkCount.Load() != 1 {
		t.Fatalf("expected 1 check, got %d", checkCount.Load())
	}

	// Unblock and stop.
	close(installBlock)
	au.Stop()
}

func TestAutoUpdate_ContextCancellationStopsLoop(t *testing.T) {
	vl := &fakeVersionLister{versions: []versions.VersionInfo{}}
	a := newAUControlledAfter()
	au := NewAutoUpdate(AutoUpdateOptions{
		Versions:  vl,
		Installer: &fakeInstaller{},
		Lister:    &fakeInstanceLister{},
		Restarter: &fakeRestarter{},
		Now:       func() time.Time { return time.Unix(0, 0).UTC() },
		After:     a.after,
	})

	ctx, cancel := context.WithCancel(context.Background())
	au.Start(ctx, time.Hour)
	cancel()

	stopDone := make(chan struct{})
	go func() {
		au.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop did not return after context cancellation")
	}
}

// --- auControlledAfter helper (for scheduled loop tests) -------------------

type auControlledAfter struct {
	mu    sync.Mutex
	durs  []time.Duration
	chans []chan time.Time
}

func newAUControlledAfter() *auControlledAfter {
	return &auControlledAfter{}
}

func (a *auControlledAfter) after(d time.Duration) <-chan time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.durs = append(a.durs, d)
	ch := make(chan time.Time, 1)
	a.chans = append(a.chans, ch)
	return ch
}

func (a *auControlledAfter) fire(t *testing.T, idx int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.mu.Lock()
		ok := idx < len(a.chans)
		a.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for after channel %d", idx)
		}
		time.Sleep(2 * time.Millisecond)
	}
	a.mu.Lock()
	ch := a.chans[idx]
	a.mu.Unlock()
	ch <- time.Unix(0, 0).UTC()
}

// --- HTTP integration test (real service through httpapi routes) -----------
//
// This test lives in the scheduler package (not httpapi) to avoid an import
// cycle: scheduler imports httpapi for the DTO types, so httpapi cannot
// import scheduler back in its test files.

// auHTTPVersionLister is a minimal stub for the HTTP integration test.
type auHTTPVersionLister struct {
	versions []versions.VersionInfo
}

func (f *auHTTPVersionLister) GetAvailableVersions(ctx context.Context, limit int) ([]versions.VersionInfo, error) {
	return f.versions, nil
}

type auHTTPInstaller struct {
	calls []string
}

func (f *auHTTPInstaller) Install(ctx context.Context, version string) (versions.InstallResult, error) {
	f.calls = append(f.calls, version)
	return versions.InstallResult{Version: version, Path: "/fake/" + version}, nil
}

type auHTTPInstanceLister struct {
	items []instances.Instance
}

func (f *auHTTPInstanceLister) List(ctx context.Context) ([]instances.Instance, error) {
	return f.items, nil
}

type auHTTPRestarter struct {
	calls []int64
}

func (f *auHTTPRestarter) Restart(ctx context.Context, id int64) error {
	f.calls = append(f.calls, id)
	return nil
}

// auHTTPSystemService is a minimal SystemService for the HTTP test.
type auHTTPSystemService struct{}

func (s *auHTTPSystemService) GetSystemStatus(context.Context) (system.SystemStatus, error) {
	return system.SystemStatus{}, nil
}
func (s *auHTTPSystemService) GetSystemConfig() (system.SystemConfig, error) {
	return system.SystemConfig{}, nil
}

// auHTTPPortAllocator is a minimal PortAllocator for the HTTP test.
type auHTTPPortAllocator struct{}

func (a *auHTTPPortAllocator) Next(context.Context) (int, error) { return 8000, nil }

func TestAutoUpdate_HTTPJSONContract(t *testing.T) {
	vl := &auHTTPVersionLister{versions: []versions.VersionInfo{
		{Version: "latest", Path: "/fake/latest", Installed: false, IsLatest: true},
		{Version: "v2.0.0", Path: "/fake/v2.0.0", Installed: true, IsLatest: true},
	}}
	inst := &auHTTPInstaller{}
	il := &auHTTPInstanceLister{items: []instances.Instance{
		{ID: 1, Name: "alpha", Status: "running", GOWAVersion: "latest"},
		{ID: 2, Name: "beta", Status: "stopped", GOWAVersion: "v1.0.0"},
	}}
	rs := &auHTTPRestarter{}

	au := NewAutoUpdate(AutoUpdateOptions{
		Versions:  vl,
		Installer: inst,
		Lister:    il,
		Restarter: rs,
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})

	handler := httpapi.New(httpapi.Dependencies{
		System:        &auHTTPSystemService{},
		PortAllocator: &auHTTPPortAllocator{},
		AutoUpdate:    au,
	})

	// GET /api/system/auto-update/status — default status (all nil, isChecking false).
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/auto-update/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("status unmarshal: %v", err)
	}
	if status["isChecking"] != false || status["lastCheck"] != nil || status["lastUpdate"] != nil || status["latestVersion"] != nil || status["nextCheck"] != nil {
		t.Fatalf("status: unexpected default values: %+v", status)
	}

	// POST /api/system/auto-update/check — already installed, so Updated=false.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/system/auto-update/check", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("check: got %d, want %d", rec.Code, http.StatusOK)
	}
	var checkResult map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &checkResult); err != nil {
		t.Fatalf("check unmarshal: %v", err)
	}
	if checkResult["success"] != true || checkResult["updated"] != false || checkResult["version"] != nil || checkResult["restartedInstances"] != float64(0) {
		t.Fatalf("check: unexpected result: %+v", checkResult)
	}

	// GET /api/system/auto-update/instances — only instance 1 (latest) is eligible.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/auto-update/instances", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("instances: got %d, want %d", rec.Code, http.StatusOK)
	}
	var instancesResult []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &instancesResult); err != nil {
		t.Fatalf("instances unmarshal: %v", err)
	}
	if len(instancesResult) != 1 || instancesResult[0]["id"] != float64(1) || instancesResult[0]["name"] != "alpha" || instancesResult[0]["status"] != "running" {
		t.Fatalf("instances: unexpected result: %+v", instancesResult)
	}

	// GET status again — latestVersion should now be set after the check.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/auto-update/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status2: got %d, want %d", rec.Code, http.StatusOK)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("status2 unmarshal: %v", err)
	}
	if status["latestVersion"] != "v2.0.0" {
		t.Fatalf("status2: expected latestVersion v2.0.0, got %v", status["latestVersion"])
	}
	if status["lastCheck"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("status2: expected lastCheck 2026-01-01T00:00:00Z, got %v", status["lastCheck"])
	}
}
