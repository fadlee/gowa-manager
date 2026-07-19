package monitoring

import "sync"

type History struct {
	mu     sync.RWMutex
	limit  int
	points map[int64][]Sample
}

type Average struct {
	AvgCPU    float64
	AvgMemory float64
}

func NewHistory(limit int) *History {
	if limit <= 0 {
		limit = 60
	}
	return &History{limit: limit, points: map[int64][]Sample{}}
}

func (h *History) Record(instanceID int64, sample Sample) {
	h.mu.Lock()
	defer h.mu.Unlock()
	points := append(h.points[instanceID], sample)
	if len(points) > h.limit {
		points = points[len(points)-h.limit:]
	}
	h.points[instanceID] = points
}

func (h *History) Points(instanceID int64) []Sample {
	h.mu.RLock()
	defer h.mu.RUnlock()
	points := h.points[instanceID]
	return append([]Sample(nil), points...)
}

func (h *History) Average(instanceID int64) Average {
	h.mu.RLock()
	defer h.mu.RUnlock()
	points := h.points[instanceID]
	if len(points) == 0 {
		return Average{}
	}
	var cpu, memory float64
	for _, point := range points {
		cpu += point.CPUPercent
		memory += point.MemoryMB
	}
	count := float64(len(points))
	return Average{AvgCPU: cpu / count, AvgMemory: memory / count}
}

func (h *History) Clear(instanceID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.points, instanceID)
}
