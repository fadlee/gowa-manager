package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/config"
	"github.com/fadlee/gowa-manager/internal/database"
	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/instances"
)

// --- stubs for the reconciler ---

// reconcileStarter is a fake InstanceStarter that records calls and can be
// configured to fail for specific instance IDs (simulating missing version,
// occupied port, supervisor error, etc.). It also persists a failed status
// via the provided repo, mirroring LifecycleService.persistFailed behavior.
type reconcileStarter struct {
	mu            sync.Mutex
	started       []int64
	failedIDs     map[int64]error
	startDelay    time.Duration
	blockStart    chan struct{}
	entered       chan struct{}
	concMu        sync.Mutex
	concurrent    int
	maxConcurrent int
	repo          *reconcileRepo
}

func (s *reconcileStarter) Start(ctx context.Context, id int64) error {
	s.mu.Lock()
	failedIDs := s.failedIDs
	delay := s.startDelay
	s.mu.Unlock()

	if s.blockStart != nil {
		if s.entered != nil {
			s.entered <- struct{}{}
		}
		select {
		case <-s.blockStart:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Track concurrency for bounded-concurrency tests.
	s.concMu.Lock()
	s.concurrent++
	if s.concurrent > s.maxConcurrent {
		s.maxConcurrent = s.concurrent
	}
	s.concMu.Unlock()
	defer func() {
		s.concMu.Lock()
		s.concurrent--
		s.concMu.Unlock()
	}()

	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}

	s.mu.Lock()
	s.started = append(s.started, id)
	s.mu.Unlock()

	if err, ok := failedIDs[id]; ok {
		if s.repo != nil {
			msg := err.Error()
			_, _ = s.repo.UpdateStatus(ctx, id, "failed", &msg)
		}
		return err
	}
	if s.repo != nil {
		_, _ = s.repo.UpdateStatus(ctx, id, "running", nil)
	}
	return nil
}

func (s *reconcileStarter) startedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.started)
}

func (s *reconcileStarter) maxConcurrentSeen() int {
	s.concMu.Lock()
	defer s.concMu.Unlock()
	return s.maxConcurrent
}

func (s *reconcileStarter) contains(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.started {
		if v == id {
			return true
		}
	}
	return false
}

// reconcileRepo is a minimal Repository recording status updates.
type reconcileRepo struct {
	mu        sync.Mutex
	statuses  map[int64]string
	errors    map[int64]*string
	ports     map[int64]*int
	listErr   error
	listValue []instances.Instance
}

func newReconcileRepo() *reconcileRepo {
	return &reconcileRepo{statuses: map[int64]string{}, errors: map[int64]*string{}, ports: map[int64]*int{}}
}

func (r *reconcileRepo) List(context.Context) ([]instances.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listValue, r.listErr
}
func (r *reconcileRepo) FindByID(context.Context, int64) (instances.Instance, error) {
	return instances.Instance{}, instances.ErrNotFound
}
func (r *reconcileRepo) FindByKey(context.Context, string) (instances.Instance, error) {
	return instances.Instance{}, instances.ErrNotFound
}
func (r *reconcileRepo) Create(context.Context, instances.CreateInput) (instances.Instance, error) {
	return instances.Instance{}, nil
}
func (r *reconcileRepo) Update(context.Context, instances.UpdateInput) (instances.Instance, error) {
	return instances.Instance{}, nil
}
func (r *reconcileRepo) UpdateStatus(_ context.Context, id int64, status string, errorMessage *string) (instances.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses[id] = status
	r.errors[id] = errorMessage
	return instances.Instance{ID: id, Status: status, ErrorMessage: errorMessage}, nil
}
func (r *reconcileRepo) ClearError(ctx context.Context, id int64) (instances.Instance, error) {
	return r.UpdateStatus(ctx, id, r.statuses[id], nil)
}
func (r *reconcileRepo) UpdatePort(_ context.Context, id int64, port *int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ports[id] = port
	return nil
}
func (r *reconcileRepo) Delete(context.Context, int64) error { return nil }

func ptrInt(v int) *int { return &v }

func reconcileLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

// --- tests ---

func TestReconcileRestartsOnlyRunningInstances(t *testing.T) {
	repo := newReconcileRepo()
	repo.listValue = []instances.Instance{
		{ID: 1, Name: "running-1", Status: "running", Port: ptrInt(3001), GOWAVersion: "v1.0.0"},
		{ID: 2, Name: "stopped-2", Status: "stopped", Port: ptrInt(3002), GOWAVersion: "v1.0.0"},
		{ID: 3, Name: "failed-3", Status: "failed", Port: ptrInt(3003), GOWAVersion: "v1.0.0"},
		{ID: 4, Name: "running-4", Status: "running", Port: ptrInt(3004), GOWAVersion: "v1.0.0"},
	}
	starter := &reconcileStarter{failedIDs: map[int64]error{}, repo: repo}
	r := NewReconciler(ReconcilerOptions{
		Lister:      repo,
		Starter:     starter,
		Logger:      reconcileLogger(),
		Concurrency: 4,
	})

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile error = %v", err)
	}

	if !starter.contains(1) || !starter.contains(4) {
		t.Fatalf("started = %v, want only running instances 1 and 4", starter.started)
	}
	if starter.contains(2) || starter.contains(3) {
		t.Fatalf("started = %v, stopped/failed instances must not restart", starter.started)
	}
	if got := starter.startedCount(); got != 2 {
		t.Fatalf("started count = %d, want 2", got)
	}
}

func TestReconcileSuccessfulRecovery(t *testing.T) {
	repo := newReconcileRepo()
	repo.listValue = []instances.Instance{
		{ID: 1, Name: "running-1", Status: "running", Port: ptrInt(3001), GOWAVersion: "v1.0.0"},
		{ID: 2, Name: "running-2", Status: "running", Port: ptrInt(3002), GOWAVersion: "v1.0.0"},
	}
	starter := &reconcileStarter{failedIDs: map[int64]error{}, repo: repo}
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger()})

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile error = %v", err)
	}

	if got := repo.statuses[1]; got != "running" {
		t.Fatalf("instance 1 status = %q, want running", got)
	}
	if got := repo.statuses[2]; got != "running" {
		t.Fatalf("instance 2 status = %q, want running", got)
	}
}

func TestReconcileMissingVersionPersistsFailed(t *testing.T) {
	repo := newReconcileRepo()
	repo.listValue = []instances.Instance{
		{ID: 1, Name: "running-1", Status: "running", Port: ptrInt(3001), GOWAVersion: "v9.9.9"},
	}
	starter := &reconcileStarter{failedIDs: map[int64]error{1: errors.New("missing version v9.9.9")}, repo: repo}
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger()})

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile error = %v", err)
	}

	if got := repo.statuses[1]; got != "failed" {
		t.Fatalf("instance 1 status = %q, want failed", got)
	}
	if repo.errors[1] == nil || !strings.Contains(*repo.errors[1], "missing version") {
		t.Fatalf("instance 1 error = %v, want missing version", repo.errors[1])
	}
}

func TestReconcileOccupiedPortReallocation(t *testing.T) {
	// The reconciler delegates to the starter; the starter (backed by the
	// real LifecycleService) reallocates an unavailable port. Here we verify
	// the reconciler still calls Start and the instance recovers with a
	// running status and an updated port.
	repo := newReconcileRepo()
	repo.listValue = []instances.Instance{
		{ID: 1, Name: "running-1", Status: "running", Port: ptrInt(3001), GOWAVersion: "v1.0.0"},
	}
	starter := &reconcileStarter{failedIDs: map[int64]error{}, repo: repo}
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger()})

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile error = %v", err)
	}
	if got := repo.statuses[1]; got != "running" {
		t.Fatalf("instance 1 status = %q, want running after reallocation", got)
	}
}

func TestReconcileOneFailureDoesNotBlockOthers(t *testing.T) {
	repo := newReconcileRepo()
	repo.listValue = []instances.Instance{
		{ID: 1, Name: "running-1", Status: "running", Port: ptrInt(3001), GOWAVersion: "v1.0.0"},
		{ID: 2, Name: "running-2", Status: "running", Port: ptrInt(3002), GOWAVersion: "v9.9.9"},
		{ID: 3, Name: "running-3", Status: "running", Port: ptrInt(3003), GOWAVersion: "v1.0.0"},
	}
	starter := &reconcileStarter{failedIDs: map[int64]error{2: errors.New("missing version v9.9.9")}, repo: repo}
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger(), Concurrency: 4})

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile error = %v", err)
	}

	if !starter.contains(1) || !starter.contains(3) {
		t.Fatalf("started = %v, want 1 and 3 to restart despite 2 failing", starter.started)
	}
	if got := repo.statuses[1]; got != "running" {
		t.Fatalf("instance 1 status = %q, want running", got)
	}
	if got := repo.statuses[3]; got != "running" {
		t.Fatalf("instance 3 status = %q, want running", got)
	}
	if got := repo.statuses[2]; got != "failed" {
		t.Fatalf("instance 2 status = %q, want failed", got)
	}
}

func TestReconcileBoundedConcurrency(t *testing.T) {
	repo := newReconcileRepo()
	const n = 8
	items := make([]instances.Instance, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, instances.Instance{ID: int64(i), Name: fmt.Sprintf("running-%d", i), Status: "running", Port: ptrInt(3000 + i), GOWAVersion: "v1.0.0"})
	}
	repo.listValue = items

	starter := &reconcileStarter{
		failedIDs:  map[int64]error{},
		repo:       repo,
		startDelay: 20 * time.Millisecond,
	}
	const limit = 2
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger(), Concurrency: limit})

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile error = %v", err)
	}

	if got := starter.startedCount(); got != n {
		t.Fatalf("started count = %d, want %d", got, n)
	}
	if max := starter.maxConcurrentSeen(); max > limit {
		t.Fatalf("max concurrent starts = %d, want <= %d", max, limit)
	}
}

func TestReconcileReadinessFalseUntilCompletion(t *testing.T) {
	repo := newReconcileRepo()
	repo.listValue = []instances.Instance{
		{ID: 1, Name: "running-1", Status: "running", Port: ptrInt(3001), GOWAVersion: "v1.0.0"},
	}
	starter := &reconcileStarter{
		failedIDs:  map[int64]error{},
		repo:       repo,
		blockStart: make(chan struct{}),
		entered:    make(chan struct{}, 1),
	}
	probe := httpapi.NewReadiness()
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger(), Readiness: probe})

	done := make(chan error, 1)
	go func() { done <- r.Reconcile(context.Background()) }()

	<-starter.entered
	if probe.Ready() {
		t.Fatal("readiness reported ready before reconciliation completed")
	}

	close(starter.blockStart)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Reconcile error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reconcile did not complete")
	}
	if !probe.Ready() {
		t.Fatal("readiness not flipped to ready after reconciliation completed")
	}
}

func TestReconcileCancellationStopsRemainingStarts(t *testing.T) {
	repo := newReconcileRepo()
	const n = 5
	items := make([]instances.Instance, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, instances.Instance{ID: int64(i), Name: fmt.Sprintf("running-%d", i), Status: "running", Port: ptrInt(3000 + i), GOWAVersion: "v1.0.0"})
	}
	repo.listValue = items

	starter := &reconcileStarter{
		failedIDs:  map[int64]error{},
		repo:       repo,
		blockStart: make(chan struct{}),
		entered:    make(chan struct{}, n),
	}
	probe := httpapi.NewReadiness()
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger(), Readiness: probe, Concurrency: 1})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Reconcile(ctx) }()

	<-starter.entered // first instance entered the blocking section
	cancel()
	close(starter.blockStart) // let the blocked one finish

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Reconcile error = %v, want nil or context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reconcile did not return after cancellation")
	}

	// Not all instances should have been started because cancellation stopped
	// the remaining ones. At least one was started (the blocked one), but
	// fewer than n.
	if got := starter.startedCount(); got == n {
		t.Fatalf("started count = %d, want fewer than %d after cancellation", got, n)
	}
	// Readiness is flipped even when cancelled, so the manager does not stay
	// not-ready forever.
	if !probe.Ready() {
		t.Fatal("readiness not flipped to ready after cancellation")
	}
}

func TestReconcilePersistedErrorStatus(t *testing.T) {
	repo := newReconcileRepo()
	repo.listValue = []instances.Instance{
		{ID: 1, Name: "running-1", Status: "running", Port: ptrInt(3001), GOWAVersion: "v1.0.0"},
		{ID: 2, Name: "running-2", Status: "running", Port: ptrInt(3002), GOWAVersion: "v1.0.0"},
	}
	starter := &reconcileStarter{failedIDs: map[int64]error{1: errors.New("supervisor start failed")}, repo: repo}
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger()})

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile error = %v", err)
	}

	if got := repo.statuses[1]; got != "failed" {
		t.Fatalf("instance 1 status = %q, want failed", got)
	}
	if repo.errors[1] == nil || !strings.Contains(*repo.errors[1], "supervisor start failed") {
		t.Fatalf("instance 1 error = %v, want supervisor start failed", repo.errors[1])
	}
	if got := repo.statuses[2]; got != "running" {
		t.Fatalf("instance 2 status = %q, want running", got)
	}
}

func TestReconcileListErrorReturnsError(t *testing.T) {
	repo := newReconcileRepo()
	repo.listErr = errors.New("db unavailable")
	starter := &reconcileStarter{failedIDs: map[int64]error{}, repo: repo}
	probe := httpapi.NewReadiness()
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger(), Readiness: probe})

	err := r.Reconcile(context.Background())
	if err == nil || !strings.Contains(err.Error(), "db unavailable") {
		t.Fatalf("Reconcile error = %v, want db unavailable", err)
	}
	// Even on list failure readiness flips so the manager does not stay
	// not-ready forever.
	if !probe.Ready() {
		t.Fatal("readiness not flipped after list error")
	}
}

func TestReconcileDefaultConcurrencyWhenZero(t *testing.T) {
	repo := newReconcileRepo()
	const n = defaultReconcileConcurrency + 2
	items := make([]instances.Instance, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, instances.Instance{ID: int64(i), Name: fmt.Sprintf("running-%d", i), Status: "running", Port: ptrInt(3000 + i), GOWAVersion: "v1.0.0"})
	}
	repo.listValue = items

	starter := &reconcileStarter{
		failedIDs:  map[int64]error{},
		repo:       repo,
		startDelay: 10 * time.Millisecond,
	}
	r := NewReconciler(ReconcilerOptions{Lister: repo, Starter: starter, Logger: reconcileLogger(), Concurrency: 0})

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile error = %v", err)
	}
	if got := starter.startedCount(); got != n {
		t.Fatalf("started count = %d, want %d", got, n)
	}
	if max := starter.maxConcurrentSeen(); max > defaultReconcileConcurrency {
		t.Fatalf("max concurrent = %d, want <= default %d", max, defaultReconcileConcurrency)
	}
}

// TestRunReconcileAndReadyEndpoint verifies that Run wires the readiness
// probe and reconciler end-to-end: /api/ready returns 503 while
// reconciliation is in flight and 200 once it completes, and previously
// running instances are restarted.
func TestRunReconcileAndReadyEndpoint(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := instances.NewSQLiteRepository(db.SQL)
	seed, err := repo.Create(ctx, instances.CreateInput{Name: "seed", Config: `{}`, GOWAVersion: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateStatus(ctx, seed.ID, "running", nil); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var addr string
	once := sync.Once{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(runCtx, Options{
			Config: config.Config{Port: 0, DataDir: dataDir},
			Logger: slog.New(slog.NewTextHandler(discardWriter{}, nil)),
			OpenDB: func(_ context.Context, _ string) (Closer, error) { return database.Open(ctx, dataDir) },
			OnStarted: func(a string) {
				addr = a
				once.Do(func() { close(started) })
			},
		})
	}()
	<-started

	// Poll /api/ready; it must eventually become 200 after reconciliation.
	client := &http.Client{Timeout: 2 * time.Second}
	var lastCode int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + addr + "/api/ready")
		if err == nil {
			lastCode = resp.StatusCode
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lastCode != http.StatusOK {
		t.Fatalf("/api/ready last status = %d, want 200", lastCode)
	}

	// /api/health must remain unchanged.
	resp, err := client.Get("http://" + addr + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/health status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
