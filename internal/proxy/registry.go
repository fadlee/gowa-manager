// Package proxy — WebSocket connection registry. registry.go provides
// a thread-safe map of active upstream WebSocket connections keyed by a
// unique per-client connection ID (NOT per instance). Multiple clients
// may connect to the same instance; each owns its own upstream
// connection and registry entry.
//
// The registry supports:
//   - Add / Get / Delete / Close by connection ID;
//   - CloseAll (used during manager shutdown);
//   - CloseAllForInstance (used when an instance is stopped or
//     restarted, to tear down every client connected to it);
//   - Count for observability and leak detection.
//
// WSConnection wraps an upstream *websocket.Conn and closes it
// idempotently via sync.Once so concurrent callers (the copy loops, the
// registry, and shutdown) cannot double-close.
package proxy

import (
	"sync"

	"github.com/coder/websocket"
)

// WSConnection wraps a single upstream WebSocket connection together
// with the metadata needed to manage it: a unique per-client id and the
// instance key it is connected to. All close paths are idempotent.
type WSConnection struct {
	conn *websocket.Conn
	id   string
	key  string

	closeOnce sync.Once
}

// Close performs the WebSocket close handshake with the given status
// code and reason. It is idempotent: subsequent calls are no-ops.
func (c *WSConnection) Close(code websocket.StatusCode, reason string) error {
	var err error
	c.closeOnce.Do(func() {
		if c.conn != nil {
			err = c.conn.Close(code, reason)
		}
	})
	return err
}

// CloseNow closes the underlying connection immediately without a close
// handshake. It is idempotent and is used for abnormal disconnects and
// forced shutdown.
func (c *WSConnection) CloseNow() {
	c.closeOnce.Do(func() {
		if c.conn != nil {
			_ = c.conn.CloseNow()
		}
	})
}

// Registry is a thread-safe registry of active upstream WebSocket
// connections keyed by unique connection ID. The zero value is NOT
// ready for use; construct one with NewRegistry.
type Registry struct {
	mu    sync.Mutex
	conns map[string]*WSConnection
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{conns: make(map[string]*WSConnection)}
}

// Add registers a connection under its id. If a connection with the
// same id already exists it is replaced (the caller is responsible for
// closing the displaced connection if needed).
func (r *Registry) Add(id string, conn *WSConnection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conns == nil {
		r.conns = make(map[string]*WSConnection)
	}
	r.conns[id] = conn
}

// Get returns the connection registered under id, or (nil, false) when
// no such connection exists.
func (r *Registry) Get(id string) (*WSConnection, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.conns[id]
	return c, ok
}

// Delete removes the connection under id from the registry WITHOUT
// closing it. It is a no-op when the id is not present. The caller owns
// closing the returned connection if it still needs to be torn down.
func (r *Registry) Delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.conns, id)
}

// Close closes the connection registered under id (performing a normal
// close handshake) and removes it from the registry. It is a no-op when
// the id is not present.
func (r *Registry) Close(id string) {
	r.mu.Lock()
	c, ok := r.conns[id]
	if ok {
		delete(r.conns, id)
	}
	r.mu.Unlock()
	if ok {
		c.Close(websocket.StatusNormalClosure, "")
	}
}

// CloseAll closes every registered connection and clears the registry.
// It is used during manager shutdown. It is safe to call on an empty
// registry.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	conns := r.conns
	r.conns = make(map[string]*WSConnection)
	r.mu.Unlock()
	for _, c := range conns {
		c.CloseNow()
	}
}

// CloseAllForInstance closes every connection whose instance key
// matches instanceKey and removes them from the registry. It returns
// the number of connections that were closed. It is used when an
// instance is stopped or restarted.
func (r *Registry) CloseAllForInstance(instanceKey string) int {
	r.mu.Lock()
	var toClose []*WSConnection
	for id, c := range r.conns {
		if c.key == instanceKey {
			toClose = append(toClose, c)
			delete(r.conns, id)
		}
	}
	r.mu.Unlock()
	for _, c := range toClose {
		c.CloseNow()
	}
	return len(toClose)
}

// Count returns the current number of registered connections.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.conns)
}
