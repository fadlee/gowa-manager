package instancelogs

import (
	"strings"
	"sync"
	"time"
)

const DefaultCapacity = 1000

type Stream string

const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

type Entry struct {
	InstanceID int64     `json:"instanceId"`
	Timestamp  time.Time `json:"timestamp"`
	Stream     Stream    `json:"stream"`
	Line       string    `json:"line"`
}

type Store struct {
	capacity int
	mu       sync.Mutex
	buffers  map[int64]*ringBuffer
}

type ringBuffer struct {
	entries []Entry
	start   int
	count   int
}

func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Store{capacity: capacity, buffers: make(map[int64]*ringBuffer)}
}

func (s *Store) Append(instanceID int64, stream Stream, line string, timestamp time.Time) {
	if s == nil || instanceID <= 0 {
		return
	}
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := s.buffers[instanceID]
	if buf == nil {
		buf = &ringBuffer{entries: make([]Entry, s.capacity)}
		s.buffers[instanceID] = buf
	}
	entry := Entry{InstanceID: instanceID, Timestamp: timestamp.UTC(), Stream: stream, Line: line}
	if buf.count < len(buf.entries) {
		idx := (buf.start + buf.count) % len(buf.entries)
		buf.entries[idx] = entry
		buf.count++
		return
	}
	buf.entries[buf.start] = entry
	buf.start = (buf.start + 1) % len(buf.entries)
}

func (s *Store) Tail(instanceID int64, tail int) []Entry {
	if s == nil || instanceID <= 0 || tail <= 0 {
		return []Entry{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := s.buffers[instanceID]
	if buf == nil || buf.count == 0 {
		return []Entry{}
	}
	if tail > buf.count {
		tail = buf.count
	}
	out := make([]Entry, 0, tail)
	first := buf.count - tail
	for i := first; i < buf.count; i++ {
		idx := (buf.start + i) % len(buf.entries)
		out = append(out, buf.entries[idx])
	}
	return out
}

func (s *Store) Clear(instanceID int64) {
	if s == nil || instanceID <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.buffers, instanceID)
	s.mu.Unlock()
}
