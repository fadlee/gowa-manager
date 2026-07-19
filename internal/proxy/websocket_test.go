package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
)

// ---- shared WebSocket test helpers (used by registry_test.go too) ----

// newTestHTTPServer wraps httptest.NewServer and registers Close via
// t.Cleanup so callers don't need to defer it.
func newTestHTTPServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// wsURL converts an http:// httptest server URL into a ws:// URL.
func wsURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + httpURL[len("https://"):]
	}
	if strings.HasPrefix(httpURL, "http://") {
		return "ws://" + httpURL[len("http://"):]
	}
	return httpURL
}

// newWSEchoServer starts a coder/websocket echo server that bounces
// every received message back to the sender. The server is closed
// automatically via t.Cleanup. It returns the running httptest.Server;
// use srv.URL for dialing and upstreamPort(t, srv) for the port.
func newWSEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		// Raise the read limit so large-payload tests pass through.
		c.SetReadLimit(1 << 20)
		echoLoop(context.Background(), c)
	})
	return newTestHTTPServer(t, handler)
}

// dialWSEcho dials the echo server at srv.URL and returns a WSConnection
// wrapping the upstream connection. The caller is responsible for
// closing the connection (or registering it in a Registry that will).
func dialWSEcho(t *testing.T, srv *httptest.Server, id, key string) *WSConnection {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv.URL), &websocket.DialOptions{})
	if err != nil {
		t.Fatalf("dial echo server: %v", err)
	}
	return &WSConnection{conn: c, id: id, key: key}
}

// echoLoop reads messages from c and writes them back until an error
// (close or disconnect) terminates the loop.
func echoLoop(ctx context.Context, c *websocket.Conn) {
	for {
		msgType, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if err := c.Write(ctx, msgType, data); err != nil {
			return
		}
	}
}

// ---- WebSocket bridge test harness ----
//
// newWSBridgeServer wires a WSBridge to a fake repository containing the
// given instances, hosts it behind an httptest.Server, and returns the
// server along with the bridge and its registry. The server is closed
// automatically via t.Cleanup.
func newWSBridgeServer(t *testing.T, magicAuth *auth.MagicAuthService, insts ...instances.Instance) (*httptest.Server, *WSBridge, *Registry) {
	t.Helper()
	repo := newFakeRepo(insts...)
	registry := NewRegistry()
	bridge := NewWSBridge(NewTargetResolver(repo), magicAuth, registry)
	srv := newTestHTTPServer(t, http.HandlerFunc(bridge.ServeWS))
	return srv, bridge, registry
}

// wsBridgeURL builds a URL for a proxied WebSocket request:
// {proxyServer}/app/{key}{subPath}.
func wsBridgeURL(proxyServer, key, subPath string) string {
	return wsURL(proxyServer) + "/" + ProxyPrefix + "/" + key + subPath
}

// dialBridge dials the WSBridge as a client and returns the connection.
// The caller is responsible for closing it.
func dialBridge(t *testing.T, httpURL, key, subPath string, opts *websocket.DialOptions) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsBridgeURL(httpURL, key, subPath), opts)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	return c
}

// wsRoundTrip sends a single message and reads a single response.
func wsRoundTrip(t *testing.T, c *websocket.Conn, msgType websocket.MessageType, payload []byte) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, msgType, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	rt, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return rt, data
}

// ---- 1. Text message echo ----

func TestWebSocket_TextEcho(t *testing.T) {
	upstream := newWSEchoServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)
	defer c.Close(websocket.StatusNormalClosure, "")

	rt, data := wsRoundTrip(t, c, websocket.MessageText, []byte("hello text"))
	if rt != websocket.MessageText {
		t.Fatalf("response type = %v, want MessageText", rt)
	}
	if string(data) != "hello text" {
		t.Fatalf("response = %q, want %q", data, "hello text")
	}

	c.Close(websocket.StatusNormalClosure, "done")
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 2. Binary message echo ----

func TestWebSocket_BinaryEcho(t *testing.T) {
	upstream := newWSEchoServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)
	defer c.Close(websocket.StatusNormalClosure, "")

	payload := []byte{0x00, 0x01, 0x02, 0xff, 0xfe}
	rt, data := wsRoundTrip(t, c, websocket.MessageBinary, payload)
	if rt != websocket.MessageBinary {
		t.Fatalf("response type = %v, want MessageBinary", rt)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("response = %x, want %x", data, payload)
	}

	c.Close(websocket.StatusNormalClosure, "done")
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 3. Large payloads ----

func TestWebSocket_LargePayload(t *testing.T) {
	upstream := newWSEchoServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)
	defer c.Close(websocket.StatusNormalClosure, "")
	// The test client uses the library default 32 KiB read limit; raise
	// it so the 100 KB echo response can be read back.
	c.SetReadLimit(1 << 20)

	// 100 KB payload — above the library default 32 KB read limit, so
	// the bridge must raise the limit for the copy loops to succeed.
	payload := make([]byte, 100*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	rt, data := wsRoundTrip(t, c, websocket.MessageBinary, payload)
	if rt != websocket.MessageBinary {
		t.Fatalf("response type = %v, want MessageBinary", rt)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("response length = %d, want %d", len(data), len(payload))
	}

	c.Close(websocket.StatusNormalClosure, "done")
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 4. Query string forwarding ----

func TestWebSocket_QueryForwarding(t *testing.T) {
	// Upstream records the request URL so the test can assert the query
	// was forwarded.
	var seenQuery atomic.Value // string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery.Store(r.URL.RawQuery)
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		echoLoop(context.Background(), c)
	})
	upstream := newTestHTTPServer(t, handler)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws?foo=bar&n=42", nil)
	defer c.Close(websocket.StatusNormalClosure, "")

	_, _ = wsRoundTrip(t, c, websocket.MessageText, []byte("ping"))

	got := seenQuery.Load()
	if got != "foo=bar&n=42" {
		t.Fatalf("upstream query = %v, want %q", got, "foo=bar&n=42")
	}

	c.Close(websocket.StatusNormalClosure, "done")
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 5. Safe headers forwarded ----

func TestWebSocket_SafeHeadersForwarded(t *testing.T) {
	var seenHeaders atomic.Value // map[string]string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdrs := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				hdrs[k] = v[0]
			}
		}
		seenHeaders.Store(hdrs)
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		echoLoop(context.Background(), c)
	})
	upstream := newTestHTTPServer(t, handler)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c, _, err := websocket.Dial(context.Background(), wsBridgeURL(srv.URL, "k", "/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization":     []string{"Bearer abc"},
			"Cookie":            []string{"sess=xyz"},
			"Origin":            []string{"http://example.com"},
			"User-Agent":        []string{"test-agent/1.0"},
			"Accept-Language":   []string{"en-US"},
			"X-Should-Not-Pass": []string{"nope"},
		},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	_, _ = wsRoundTrip(t, c, websocket.MessageText, []byte("ping"))

	hdrs, ok := seenHeaders.Load().(map[string]string)
	if !ok {
		t.Fatal("upstream did not record headers")
	}
	checks := map[string]string{
		"Authorization":   "Bearer abc",
		"Cookie":          "sess=xyz",
		"Origin":          "http://example.com",
		"User-Agent":      "test-agent/1.0",
		"Accept-Language": "en-US",
	}
	for h, want := range checks {
		if got := hdrs[h]; got != want {
			t.Fatalf("upstream header %s = %q, want %q", h, got, want)
		}
	}
	if v := hdrs["X-Should-Not-Pass"]; v != "" {
		t.Fatalf("unsafe header X-Should-Not-Pass was forwarded as %q", v)
	}

	c.Close(websocket.StatusNormalClosure, "done")
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 6. Subprotocol negotiation ----

func TestWebSocket_Subprotocol(t *testing.T) {
	// Upstream accepts and records the negotiated subprotocol. It
	// negotiates whatever subprotocols the bridge forwards.
	var upstreamSubproto atomic.Value // string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
			Subprotocols:       parseSubprotocols(r.Header),
		})
		if err != nil {
			return
		}
		upstreamSubproto.Store(c.Subprotocol())
		defer c.Close(websocket.StatusNormalClosure, "")
		echoLoop(context.Background(), c)
	})
	upstream := newTestHTTPServer(t, handler)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", &websocket.DialOptions{
		Subprotocols: []string{"chat.v1", "chat.v2"},
	})
	defer c.Close(websocket.StatusNormalClosure, "")

	if c.Subprotocol() == "" {
		t.Fatal("client negotiated no subprotocol")
	}
	if c.Subprotocol() != "chat.v1" {
		t.Fatalf("client subprotocol = %q, want chat.v1", c.Subprotocol())
	}

	_, _ = wsRoundTrip(t, c, websocket.MessageText, []byte("hi"))
	if got := upstreamSubproto.Load(); got != "chat.v1" {
		t.Fatalf("upstream subprotocol = %v, want chat.v1", got)
	}

	c.Close(websocket.StatusNormalClosure, "done")
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 7. Ping/pong keeps connection alive ----

func TestWebSocket_PingPong(t *testing.T) {
	upstream := newWSEchoServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Ping requires a concurrent Read on the same connection to process
	// the pong control frame; start a background reader that discards
	// messages so pings can complete. The reader runs for the lifetime
	// of the connection to avoid racing with a separate Read.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	// A successful ping proves the bridge forwards ping/pong control
	// frames and the connection is alive.

	c.Close(websocket.StatusNormalClosure, "done")
	<-readDone
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 8. Normal close with code and reason ----

func TestWebSocket_NormalClose(t *testing.T) {
	// Upstream closes with a specific code/reason; the bridge must
	// forward that close frame to the client so the client observes the
	// same code and reason.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		_ = c.Close(websocket.StatusGoingAway, "restarting")
	})
	upstream := newTestHTTPServer(t, handler)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)
	defer c.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if err == nil {
		t.Fatal("expected Read to return an error after upstream close")
	}
	status := websocket.CloseStatus(err)
	if status != websocket.StatusGoingAway {
		t.Fatalf("close status = %v, want %v (StatusGoingAway)", status, websocket.StatusGoingAway)
	}
	var ce websocket.CloseError
	if errors.As(err, &ce) && ce.Reason != "restarting" {
		t.Fatalf("close reason = %q, want %q", ce.Reason, "restarting")
	}

	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 9. Abnormal client disconnect ----

func TestWebSocket_AbnormalClientDisconnect(t *testing.T) {
	upstream := newWSEchoServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)

	// CloseNow drops the TCP connection without a close handshake.
	c.CloseNow()

	// The registry must drain to zero once the bridge notices the
	// disconnect and tears down the upstream.
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 10. Upstream disconnect ----

func TestWebSocket_UpstreamDisconnect(t *testing.T) {
	// Upstream closes the connection immediately after the handshake.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		_ = c.Close(websocket.StatusGoingAway, "restarting")
	})
	upstream := newTestHTTPServer(t, handler)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)
	defer c.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if err == nil {
		t.Fatal("expected Read to return an error after upstream disconnect")
	}

	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 11. Cancellation mid-connection ----

func TestWebSocket_Cancellation(t *testing.T) {
	upstream := newWSEchoServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, bridge, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)
	defer c.CloseNow()

	// Wait for the bridge to register the upstream connection before
	// tearing it down; dialBridge returns after the handshake, but the
	// bridge registers the connection asynchronously after dialing the
	// upstream.
	waitRegistryNonZero(t, registry, 3*time.Second)

	// CloseAll on the bridge cancels/closes every upstream connection,
	// which should propagate to the client as a disconnect.
	bridge.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if err == nil {
		t.Fatal("expected Read to return an error after CloseAll")
	}

	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 12. Multiple concurrent clients — no cross-client mixing ----

func TestWebSocket_ConcurrentClientsNoMixing(t *testing.T) {
	upstream := newWSEchoServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			c := dialBridge(t, srv.URL, "k", "/ws", nil)
			defer c.Close(websocket.StatusNormalClosure, "")

			payload := fmt.Sprintf("client-%d-msg", i)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
				errCh <- fmt.Errorf("client %d write: %w", i, err)
				return
			}
			_, data, err := c.Read(ctx)
			if err != nil {
				errCh <- fmt.Errorf("client %d read: %w", i, err)
				return
			}
			if string(data) != payload {
				errCh <- fmt.Errorf("client %d got %q, want %q (cross-client mixing!)", i, data, payload)
				return
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	waitRegistryZero(t, registry, 5*time.Second)
}

// ---- 13. Instance not found → connection rejected ----

func TestWebSocket_InstanceNotFound(t *testing.T) {
	srv, _, registry := newWSBridgeServer(t, nil) // no instances

	c, _, err := websocket.Dial(context.Background(), wsBridgeURL(srv.URL, "missing", "/ws"), nil)
	if err == nil {
		c.CloseNow()
		defer c.CloseNow()
		t.Fatal("expected dial to fail for missing instance")
	}
	if registry.Count() != 0 {
		t.Fatalf("registry Count() = %d, want 0", registry.Count())
	}
}

// ---- 14. Instance not running → connection rejected ----

func TestWebSocket_InstanceNotRunning(t *testing.T) {
	stopped := instances.Instance{ID: 1, Key: "k", Status: "stopped", Port: intPtr(8080)}
	srv, _, registry := newWSBridgeServer(t, nil, stopped)

	c, _, err := websocket.Dial(context.Background(), wsBridgeURL(srv.URL, "k", "/ws"), nil)
	if err == nil {
		c.CloseNow()
		defer c.CloseNow()
		t.Fatal("expected dial to fail for stopped instance")
	}
	if registry.Count() != 0 {
		t.Fatalf("registry Count() = %d, want 0", registry.Count())
	}
}

// ---- 15. Auth injection: instance Basic Auth injected when no Authorization ----

func TestWebSocket_AuthInjection(t *testing.T) {
	var seenAuth atomic.Value // string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth.Store(r.Header.Get("Authorization"))
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		echoLoop(context.Background(), c)
	})
	upstream := newTestHTTPServer(t, handler)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, basicAuthConfig("admin", "s3cret"))
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c := dialBridge(t, srv.URL, "k", "/ws", nil)
	defer c.Close(websocket.StatusNormalClosure, "")

	_, _ = wsRoundTrip(t, c, websocket.MessageText, []byte("hi"))

	got := seenAuth.Load()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:s3cret"))
	if got != want {
		t.Fatalf("upstream Authorization = %v, want %v", got, want)
	}

	c.Close(websocket.StatusNormalClosure, "done")
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- 16. Explicit Authorization header is preserved (no injection) ----

func TestWebSocket_ExplicitAuthPreserved(t *testing.T) {
	var seenAuth atomic.Value // string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth.Store(r.Header.Get("Authorization"))
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		echoLoop(context.Background(), c)
	})
	upstream := newTestHTTPServer(t, handler)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, basicAuthConfig("admin", "s3cret"))
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	c, _, err := websocket.Dial(context.Background(), wsBridgeURL(srv.URL, "k", "/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer custom"}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	_, _ = wsRoundTrip(t, c, websocket.MessageText, []byte("hi"))

	if got := seenAuth.Load(); got != "Bearer custom" {
		t.Fatalf("upstream Authorization = %v, want Bearer custom", got)
	}

	c.Close(websocket.StatusNormalClosure, "done")
	waitRegistryZero(t, registry, 3*time.Second)
}

// ---- helpers ----

// waitRegistryNonZero polls the registry until Count() is non-zero or
// the timeout elapses. It is used to synchronise tests that need the
// bridge to have registered a connection before acting on it.
func waitRegistryNonZero(t *testing.T, r *Registry, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.Count() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if r.Count() == 0 {
		t.Fatalf("registry did not become non-empty within %v", timeout)
	}
}

// waitRegistryZero polls the registry until Count() reaches zero or the
// timeout elapses. WebSocket teardown is asynchronous (close handshake
// + copy loop cancellation), so tests must wait rather than assert
// synchronously.
func waitRegistryZero(t *testing.T, r *Registry, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.Count() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Count() != 0 {
		t.Fatalf("registry did not drain to zero within %v (Count=%d)", timeout, r.Count())
	}
}

// ---- leak check: registry returns to zero across many iterations ----

func TestWebSocket_RegistryDrainsAcrossIterations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long iteration test in -short mode")
	}
	upstream := newWSEchoServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv, _, registry := newWSBridgeServer(t, nil, inst)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		c := dialBridge(t, srv.URL, "k", "/ws", nil)
		_, _ = wsRoundTrip(t, c, websocket.MessageText, []byte("iter"))
		c.Close(websocket.StatusNormalClosure, "done")
	}
	waitRegistryZero(t, registry, 5*time.Second)
}

// Ensure unused imports are referenced (io, json, errors) for future
// test expansions without breaking the build.
var (
	_ = io.EOF
	_ = json.Valid
	_ = errors.Is
)
