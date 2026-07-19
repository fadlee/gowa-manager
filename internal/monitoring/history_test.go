package monitoring

import (
	"sync"
	"testing"
	"time"
)

func TestHistoryRecordsRollingAveragesAndBounds(t *testing.T) {
	h := NewHistory(3)

	h.Record(1, Sample{CPUPercent: 10, MemoryMB: 100, MemoryPercent: 20, At: time.Unix(1, 0)})
	h.Record(1, Sample{CPUPercent: 20, MemoryMB: 200, MemoryPercent: 30, At: time.Unix(2, 0)})
	h.Record(1, Sample{CPUPercent: 30, MemoryMB: 300, MemoryPercent: 40, At: time.Unix(3, 0)})
	h.Record(1, Sample{CPUPercent: 40, MemoryMB: 400, MemoryPercent: 50, At: time.Unix(4, 0)})

	points := h.Points(1)
	if len(points) != 3 {
		t.Fatalf("Points len = %d, want 3", len(points))
	}
	if points[0].CPUPercent != 20 || points[2].CPUPercent != 40 {
		t.Fatalf("Points = %#v, want oldest point trimmed", points)
	}
	avg := h.Average(1)
	if avg.AvgCPU != 30 || avg.AvgMemory != 300 {
		t.Fatalf("Average = %+v, want cpu 30 memory 300", avg)
	}
}

func TestHistoryConcurrentRequestsAndClear(t *testing.T) {
	h := NewHistory(50)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h.Record(7, Sample{CPUPercent: float64(i), MemoryMB: float64(i)})
			_ = h.Points(7)
			_ = h.Average(7)
		}(i)
	}
	wg.Wait()

	if got := len(h.Points(7)); got != 50 {
		t.Fatalf("Points len = %d, want bounded 50", got)
	}
	h.Clear(7)
	if got := len(h.Points(7)); got != 0 {
		t.Fatalf("Points after Clear = %d, want 0", got)
	}
}
