package instancelogs

import (
	"bytes"
	"sync"
	"time"
)

type LineWriter struct {
	store      *Store
	instanceID int64
	stream     Stream
	now        func() time.Time

	mu      sync.Mutex
	partial bytes.Buffer
}

func NewLineWriter(store *Store, instanceID int64, stream Stream, now func() time.Time) *LineWriter {
	if now == nil {
		now = time.Now
	}
	return &LineWriter{store: store, instanceID: instanceID, stream: stream, now: now}
}

func (w *LineWriter) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	written := len(p)
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			_, _ = w.partial.Write(p)
			return written, nil
		}
		_, _ = w.partial.Write(p[:idx])
		w.appendLocked(w.partial.String())
		w.partial.Reset()
		p = p[idx+1:]
	}
	return written, nil
}

func (w *LineWriter) Flush() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.partial.Len() == 0 {
		return
	}
	w.appendLocked(w.partial.String())
	w.partial.Reset()
}

func (w *LineWriter) appendLocked(line string) {
	if w.store == nil {
		return
	}
	w.store.Append(w.instanceID, w.stream, line, w.now())
}
