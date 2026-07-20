package monitoring

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMonitorSamplesProcessTreeAndHistory(t *testing.T) {
	sampler := &fakeSampler{
		processes: map[int]ProcessSample{
			10: {PID: 10, CPUPercent: 12.5, MemoryMB: 100, MemoryPercent: 10, Children: []int{11, 12}},
			11: {PID: 11, CPUPercent: 2.5, MemoryMB: 50, MemoryPercent: 5},
			12: {PID: 12, CPUPercent: 5, MemoryMB: 25, MemoryPercent: 2},
		},
		diskMB: 321,
	}
	m := New(MonitorOptions{Sampler: sampler, HistoryLimit: 5, DiskCacheTTL: time.Second, Now: func() time.Time { return time.Unix(10, 0) }})

	res, ok := m.Resources(context.Background(), 1, 10, `/tmp/instance-1`)
	if !ok {
		t.Fatal("Resources ok = false, want true")
	}
	if res.CPUPercent != 20 || res.MemoryMB != 175 || res.MemoryPercent != 17 {
		t.Fatalf("resources = %+v, want aggregated cpu 20 memory 175 percent 17", res)
	}
	if res.AvgCPU == nil || *res.AvgCPU != 20 || res.AvgMemory == nil || *res.AvgMemory != 175 {
		t.Fatalf("averages = %+v/%+v, want 20/175", res.AvgCPU, res.AvgMemory)
	}
	if res.DiskMB == nil || *res.DiskMB != 321 {
		t.Fatalf("disk = %+v, want 321", res.DiskMB)
	}
}

func TestMonitorOmitsResourcesForMissingOrExitedPID(t *testing.T) {
	m := New(MonitorOptions{Sampler: &fakeSampler{err: ErrProcessNotFound}, HistoryLimit: 5})
	if _, ok := m.Resources(context.Background(), 1, 99, `x`); ok {
		t.Fatal("Resources ok = true for missing process, want false")
	}
}

func TestMonitorOmitsDiskOnFilesystemErrorAndDoesNotCacheError(t *testing.T) {
	now := time.Unix(10, 0)
	sampler := &fakeSampler{processes: map[int]ProcessSample{10: {PID: 10, CPUPercent: 1, MemoryMB: 2, MemoryPercent: 3}}, diskErr: errors.New("denied")}
	m := New(MonitorOptions{Sampler: sampler, HistoryLimit: 5, DiskCacheTTL: time.Second, Now: func() time.Time { return now }})

	res, ok := m.Resources(context.Background(), 1, 10, `dir`)
	if !ok || res.DiskMB != nil {
		t.Fatalf("Resources = %+v ok %v, want ok with omitted disk", res, ok)
	}
	sampler.diskErr = nil
	sampler.diskMB = 42
	res, ok = m.Resources(context.Background(), 1, 10, `dir`)
	if !ok || res.DiskMB == nil || *res.DiskMB != 42 || sampler.diskCalls != 2 {
		t.Fatalf("retry after error Resources = %+v ok %v diskCalls %d, want disk 42", res, ok, sampler.diskCalls)
	}
}

func TestMonitorCachesSuccessfulZeroDiskUsage(t *testing.T) {
	sampler := &fakeSampler{processes: map[int]ProcessSample{10: {PID: 10, CPUPercent: 1, MemoryMB: 2, MemoryPercent: 3}}, diskMB: 0}
	m := New(MonitorOptions{Sampler: sampler, HistoryLimit: 5, DiskCacheTTL: time.Second})

	res, ok := m.Resources(context.Background(), 1, 10, `dir`)
	if !ok || res.DiskMB == nil || *res.DiskMB != 0 {
		t.Fatalf("Resources = %+v ok %v, want zero disk", res, ok)
	}
	sampler.diskErr = errors.New("transient")
	res, ok = m.Resources(context.Background(), 1, 10, `dir`)
	if !ok || res.DiskMB == nil || *res.DiskMB != 0 || sampler.diskCalls != 1 {
		t.Fatalf("cached zero Resources = %+v ok %v diskCalls %d, want cached zero", res, ok, sampler.diskCalls)
	}
}

func TestMonitorConcurrentRequestsAndClear(t *testing.T) {
	sampler := &fakeSampler{processes: map[int]ProcessSample{10: {PID: 10, CPUPercent: 1, MemoryMB: 2, MemoryPercent: 3}}, diskMB: 4}
	m := New(MonitorOptions{Sampler: sampler, HistoryLimit: 10, DiskCacheTTL: time.Minute})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = m.Resources(context.Background(), 1, 10, `dir`) }()
	}
	wg.Wait()
	if got := len(m.History(1)); got != 10 {
		t.Fatalf("History len = %d, want 10", got)
	}
	m.Clear(1)
	if got := len(m.History(1)); got != 0 {
		t.Fatalf("History len after Clear = %d, want 0", got)
	}
	_, _ = m.Resources(context.Background(), 1, 10, `dir`)
	if sampler.diskCalls < 2 {
		t.Fatalf("disk calls after Clear = %d, want cache cleared and traversed again", sampler.diskCalls)
	}
}

type fakeSampler struct {
	mu        sync.Mutex
	processes map[int]ProcessSample
	err       error
	diskMB    float64
	diskErr   error
	diskCalls int
}

func (f *fakeSampler) Process(_ context.Context, pid int) (ProcessSample, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return ProcessSample{}, f.err
	}
	p, ok := f.processes[pid]
	if !ok {
		return ProcessSample{}, ErrProcessNotFound
	}
	return p, nil
}

func (f *fakeSampler) DiskUsageMB(_ context.Context, _ string) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.diskCalls++
	return f.diskMB, f.diskErr
}
