package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/fadlee/gowa-manager/internal/proxy"
)

// ---- test fixtures ----

// fakeProxyRepo is a minimal instances.Repository for proxy route tests.
// Only FindByKey is exercised; the other methods panic to catch misuse.
type fakeProxyRepo struct {
	items map[string]instances.Instance
}

func newFakeProxyRepo(items ...instances.Instance) *fakeProxyRepo {
	m := make(map[string]instances.Instance, len(items))
	for _, i := range items {
		m[i.Key] = i
	}
	return &fakeProxyRepo{items: m}
}

func (r *fakeProxyRepo) List(context.Context) ([]instances.Instance, error) {
	panic("List not used by proxy route tests")
}
func (r *fakeProxyRepo) FindByID(context.Context, int64) (instances.Instance, error) {
	panic("FindByID not used by proxy route tests")
}
func (r *fakeProxyRepo) FindByKey(_ context.Context, key string) (instances.Instance, error) {
	inst, ok := r.items[key]
	if !ok {
		return instances.Instance{}, instances.ErrNotFound
	}
	return inst, nil
}
func (r *fakeProxyRepo) Create(context.Context, instances.CreateInput) (instances.Instance, error) {
	panic("Create not used by proxy route tests")
}
func (r *fakeProxyRepo) Update(context.Context, instances.UpdateInput) (instances.Instance, error) {
	panic("Update not used by proxy route tests")
}
func (r *fakeProxyRepo) UpdateStatus(context.Context, int64, string, *string) (instances.Instance, error) {
	panic("UpdateStatus not used by proxy route tests")
}
func (r *fakeProxyRepo) ClearError(context.Context, int64) (instances.Instance, error) {
	panic("ClearError not used by proxy route tests")
}
func (r *fakeProxyRepo) UpdatePort(context.Context, int64, *int) error {
	panic("UpdatePort not used by proxy route tests")
}
func (r *fakeProxyRepo) Delete(context.Context, int64) error {
	panic("Delete not used by proxy route tests")
}

// intPtr returns a pointer to the given int.
func intPtr(n int) *int { return &n }

// upstreamPort extracts the TCP port from an httptest.Server URL.
func upstreamPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse upstream url %q: %v", srv.URL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse upstream port %q: %v", u.Port(), err)
	}
	return port
}

// freePort returns a TCP port that is (briefly) free. The listener is
// closed before returning so the port can be used for "connection
// refused" tests.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// newUpstreamServer starts an httptest.Server with the endpoints the
// proxy route tests need. The caller does not need to close it
// (t.Cleanup is registered).
func newUpstreamServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Root handler — used by the health check.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Echo handler — reflects method, path, query, and body.
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"method":  r.Method,
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
			"headers": r.Header.Get("X-Forwarded-Host"),
			"body":    string(body),
		})
	})

	// Text handler — returns plain text.
	mux.HandleFunc("/text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello text"))
	})

	// Binary handler — returns a small PNG.
	mux.HandleFunc("/binary", func(w http.ResponseWriter, r *http.Request) {
		data := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	})

	// Redirect handler — returns a 302 to the given location.
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		to := r.URL.Query().Get("to")
		if to == "" {
			to = "/echo"
		}
		http.Redirect(w, r, to, http.StatusFound)
	})

	// WebSocket echo handler — upgrades and bounces messages.
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		c.SetReadLimit(1 << 20)
		for {
			msgType, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), msgType, data); err != nil {
				return
			}
		}
	})

	// The proxy forwards the full /app/{key}/ prefix to the upstream
	// (GOWA instances are configured with that base path). Register the
	// same handlers under the test instance key prefixes so the upstream
	// mirrors real GOWA behaviour. Test instance keys are "mykey",
	// "wskey", and "smoke".
	mux.Handle("/app/mykey/", http.StripPrefix("/app/mykey", mux))
	mux.Handle("/app/wskey/", http.StripPrefix("/app/wskey", mux))
	mux.Handle("/app/smoke/", http.StripPrefix("/app/smoke", mux))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// proxyRouteDeps builds a Dependencies struct with the proxy-related
// fields wired to a fake repository and the given upstream server.
func proxyRouteDeps(t *testing.T, upstream *httptest.Server, insts ...instances.Instance) Dependencies {
	t.Helper()
	repo := newFakeProxyRepo(insts...)
	magicAuth := auth.NewMagicAuthServiceWithSecret("test-secret")
	return Dependencies{
		HTTPProxy:      proxy.NewHTTPProxy(proxy.NewTargetResolver(repo), magicAuth, nil),
		WSBridge:       proxy.NewWSBridge(proxy.NewTargetResolver(repo), magicAuth, proxy.NewRegistry()),
		MagicAuth:      magicAuth,
		InstanceLookup: repo,
	}
}

// newProxyRouteServer builds a full HTTP API server with proxy routes
// wired and hosts it behind an httptest.Server. The server is closed
// automatically via t.Cleanup.
func newProxyRouteServer(t *testing.T, deps Dependencies) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(deps))
	t.Cleanup(srv.Close)
	return srv
}

// noRedirectClient returns an http.Client that does not follow redirects.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// runningInstance builds a running instance pointing at the given port.
func runningInstance(key string, port int) instances.Instance {
	return instances.Instance{
		ID:     1,
		Key:    key,
		Name:   "Test " + key,
		Status: "running",
		Port:   &port,
	}
}

// stoppedInstance builds a stopped instance with a port.
func stoppedInstance(key string, port int) instances.Instance {
	return instances.Instance{
		ID:     2,
		Key:    key,
		Name:   "Stopped " + key,
		Status: "stopped",
		Port:   &port,
	}
}

// ---- 1. Status endpoint ----

func TestProxyRoutes_StatusFound(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body proxyStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.InstanceKey != "mykey" {
		t.Fatalf("instanceKey = %q, want mykey", body.InstanceKey)
	}
	if body.InstanceName != "Test mykey" {
		t.Fatalf("instanceName = %q, want 'Test mykey'", body.InstanceName)
	}
	if body.Status != "running" {
		t.Fatalf("status = %q, want running", body.Status)
	}
	if body.Port == nil || *body.Port != port {
		t.Fatalf("port = %v, want %d", body.Port, port)
	}
	if body.TargetPort == nil || *body.TargetPort != port {
		t.Fatalf("targetPort = %v, want %d", body.TargetPort, port)
	}
	if body.ProxyPath != "app/mykey" {
		t.Fatalf("proxyPath = %q, want app/mykey", body.ProxyPath)
	}
}

func TestProxyRoutes_StatusNotFound(t *testing.T) {
	upstream := newUpstreamServer(t)
	deps := proxyRouteDeps(t, upstream) // no instances
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/nonexistent/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "Instance not found" {
		t.Fatalf("error = %v, want 'Instance not found'", body["error"])
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
}

// ---- 2. Health endpoint ----

func TestProxyRoutes_HealthRunningHealthy(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body proxyHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Healthy {
		t.Fatal("healthy = false, want true")
	}
	if body.Status != "running" {
		t.Fatalf("status = %q, want running", body.Status)
	}
}

func TestProxyRoutes_HealthStoppedUnhealthy(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := stoppedInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body proxyHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Healthy {
		t.Fatal("healthy = true, want false (instance stopped)")
	}
}

func TestProxyRoutes_HealthNotFound(t *testing.T) {
	upstream := newUpstreamServer(t)
	deps := proxyRouteDeps(t, upstream)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/nonexistent/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// ---- 3. WebSocket route ----

func TestProxyRoutes_WebSocketEcho(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("wskey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	// Dial the proxied WebSocket endpoint at /app/wskey/ws.
	// The upstream /ws handler echoes messages.
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/app/wskey/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	c.SetReadLimit(1 << 20)

	// Send a message and expect it echoed back.
	payload := []byte("hello websocket")
	if err := c.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	msgType, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if msgType != websocket.MessageText {
		t.Fatalf("msgType = %v, want Text", msgType)
	}
	if string(data) != string(payload) {
		t.Fatalf("echo = %q, want %q", data, payload)
	}
}

// ---- 4. Autologin with valid token ----

func TestProxyRoutes_AutologinValidToken(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	// Create a valid token.
	token, _ := deps.MagicAuth.CreateToken("mykey", time.Now())

	// Request with ?autologin=<token>.
	client := noRedirectClient()
	resp, err := client.Get(srv.URL + "/app/mykey/?autologin=" + token)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if strings.Contains(loc, "autologin") {
		t.Fatalf("Location %q still contains autologin", loc)
	}
	if loc != "/app/mykey/" {
		t.Fatalf("Location = %q, want /app/mykey/", loc)
	}
	cookie := resp.Header.Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("missing Set-Cookie header")
	}
	if !strings.HasPrefix(cookie, "gowa_admin_auth_mykey=") {
		t.Fatalf("Set-Cookie = %q, want gowa_admin_auth_mykey=...", cookie)
	}
}

// ---- 5. Autologin with invalid token ----

func TestProxyRoutes_AutologinInvalidToken(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/?autologin=invalid-token")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Invalid or expired admin link" {
		t.Fatalf("body = %q, want 'Invalid or expired admin link'", body)
	}
	cookie := resp.Header.Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("missing Set-Cookie header")
	}
	if !strings.Contains(cookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want Max-Age=0 (clear)", cookie)
	}
}

// ---- 6. Autologin with expired token ----

func TestProxyRoutes_AutologinExpiredToken(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	// Create a token that expired 1 minute ago.
	token, _ := deps.MagicAuth.CreateToken("mykey", time.Now().Add(-2*time.Minute))

	resp, err := http.Get(srv.URL + "/app/mykey/?autologin=" + token)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Invalid or expired admin link" {
		t.Fatalf("body = %q, want 'Invalid or expired admin link'", body)
	}
}

// ---- 7. Normal proxy request (no autologin) ----

func TestProxyRoutes_NormalProxyForwards(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/echo?foo=bar")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["method"] != "GET" {
		t.Fatalf("method = %v, want GET", body["method"])
	}
	if body["path"] != "/echo" {
		t.Fatalf("path = %v, want /echo", body["path"])
	}
	if body["query"] != "foo=bar" {
		t.Fatalf("query = %v, want foo=bar", body["query"])
	}
}

// ---- 8. Proxy with instance not found ----

func TestProxyRoutes_ProxyInstanceNotFound(t *testing.T) {
	upstream := newUpstreamServer(t)
	deps := proxyRouteDeps(t, upstream) // no instances
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/nonexistent/echo")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "Instance not found" {
		t.Fatalf("error = %v, want 'Instance not found'", body["error"])
	}
}

// ---- 9. Proxy with instance not running ----

func TestProxyRoutes_ProxyInstanceNotRunning(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := stoppedInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/echo")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "Instance is not running" {
		t.Fatalf("error = %v, want 'Instance is not running'", body["error"])
	}
	if body["instanceKey"] != "mykey" {
		t.Fatalf("instanceKey = %v, want mykey", body["instanceKey"])
	}
}

// ---- 10. Proxy with upstream error ----

func TestProxyRoutes_ProxyUpstreamError(t *testing.T) {
	// Use a port that has nothing listening on it.
	deadPort := freePort(t)
	inst := runningInstance("mykey", deadPort)
	deps := proxyRouteDeps(t, nil, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/echo")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "Proxy request failed" {
		t.Fatalf("error = %v, want 'Proxy request failed'", body["error"])
	}
}

// ---- 11. Content types preserved ----

func TestProxyRoutes_ContentTypeJSON(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/echo")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func TestProxyRoutes_ContentTypeBinary(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/binary")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "image/png") {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
}

func TestProxyRoutes_ContentTypeText(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/text")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello text" {
		t.Fatalf("body = %q, want 'hello text'", body)
	}
}

// ---- 12. Redirects: upstream redirect → Location rewritten ----

func TestProxyRoutes_UpstreamRedirectRewritten(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	// Upstream /redirect returns 302 to /echo.
	// The proxy should rewrite Location to /app/mykey/echo.
	client := noRedirectClient()
	resp, err := client.Get(srv.URL + "/app/mykey/redirect")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/app/mykey/echo" {
		t.Fatalf("Location = %q, want /app/mykey/echo", loc)
	}
}

// ---- 13. Route precedence: /app/{key}/status not caught by /app/{key}/* ----

func TestProxyRoutes_RoutePrecedenceStatus(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	// /app/mykey/status should hit the status handler, not the proxy.
	resp, err := http.Get(srv.URL + "/app/mykey/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (status handler, not proxy)", resp.StatusCode)
	}
	// Verify it's the status JSON, not a proxied response.
	var body proxyStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ProxyPath != "app/mykey" {
		t.Fatalf("proxyPath = %q, want app/mykey (status handler)", body.ProxyPath)
	}
}

func TestProxyRoutes_RoutePrecedenceHealth(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	// /app/mykey/health should hit the health handler, not the proxy.
	resp, err := http.Get(srv.URL + "/app/mykey/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (health handler, not proxy)", resp.StatusCode)
	}
	var body proxyHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.InstanceKey != "mykey" {
		t.Fatalf("instanceKey = %q, want mykey (health handler)", body.InstanceKey)
	}
}

// ---- 14. Cookie set/clear ----

func TestProxyRoutes_AutologinSetsCookie(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	token, _ := deps.MagicAuth.CreateToken("mykey", time.Now())
	client := noRedirectClient()
	resp, err := client.Get(srv.URL + "/app/mykey/?autologin=" + token)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	cookie := resp.Header.Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("missing Set-Cookie")
	}
	if !strings.Contains(cookie, "gowa_admin_auth_mykey=") {
		t.Fatalf("Set-Cookie = %q, want gowa_admin_auth_mykey=...", cookie)
	}
	if !strings.Contains(cookie, "Path=/app/mykey") {
		t.Fatalf("Set-Cookie = %q, want Path=/app/mykey", cookie)
	}
	if !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("Set-Cookie = %q, want HttpOnly", cookie)
	}
}

func TestProxyRoutes_AutologinClearsCookie(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/?autologin=bogus")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	cookie := resp.Header.Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("missing Set-Cookie")
	}
	if !strings.Contains(cookie, "gowa_admin_auth_mykey=;") {
		t.Fatalf("Set-Cookie = %q, want empty value (clear)", cookie)
	}
	if !strings.Contains(cookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want Max-Age=0", cookie)
	}
}

// ---- Additional: autologin with extra query params preserved ----

func TestProxyRoutes_AutologinPreservesOtherQueryParams(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	token, _ := deps.MagicAuth.CreateToken("mykey", time.Now())
	client := noRedirectClient()
	resp, err := client.Get(srv.URL + "/app/mykey/echo?autologin=" + token + "&foo=bar")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if strings.Contains(loc, "autologin") {
		t.Fatalf("Location %q still contains autologin", loc)
	}
	if !strings.Contains(loc, "foo=bar") {
		t.Fatalf("Location = %q, want foo=bar preserved", loc)
	}
}

// ---- Additional: proxy root path (/app/{key} without trailing slash) ----

func TestProxyRoutes_RootPathNoTrailingSlash(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	// /app/mykey (no trailing slash) should proxy to the instance root.
	resp, err := http.Get(srv.URL + "/app/mykey")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// ---- Additional: proxy routes outside Basic Auth ----

func TestProxyRoutes_OutsideBasicAuth(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	// Configure Basic Auth credentials — proxy routes should NOT require them.
	deps.AdminUsername = "admin"
	deps.AdminPassword = "secret"
	srv := newProxyRouteServer(t, deps)

	// Request a proxy route WITHOUT Basic Auth credentials.
	resp, err := http.Get(srv.URL + "/app/mykey/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Should NOT get 401 — proxy routes are outside Basic Auth.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("proxy route returned 401, expected to be outside Basic Auth")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// ---- Additional: POST method forwarded through proxy ----

func TestProxyRoutes_PostMethodForwarded(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("mykey", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Post(srv.URL+"/app/mykey/echo", "text/plain", strings.NewReader("post body"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["method"] != "POST" {
		t.Fatalf("method = %v, want POST", body["method"])
	}
	if body["body"] != "post body" {
		t.Fatalf("body = %v, want 'post body'", body["body"])
	}
}

// ---- Additional: status endpoint with nil port ----

func TestProxyRoutes_StatusNilPort(t *testing.T) {
	inst := instances.Instance{
		ID:     3,
		Key:    "noport",
		Name:   "No Port",
		Status: "stopped",
		Port:   nil,
	}
	deps := proxyRouteDeps(t, nil, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/noport/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body proxyStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Port != nil {
		t.Fatalf("port = %v, want nil", body.Port)
	}
	if body.TargetPort != nil {
		t.Fatalf("targetPort = %v, want nil", body.TargetPort)
	}
}

// ---- Additional: health check with unreachable port ----

func TestProxyRoutes_HealthUnreachablePort(t *testing.T) {
	deadPort := freePort(t)
	inst := runningInstance("mykey", deadPort)
	deps := proxyRouteDeps(t, nil, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/mykey/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body proxyHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Healthy {
		t.Fatal("healthy = true, want false (port unreachable)")
	}
}

// ---- Ensure no proxy routes registered when deps are nil ----

func TestProxyRoutes_NotRegisteredWhenDepsNil(t *testing.T) {
	srv := newProxyRouteServer(t, Dependencies{})

	resp, err := http.Get(srv.URL + "/app/mykey/status")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Without proxy deps, /app/ falls through to the static handler
	// or 404. It should NOT be a proxy status response.
	if resp.StatusCode == http.StatusOK {
		// Could be the static handler serving index.html; just verify
		// it's not the proxy status JSON.
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") {
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), "proxyPath") {
				t.Fatal("proxy routes should not be registered when deps are nil")
			}
		}
	}
}

// ---- Helper: verify all TestProxyRoutes tests can run ----

func TestProxyRoutes_SmokeTest(t *testing.T) {
	// This is a meta-test that verifies the test harness compiles
	// and the basic plumbing works.
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := runningInstance("smoke", port)
	deps := proxyRouteDeps(t, upstream, inst)
	srv := newProxyRouteServer(t, deps)

	resp, err := http.Get(srv.URL + "/app/smoke/echo")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
