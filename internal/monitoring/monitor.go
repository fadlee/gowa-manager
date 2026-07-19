package monitoring

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

var ErrProcessNotFound = errors.New("process not found")

type Sample struct {
	CPUPercent    float64
	MemoryMB      float64
	MemoryPercent float64
	At            time.Time
}

type ProcessSample struct {
	PID           int
	CPUPercent    float64
	MemoryMB      float64
	MemoryPercent float64
	Children      []int
}

type Resources struct {
	CPUPercent    float64  `json:"cpuPercent"`
	MemoryMB      float64  `json:"memoryMB"`
	MemoryPercent float64  `json:"memoryPercent"`
	AvgCPU        *float64 `json:"avgCpu,omitempty"`
	AvgMemory     *float64 `json:"avgMemory,omitempty"`
	DiskMB        *float64 `json:"diskMB,omitempty"`
}

type Sampler interface {
	Process(context.Context, int) (ProcessSample, error)
	DiskUsageMB(context.Context, string) (float64, error)
}

type MonitorOptions struct {
	Sampler      Sampler
	HistoryLimit int
	DiskCacheTTL time.Duration
	Now          func() time.Time
}

type Monitor struct {
	sampler Sampler
	history *History
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	disks map[int64]diskEntry
}

type diskEntry struct {
	value *float64
	at    time.Time
}

func New(opts MonitorOptions) *Monitor {
	sampler := opts.Sampler
	if sampler == nil {
		sampler = GopsutilSampler{}
	}
	ttl := opts.DiskCacheTTL
	if ttl <= 0 {
		// Disk usage walks the instance data directory; keep the cache short so
		// reset/delete changes are reflected quickly without traversing every poll.
		ttl = 5 * time.Second
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Monitor{sampler: sampler, history: NewHistory(opts.HistoryLimit), ttl: ttl, now: now, disks: map[int64]diskEntry{}}
}

func (m *Monitor) Resources(ctx context.Context, instanceID int64, pid int, dataDir string) (Resources, bool) {
	if pid <= 0 {
		return Resources{}, false
	}
	agg, err := m.processTree(ctx, pid, map[int]bool{})
	if err != nil {
		return Resources{}, false
	}
	sample := Sample{CPUPercent: agg.CPUPercent, MemoryMB: agg.MemoryMB, MemoryPercent: agg.MemoryPercent, At: m.now()}
	m.history.Record(instanceID, sample)
	avg := m.history.Average(instanceID)
	res := Resources{CPUPercent: sample.CPUPercent, MemoryMB: sample.MemoryMB, MemoryPercent: sample.MemoryPercent, AvgCPU: &avg.AvgCPU, AvgMemory: &avg.AvgMemory}
	res.DiskMB = m.disk(ctx, instanceID, dataDir)
	return res, true
}

func (m *Monitor) History(instanceID int64) []Sample { return m.history.Points(instanceID) }

func (m *Monitor) Clear(instanceID int64) {
	m.history.Clear(instanceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.disks, instanceID)
}

func (m *Monitor) processTree(ctx context.Context, pid int, seen map[int]bool) (ProcessSample, error) {
	if seen[pid] {
		return ProcessSample{}, nil
	}
	seen[pid] = true
	root, err := m.sampler.Process(ctx, pid)
	if err != nil {
		return ProcessSample{}, err
	}
	for _, childPID := range root.Children {
		child, err := m.processTree(ctx, childPID, seen)
		if err != nil {
			if errors.Is(err, ErrProcessNotFound) {
				continue
			}
			return ProcessSample{}, err
		}
		root.CPUPercent += child.CPUPercent
		root.MemoryMB += child.MemoryMB
		root.MemoryPercent += child.MemoryPercent
	}
	return root, nil
}

func (m *Monitor) disk(ctx context.Context, instanceID int64, dataDir string) *float64 {
	if dataDir == "" {
		return nil
	}
	now := m.now()
	m.mu.Lock()
	if entry, ok := m.disks[instanceID]; ok && now.Sub(entry.at) < m.ttl {
		m.mu.Unlock()
		return entry.value
	}
	m.mu.Unlock()
	value, err := m.sampler.DiskUsageMB(ctx, dataDir)
	var ptr *float64
	if err == nil {
		ptr = &value
	}
	m.mu.Lock()
	m.disks[instanceID] = diskEntry{value: ptr, at: now}
	m.mu.Unlock()
	return ptr
}

type GopsutilSampler struct{}

func (GopsutilSampler) Process(ctx context.Context, pid int) (ProcessSample, error) {
	p, err := process.NewProcessWithContext(ctx, int32(pid))
	if err != nil {
		return ProcessSample{}, ErrProcessNotFound
	}
	cpu, err := p.CPUPercentWithContext(ctx)
	if err != nil {
		return ProcessSample{}, ErrProcessNotFound
	}
	memInfo, err := p.MemoryInfoWithContext(ctx)
	if err != nil {
		return ProcessSample{}, ErrProcessNotFound
	}
	memPercent, err := p.MemoryPercentWithContext(ctx)
	if err != nil {
		return ProcessSample{}, ErrProcessNotFound
	}
	children, err := p.ChildrenWithContext(ctx)
	if err != nil {
		children = nil
	}
	childPIDs := make([]int, 0, len(children))
	for _, child := range children {
		childPIDs = append(childPIDs, int(child.Pid))
	}
	return ProcessSample{PID: pid, CPUPercent: cpu, MemoryMB: float64(memInfo.RSS) / 1024 / 1024, MemoryPercent: float64(memPercent), Children: childPIDs}, nil
}

func (GopsutilSampler) DiskUsageMB(ctx context.Context, dir string) (float64, error) {
	var total uint64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += uint64(info.Size())
		return nil
	})
	if err != nil {
		return 0, err
	}
	return float64(total) / 1024 / 1024, nil
}
