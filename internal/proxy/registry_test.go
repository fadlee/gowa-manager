package proxy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// ---- registry tests ----
//
// The registry stores *WSConnection values that wrap real upstream
// *websocket.Conn instances. To exercise Add/Get/Delete/Close without
// spinning up a full WSBridge we dial a tiny echo WebSocket server
// (helpers shared with websocket_test.go) and wrap each dialed
// connection in a WSConnection.

// ---- Registry: Add / Get / Delete ----

func TestRegistry_AddGetDelete(t *testing.T) {
	r := NewRegistry()
	echo := newWSEchoServer(t)

	c1 := dialWSEcho(t, echo, "id1", "keyA")
	defer c1.CloseNow()

	if r.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", r.Count())
	}
	r.Add("id1", c1)
	if r.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", r.Count())
	}

	got, ok := r.Get("id1")
	if !ok {
		t.Fatal("Get(id1) not found")
	}
	if got != c1 {
		t.Fatal("Get returned a different pointer than the one added")
	}

	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get(missing) should return false")
	}

	r.Delete("id1")
	if r.Count() != 0 {
		t.Fatalf("Count() after Delete = %d, want 0", r.Count())
	}
	if _, ok := r.Get("id1"); ok {
		t.Fatal("Get after Delete should return false")
	}
	// Delete does NOT close the connection; verify it is still usable.
	if err := pingWS(c1); err != nil {
		t.Fatalf("connection closed after Delete: %v", err)
	}
}

// ---- Registry: Close closes and removes ----

func TestRegistry_Close(t *testing.T) {
	r := NewRegistry()
	echo := newWSEchoServer(t)

	c := dialWSEcho(t, echo, "id-close", "keyA")
	r.Add("id-close", c)

	r.Close("id-close")
	if r.Count() != 0 {
		t.Fatalf("Count() after Close = %d, want 0", r.Count())
	}
	if _, ok := r.Get("id-close"); ok {
		t.Fatal("Get after Close should return false")
	}
	// The upstream connection must actually be closed.
	if err := pingWS(c); err == nil {
		t.Fatal("expected connection to be closed after Registry.Close")
	}
	// Closing a missing id is a no-op (no panic, no error).
	r.Close("does-not-exist")
}

// ---- Registry: multiple clients for one instance ----

func TestRegistry_MultipleClientsSameInstance(t *testing.T) {
	r := NewRegistry()
	echo := newWSEchoServer(t)

	const key = "shared-key"
	conns := make([]*WSConnection, 5)
	for i := range conns {
		id := fmt.Sprintf("id-%d", i)
		conns[i] = dialWSEcho(t, echo, id, key)
		r.Add(id, conns[i])
	}
	if r.Count() != 5 {
		t.Fatalf("Count() = %d, want 5", r.Count())
	}

	// Each id resolves to its own connection.
	for i, c := range conns {
		got, ok := r.Get(fmt.Sprintf("id-%d", i))
		if !ok || got != c {
			t.Fatalf("Get(id-%d) returned wrong connection", i)
		}
	}

	// Closing one does not affect the others.
	r.Close("id-2")
	if r.Count() != 4 {
		t.Fatalf("Count() = %d, want 4", r.Count())
	}
	for i := range conns {
		if i == 2 {
			continue
		}
		if _, ok := r.Get(fmt.Sprintf("id-%d", i)); !ok {
			t.Fatalf("id-%d should still be present", i)
		}
	}

	// Cleanup the rest.
	for i := range conns {
		if i == 2 {
			continue
		}
		r.Close(fmt.Sprintf("id-%d", i))
	}
	if r.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", r.Count())
	}
}

// ---- Registry: CloseAll ----

func TestRegistry_CloseAll(t *testing.T) {
	r := NewRegistry()
	echo := newWSEchoServer(t)

	conns := make([]*WSConnection, 4)
	for i := range conns {
		c := dialWSEcho(t, echo, fmt.Sprintf("all-%d", i), fmt.Sprintf("k%d", i))
		conns[i] = c
		r.Add(c.id, c)
	}
	if r.Count() != 4 {
		t.Fatalf("Count() = %d, want 4", r.Count())
	}

	r.CloseAll()
	if r.Count() != 0 {
		t.Fatalf("Count() after CloseAll = %d, want 0", r.Count())
	}
	for _, c := range conns {
		if err := pingWS(c); err == nil {
			t.Fatalf("connection %s should be closed after CloseAll", c.id)
		}
	}
	// CloseAll on an empty registry is a no-op.
	r.CloseAll()
}

// ---- Registry: CloseAllForInstance ----

func TestRegistry_CloseAllForInstance(t *testing.T) {
	r := NewRegistry()
	echo := newWSEchoServer(t)

	// 3 connections for keyA, 2 for keyB.
	keyAConns := make([]*WSConnection, 3)
	for i := range keyAConns {
		c := dialWSEcho(t, echo, fmt.Sprintf("a-%d", i), "keyA")
		keyAConns[i] = c
		r.Add(c.id, c)
	}
	keyBConns := make([]*WSConnection, 2)
	for i := range keyBConns {
		c := dialWSEcho(t, echo, fmt.Sprintf("b-%d", i), "keyB")
		keyBConns[i] = c
		r.Add(c.id, c)
	}
	if r.Count() != 5 {
		t.Fatalf("Count() = %d, want 5", r.Count())
	}

	n := r.CloseAllForInstance("keyA")
	if n != 3 {
		t.Fatalf("CloseAllForInstance(keyA) = %d, want 3", n)
	}
	if r.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", r.Count())
	}
	for _, c := range keyAConns {
		if err := pingWS(c); err == nil {
			t.Fatalf("keyA connection %s should be closed", c.id)
		}
	}
	for _, c := range keyBConns {
		if err := pingWS(c); err != nil {
			t.Fatalf("keyB connection %s should still be open: %v", c.id, err)
		}
	}

	// CloseAllForInstance with no matching connections returns 0.
	if n := r.CloseAllForInstance("keyC"); n != 0 {
		t.Fatalf("CloseAllForInstance(keyC) = %d, want 0", n)
	}

	// Cleanup remaining.
	r.CloseAll()
}

// ---- Registry: Count accuracy ----

func TestRegistry_Count(t *testing.T) {
	r := NewRegistry()
	echo := newWSEchoServer(t)

	for i := 0; i < 10; i++ {
		c := dialWSEcho(t, echo, fmt.Sprintf("c-%d", i), "k")
		r.Add(c.id, c)
	}
	if got := r.Count(); got != 10 {
		t.Fatalf("Count() = %d, want 10", got)
	}
	r.Delete("c-0")
	r.Delete("c-1")
	if got := r.Count(); got != 8 {
		t.Fatalf("Count() = %d, want 8", got)
	}
	r.CloseAll()
	if got := r.Count(); got != 0 {
		t.Fatalf("Count() = %d, want 0", got)
	}
}

// ---- Registry: concurrent operations ----

func TestRegistry_Concurrent(t *testing.T) {
	r := NewRegistry()
	echo := newWSEchoServer(t)

	const goroutines = 16
	const perG = 25

	var wg sync.WaitGroup
	var addCount, delCount, closeCount atomic.Int64

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				id := fmt.Sprintf("g%d-%d", g, i)
				c := dialWSEcho(t, echo, id, fmt.Sprintf("key%d", g%3))
				r.Add(id, c)
				addCount.Add(1)

				switch i % 3 {
				case 0:
					r.Delete(id)
					delCount.Add(1)
					// Delete does not close; close manually to avoid leaks.
					c.CloseNow()
				case 1:
					r.Close(id)
					closeCount.Add(1)
				case 2:
					// leave it for CloseAll
				}
			}
		}(g)
	}
	wg.Wait()

	// After all goroutines finish, the only remaining connections are
	// the case-2 ones. CloseAll must drain the registry to zero.
	r.CloseAll()
	if got := r.Count(); got != 0 {
		t.Fatalf("Count() after concurrent ops + CloseAll = %d, want 0", got)
	}

	t.Logf("concurrent: adds=%d deletes=%d closes=%d", addCount.Load(), delCount.Load(), closeCount.Load())
}

// ---- Registry: Close is idempotent ----

func TestWSConnection_CloseIdempotent(t *testing.T) {
	echo := newWSEchoServer(t)
	c := dialWSEcho(t, echo, "once", "k")

	// Calling Close multiple times must not panic or double-close.
	_ = c.Close(websocket.StatusNormalClosure, "bye")
	_ = c.Close(websocket.StatusNormalClosure, "bye")
	c.CloseNow()
}

// ---- helpers shared with websocket_test.go ----

// pingWS verifies a WSConnection is still alive by performing a
// write+read round trip against the echo server. It returns nil when
// the connection is open and responsive, and a non-nil error when the
// connection has been closed. (We cannot use websocket.Conn.Ping here
// because Ping requires a concurrent Read on the same connection to
// process the pong control frame; a round trip exercises the same
// liveness without that constraint.)
func pingWS(c *WSConnection) error {
	if c == nil || c.conn == nil {
		return errors.New("nil connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte("alive?")); err != nil {
		return err
	}
	_, _, err := c.conn.Read(ctx)
	return err
}
