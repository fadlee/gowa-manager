package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/instances"
)

// --- stubs ---

type fakeLister struct {
	mu    sync.Mutex
	items []instances.Instance
	err   error
	calls int
}

func (f *fakeLister) List(ctx context.Context) ([]instances.Instance, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

// fakeResolver resolves instance ids to subdirectories of a base temp dir.
// ids listed in errMap return an error to simulate per-instance failure.
type fakeResolver struct {
	base   string
	errMap map[int64]error
}

func (f *fakeResolver) InstanceDir(id int64) (string, error) {
	if f.errMap != nil {
		if err, ok := f.errMap[id]; ok {
			return "", err
		}
	}
	return filepath.Join(f.base, strconv.FormatInt(id, 10)), nil
}

// newCleanup builds a Cleanup wired to a temp data dir and the given instances.
func newCleanup(t *testing.T, base string, items []instances.Instance, errMap map[int64]error) *Cleanup {
	t.Helper()
	return NewCleanup(CleanupOptions{
		Lister:   &fakeLister{items: items},
		Resolver: &fakeResolver{base: base, errMap: errMap},
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
}

// setupInstance creates the instance dir and the requested subdirs/files.
func setupInstance(t *testing.T, base string, id int64) string {
	t.Helper()
	dir := filepath.Join(base, strconv.FormatInt(id, 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir instance: %v", err)
	}
	return dir
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

// --- cleanup behavior tests ---

func TestCleanup_StorageJpegRemoval(t *testing.T) {
	base := t.TempDir()
	dir := setupInstance(t, base, 1)
	storage := filepath.Join(dir, "storages")
	writeFile(t, filepath.Join(storage, "a.jpg"))
	writeFile(t, filepath.Join(storage, "b.JPEG"))
	writeFile(t, filepath.Join(storage, "c.JPG"))
	writeFile(t, filepath.Join(storage, "d.jpeg"))

	c := newCleanup(t, base, []instances.Instance{{ID: 1, Name: "i1"}}, nil)
	res, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Deleted != 4 {
		t.Fatalf("deleted = %d, want 4", res.Deleted)
	}
	for _, name := range []string{"a.jpg", "b.JPEG", "c.JPG", "d.jpeg"} {
		if exists(t, filepath.Join(storage, name)) {
			t.Errorf("jpeg %s still present", name)
		}
	}
}

func TestCleanup_MediaAllFilesAndSubdirs(t *testing.T) {
	base := t.TempDir()
	dir := setupInstance(t, base, 2)
	media := filepath.Join(dir, "statics", "media")
	writeFile(t, filepath.Join(media, "file1.png"))
	writeFile(t, filepath.Join(media, "file2.txt"))
	// nested subdir with content - removed recursively, counted as 1.
	writeFile(t, filepath.Join(media, "sub", "nested.bin"))

	c := newCleanup(t, base, []instances.Instance{{ID: 2, Name: "i2"}}, nil)
	res, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 2 files + 1 subdir = 3 deletion units.
	if res.Deleted != 3 {
		t.Fatalf("deleted = %d, want 3", res.Deleted)
	}
	if exists(t, filepath.Join(media, "file1.png")) {
		t.Errorf("file1.png still present")
	}
	if exists(t, filepath.Join(media, "sub")) {
		t.Errorf("sub dir still present")
	}
}

func TestCleanup_MissingDirectories(t *testing.T) {
	base := t.TempDir()
	// Instance dir exists but storages/statics/media do not.
	setupInstance(t, base, 3)

	c := newCleanup(t, base, []instances.Instance{{ID: 3, Name: "i3"}}, nil)
	res, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Deleted != 0 {
		t.Fatalf("deleted = %d, want 0", res.Deleted)
	}
	if res.Errors != 0 {
		t.Fatalf("errors = %d, want 0", res.Errors)
	}

	// Entire instance dir missing: still no error, 0 count.
	c2 := newCleanup(t, base, []instances.Instance{{ID: 999, Name: "missing"}}, nil)
	res2, err := c2.Run(context.Background())
	if err != nil {
		t.Fatalf("Run missing: %v", err)
	}
	if res2.Deleted != 0 || res2.Errors != 0 {
		t.Fatalf("missing dir: deleted=%d errors=%d, want 0/0", res2.Deleted, res2.Errors)
	}
}

func TestCleanup_PreservesUnrelatedFiles(t *testing.T) {
	base := t.TempDir()
	dir := setupInstance(t, base, 4)
	storage := filepath.Join(dir, "storages")
	media := filepath.Join(dir, "statics", "media")
	other := filepath.Join(dir, "statics", "other")

	// Non-jpeg in storages must be preserved.
	writeFile(t, filepath.Join(storage, "keep.png"))
	writeFile(t, filepath.Join(storage, "keep.txt"))
	writeFile(t, filepath.Join(storage, "drop.jpg"))
	// Files outside storages/statics/media must be preserved.
	writeFile(t, filepath.Join(other, "keep_other.log"))
	writeFile(t, filepath.Join(dir, "config.json"))
	// Media is fully wiped.
	writeFile(t, filepath.Join(media, "gone.mp4"))

	c := newCleanup(t, base, []instances.Instance{{ID: 4, Name: "i4"}}, nil)
	res, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 1 jpeg + 1 media file = 2.
	if res.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", res.Deleted)
	}
	for _, p := range []string{
		filepath.Join(storage, "keep.png"),
		filepath.Join(storage, "keep.txt"),
		filepath.Join(other, "keep_other.log"),
		filepath.Join(dir, "config.json"),
	} {
		if !exists(t, p) {
			t.Errorf("preserved file missing: %s", p)
		}
	}
}

func TestCleanup_PerInstanceFailureIsolation(t *testing.T) {
	base := t.TempDir()
	// Instance 5 fails at resolve; instance 6 should still be cleaned.
	dir6 := setupInstance(t, base, 6)
	storage6 := filepath.Join(dir6, "storages")
	writeFile(t, filepath.Join(storage6, "x.jpg"))

	errMap := map[int64]error{5: errors.New("boom")}
	c := newCleanup(t, base, []instances.Instance{
		{ID: 5, Name: "bad"},
		{ID: 6, Name: "good"},
	}, errMap)
	res, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Errors != 1 {
		t.Fatalf("errors = %d, want 1", res.Errors)
	}
	if res.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (instance 6 only)", res.Deleted)
	}
	if exists(t, filepath.Join(storage6, "x.jpg")) {
		t.Errorf("instance 6 jpeg not deleted despite instance 5 failure")
	}
}

func TestCleanup_CountsAccurate(t *testing.T) {
	base := t.TempDir()
	dir := setupInstance(t, base, 7)
	storage := filepath.Join(dir, "storages")
	media := filepath.Join(dir, "statics", "media")
	writeFile(t, filepath.Join(storage, "1.jpg"))
	writeFile(t, filepath.Join(storage, "2.jpg"))
	writeFile(t, filepath.Join(storage, "skip.png"))
	writeFile(t, filepath.Join(media, "a.txt"))
	writeFile(t, filepath.Join(media, "b.txt"))
	writeFile(t, filepath.Join(media, "sub", "c.txt"))

	c := newCleanup(t, base, []instances.Instance{{ID: 7, Name: "i7"}}, nil)
	res, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 2 jpegs + (2 files + 1 subdir) media = 5.
	if res.Deleted != 5 {
		t.Fatalf("deleted = %d, want 5", res.Deleted)
	}
	if res.Errors != 0 {
		t.Fatalf("errors = %d, want 0", res.Errors)
	}
	if res.Duration < 0 {
		t.Fatalf("duration negative: %v", res.Duration)
	}
}

func TestCleanup_Cancellation(t *testing.T) {
	base := t.TempDir()
	// Many instances; cancel mid-run so not all are processed.
	var items []instances.Instance
	for i := int64(0); i < 50; i++ {
		id := int64(100 + i)
		items = append(items, instances.Instance{ID: id, Name: fmt.Sprintf("i%d", id)})
		dir := setupInstance(t, base, id)
		writeFile(t, filepath.Join(dir, "storages", "x.jpg"))
	}

	c := newCleanup(t, base, items, nil)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after starting.
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	res, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Should have processed some but not necessarily all; must not error.
	if res.Errors != 0 {
		t.Fatalf("errors = %d, want 0", res.Errors)
	}
}

func TestCleanup_ListError(t *testing.T) {
	base := t.TempDir()
	c := NewCleanup(CleanupOptions{
		Lister:   &fakeLister{err: errors.New("db down")},
		Resolver: &fakeResolver{base: base},
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if _, err := c.Run(context.Background()); err == nil {
		t.Fatalf("expected list error to propagate")
	}
}

// --- scheduling tests ---

func TestNextRun_MidnightUTC(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{
			name: "23:00 UTC -> 1h",
			now:  time.Date(2024, 1, 1, 23, 0, 0, 0, time.UTC),
			want: 1 * time.Hour,
		},
		{
			name: "00:00 UTC -> 24h",
			now:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: 24 * time.Hour,
		},
		{
			name: "12:30:45 UTC -> 11h29m15s",
			now:  time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC),
			want: 11*time.Hour + 29*time.Minute + 15*time.Second,
		},
		{
			// 23:00 in a UTC+2 zone is 21:00 UTC; next UTC midnight is 3h away.
			name: "non-UTC tz normalized to UTC",
			now:  time.Date(2024, 1, 1, 23, 0, 0, 0, time.FixedZone("UTC+2", 2*3600)),
			want: 3 * time.Hour,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextRun(tc.now)
			if got != tc.want {
				t.Fatalf("NextRun = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- runner tests ---

// controlledAfter returns a channel the test pumps manually and records the
// duration requested from the schedule.
type controlledAfter struct {
	mu    sync.Mutex
	durs  []time.Duration
	chans []chan time.Time
}

func (a *controlledAfter) after(d time.Duration) <-chan time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.durs = append(a.durs, d)
	ch := make(chan time.Time, 1)
	a.chans = append(a.chans, ch)
	return ch
}

func (a *controlledAfter) fire(t *testing.T, idx int) {
	t.Helper()
	// Wait for the loop to request the channel before firing.
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

// jobController coordinates a blocking job across multiple invocations
// without reassigning any shared variable (which would race with the job
// goroutine).
type jobController struct {
	mu      sync.Mutex
	calls   int
	block   chan struct{}
	entered chan struct{}
}

func (c *jobController) job(ctx context.Context) error {
	c.mu.Lock()
	c.calls++
	b := c.block
	c.mu.Unlock()
	c.entered <- struct{}{}
	<-b
	return nil
}

func (c *jobController) setBlock(b chan struct{}) {
	c.mu.Lock()
	c.block = b
	c.mu.Unlock()
}

func (c *jobController) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestRunner_OneAtATime(t *testing.T) {
	ctrl := &jobController{
		block:   make(chan struct{}),
		entered: make(chan struct{}, 8),
	}

	a := &controlledAfter{}
	r := NewRunner(RunnerOptions{
		Job:      ctrl.job,
		Schedule: func(time.Time) time.Duration { return time.Hour },
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
		After:    a.after,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	// First tick fires the job.
	a.fire(t, 0)
	<-ctrl.entered
	// While job is still running, a second tick must be skipped.
	a.fire(t, 1)
	// Give the loop a moment to observe the second tick and skip it.
	time.Sleep(20 * time.Millisecond)

	if got := ctrl.count(); got != 1 {
		t.Fatalf("calls = %d after overlapping tick, want 1", got)
	}

	// Unblock the job; allow the goroutine to release the run lock.
	close(ctrl.block)
	time.Sleep(20 * time.Millisecond)

	// Reset block for a second run.
	ctrl.setBlock(make(chan struct{}))
	a.fire(t, 2)
	select {
	case <-ctrl.entered:
	case <-time.After(time.Second):
		t.Fatalf("second job not started after unblock")
	}
	close(ctrl.block)
	r.Stop()
}

func TestRunner_StartStopIdempotent(t *testing.T) {
	a := &controlledAfter{}
	done := make(chan struct{}, 4)
	r := NewRunner(RunnerOptions{
		Job:      func(context.Context) error { done <- struct{}{}; return nil },
		Schedule: func(time.Time) time.Duration { return time.Hour },
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
		After:    a.after,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !r.Start(ctx) {
		t.Fatalf("first Start should succeed")
	}
	if r.Start(ctx) {
		t.Fatalf("second Start should be rejected")
	}
	if r.Stop() != true {
		t.Fatalf("first Stop should succeed")
	}
	if r.Stop() {
		t.Fatalf("second Stop should be rejected")
	}
}

func TestRunner_StopWithoutStart(t *testing.T) {
	r := NewRunner(RunnerOptions{
		Job:      func(context.Context) error { return nil },
		Schedule: func(time.Time) time.Duration { return time.Hour },
	})
	if r.Stop() {
		t.Fatalf("Stop without Start should be no-op")
	}
}

func TestRunner_ScheduleUsed(t *testing.T) {
	a := &controlledAfter{}
	called := false
	sched := func(now time.Time) time.Duration {
		called = true
		if !now.Equal(time.Unix(0, 0).UTC()) {
			t.Errorf("schedule got unexpected now: %v", now)
		}
		return 30 * time.Second
	}
	r := NewRunner(RunnerOptions{
		Job:      func(context.Context) error { return nil },
		Schedule: sched,
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
		After:    a.after,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	// The loop should have asked `after` for the scheduled duration.
	time.Sleep(20 * time.Millisecond)
	r.Stop()
	if !called {
		t.Fatalf("schedule was not invoked")
	}
	a.mu.Lock()
	if len(a.durs) == 0 || a.durs[0] != 30*time.Second {
		t.Fatalf("after got %v, want 30s", a.durs)
	}
	a.mu.Unlock()
}

func TestRunner_ContextCancellation(t *testing.T) {
	a := &controlledAfter{}
	r := NewRunner(RunnerOptions{
		Job:      func(context.Context) error { return nil },
		Schedule: func(time.Time) time.Duration { return time.Hour },
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
		After:    a.after,
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	cancel()
	// After cancellation the loop should exit; Stop returns promptly.
	stopDone := make(chan struct{})
	go func() {
		r.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop did not return after context cancellation")
	}
}
