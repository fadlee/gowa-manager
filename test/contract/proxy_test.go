package contract

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/httpapi"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/fadlee/gowa-manager/internal/proxy"
)

// This file verifies the Go proxy implementation against the same
// behavioral contract that the Bun reference implements. The tests are
// NOT live Bun-vs-Go process comparisons; they exercise the Go proxy
// (HTTPProxy, WSBridge, and the /app/{key}/* route handlers) against an
// httptest upstream fixture that mirrors the endpoints in
// internal/testutil/upstream.
//
// Test layout:
//   - TestProxyParity_HTTP:      15 HTTP forwarding subtests
//   - TestProxyParity_WebSocket:  6 WebSocket forwarding subtests

// ---- shared helpers (unique names to avoid collisions with other
// contract test files) ----

// contractFakeRepo is a minimal instances.Repository for proxy contract
// tests. Only FindByKey is exercised by the proxy; the other methods
// panic to catch accidental misuse.
type contractFakeRepo struct {
	items map[string]instances.Instance
}

func newContractFakeRepo(items ...instances.Instance) *contractFakeRepo {
	m := make(map[string]instances.Instance, len(items))
	for _, i := range items {
		m[i.Key] = i
	}
	return &contractFakeRepo{items: m}
}

func (r *contractFakeRepo) List(context.Context) ([]instances.Instance, error) {
	panic("List not used by proxy contract tests")
}
func (r *contractFakeRepo) FindByID(context.Context, int64) (instances.Instance, error) {
	panic("FindByID not used by proxy contract tests")
}
func (r *contractFakeRepo) FindByKey(_ context.Context, key string) (instances.Instance, error) {
	inst, ok := r.items[key]
	if !ok {
		return instances.Instance{}, instances.ErrNotFound
	}
	return inst, nil
}
func (r *contractFakeRepo) Create(context.Context, instances.CreateInput) (instances.Instance, error) {
	panic("Create not used by proxy contract tests")
}
func (r *contractFakeRepo) Update(context.Context, instances.UpdateInput) (instances.Instance, error) {
	panic("Update not used by proxy contract tests")
}
func (r *contractFakeRepo) UpdateStatus(context.Context, int64, string, *string) (instances.Instance, error) {
	panic("UpdateStatus not used by proxy contract tests")
}
func (r *contractFakeRepo) ClearError(context.Context, int64) (instances.Instance, error) {
	panic("ClearError not used by proxy contract tests")
}
func (r *contractFakeRepo) UpdatePort(context.Context, int64, *int) error {
	panic("UpdatePort not used by proxy contract tests")
}
func (r *contractFakeRepo) Delete(context.Context, int64) error {
	panic("Delete not used by proxy contract tests")
}

// contractEchoResponse is the JSON body returned by the /echo upstream.
type contractEchoResponse struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// minimalPNGHex is a valid 1x1 PNG image (67 bytes), matching the
// upstream fixture.
const contractMinimalPNGHex = "89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000d49444154789c6300010000000500010d0a2db40000000049454e44ae426082"

// newContractUpstream starts an httptest.Server that mirrors the subset
// of the upstream fixture endpoints used by the proxy contract tests.
// The server is closed automatically via t.Cleanup.
func newContractUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		query := make(map[string]string)
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				query[k] = v[0]
			}
		}
		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(contractEchoResponse{
			Method:  r.Method,
			Path:    r.URL.Path,
			Query:   query,
			Headers: headers,
			Body:    string(body),
		})
	})

	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		chunks := contractQueryInt(r, "chunks", 5)
		delay := contractQueryInt(r, "delay", 50)
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if flusher != nil {
			flusher.Flush()
		}
		for i := 0; i < chunks; i++ {
			fmt.Fprintf(w, "chunk %d\n", i)
			if flusher != nil {
				flusher.Flush()
			}
			if delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
	})

	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		to := r.URL.Query().Get("to")
		if to == "" {
			to = "/echo"
		}
		code := contractQueryInt(r, "code", http.StatusFound)
		if code < 300 || code > 399 {
			code = http.StatusFound
		}
		http.Redirect(w, r, to, code)
	})

	mux.HandleFunc("/set-cookie", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			name = "test"
		}
		value := r.URL.Query().Get("value")
		if value == "" {
			value = "cookie"
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}
		maxAge := contractQueryInt(r, "maxAge", 3600)
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    value,
			Path:     path,
			MaxAge:   maxAge,
			HttpOnly: true,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":   name,
			"value":  value,
			"path":   path,
			"maxAge": maxAge,
		})
	})

	mux.HandleFunc("/binary/png", func(w http.ResponseWriter, r *http.Request) {
		data, err := hex.DecodeString(contractMinimalPNGHex)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Root handler returns 200 so the health check (which pings "/")
	// reports healthy.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// /status?code=N returns the given status code with an empty body.
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		code := contractQueryInt(r, "code", http.StatusOK)
		w.WriteHeader(code)
	})

	// /ws is a WebSocket echo endpoint.
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusInternalError, "")
		c.SetReadLimit(1 << 20)
		contractEchoLoop(r.Context(), c)
	})

	// /ws/disconnect closes the connection immediately with a given
	// code/reason (query params).
	mux.HandleFunc("/ws/disconnect", func(w http.ResponseWriter, r *http.Request) {
		code := contractQueryInt(r, "code", 4000)
		reason := r.URL.Query().Get("reason")
		if reason == "" {
			reason = "forced disconnect"
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		c.Close(websocket.StatusCode(code), reason)
	})

	// The proxy forwards the full /app/{key}/ prefix to the upstream
	// (GOWA instances are configured with that base path). Register the
	// same handlers under /app/k/ so the test upstream mirrors real GOWA
	// behaviour. The test instance key is always "k".
	mux.Handle("/app/k/", http.StripPrefix("/app/k", mux))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// contractQueryInt parses a query parameter as an int with a fallback.
func contractQueryInt(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// contractEchoLoop reads messages from c and writes them back until an
// error (close or disconnect) terminates the loop.
func contractEchoLoop(ctx context.Context, c *websocket.Conn) {
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

// contractUpstreamPort extracts the TCP port from an httptest.Server URL.
func contractUpstreamPort(t *testing.T, srv *httptest.Server) int {
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

// contractBasicAuthConfig returns an instance Config JSON string with one
// basicAuth entry (user:pass).
func contractBasicAuthConfig(user, pass string) string {
	return fmt.Sprintf(`{"flags":{"basicAuth":[{"username":%q,"password":%q}]}}`, user, pass)
}

// contractRunningInstance builds an instances.Instance whose port points
// at the given upstream server and that is marked running.
func contractRunningInstance(key string, port int, config string) instances.Instance {
	return instances.Instance{
		ID:     1,
		Key:    key,
		Name:   key,
		Status: "running",
		Port:   &port,
		Config: config,
	}
}

// contractStoppedInstance builds an instances.Instance that exists but
// is not running (no port).
func contractStoppedInstance(key string) instances.Instance {
	return instances.Instance{
		ID:     2,
		Key:    key,
		Name:   key,
		Status: "stopped",
		Port:   nil,
	}
}

// contractIntPtr returns a pointer to the given int.
func contractIntPtr(v int) *int { return &v }

// newContractProxyServer wires an HTTPProxy to a fake repository
// containing the given instances, hosts it behind an httptest.Server,
// and returns the server. The server is closed via t.Cleanup.
func newContractProxyServer(t *testing.T, magicAuth *auth.MagicAuthService, insts ...instances.Instance) *httptest.Server {
	t.Helper()
	repo := newContractFakeRepo(insts...)
	p := proxy.NewHTTPProxy(proxy.NewTargetResolver(repo), magicAuth, nil)
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv
}

// newContractWSBridgeServer wires a WSBridge to a fake repository
// containing the given instances, hosts it behind an httptest.Server,
// and returns the server along with the bridge and its registry. The
// server is closed via t.Cleanup.
func newContractWSBridgeServer(t *testing.T, magicAuth *auth.MagicAuthService, insts ...instances.Instance) (*httptest.Server, *proxy.WSBridge, *proxy.Registry) {
	t.Helper()
	repo := newContractFakeRepo(insts...)
	registry := proxy.NewRegistry()
	bridge := proxy.NewWSBridge(proxy.NewTargetResolver(repo), magicAuth, registry)
	srv := httptest.NewServer(http.HandlerFunc(bridge.ServeWS))
	t.Cleanup(srv.Close)
	return srv, bridge, registry
}

// newContractAPIServer wires the full httpapi server (with proxy routes
// for /app/{key}/status, /app/{key}/health, /app/{key}/ws, and the
// catch-all proxy) plus optional Basic Auth for management endpoints.
// The server is closed via t.Cleanup.
func newContractAPIServer(t *testing.T, adminUser, adminPass string, magicAuth *auth.MagicAuthService, insts ...instances.Instance) *httptest.Server {
	t.Helper()
	repo := newContractFakeRepo(insts...)
	resolver := proxy.NewTargetResolver(repo)
	httpProxy := proxy.NewHTTPProxy(resolver, magicAuth, nil)
	registry := proxy.NewRegistry()
	wsBridge := proxy.NewWSBridge(resolver, magicAuth, registry)
	handler := httpapi.New(httpapi.Dependencies{
		AdminUsername:  adminUser,
		AdminPassword:  adminPass,
		HTTPProxy:      httpProxy,
		WSBridge:       wsBridge,
		MagicAuth:      magicAuth,
		InstanceLookup: repo,
		AllowedOrigins: []string{"*"},
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// contractProxyURL builds a URL for a proxied request:
// {proxyServer}/app/{key}{subPath}.
func contractProxyURL(proxyServer, key, subPath string) string {
	return proxyServer + "/" + proxy.ProxyPrefix + "/" + key + subPath
}

// contractWSURL converts an http:// URL into a ws:// URL and appends the
// proxy path.
func contractWSURL(proxyServer, key, subPath string) string {
	u := proxyServer
	if strings.HasPrefix(u, "https://") {
		u = "wss://" + u[len("https://"):]
	} else if strings.HasPrefix(u, "http://") {
		u = "ws://" + u[len("http://"):]
	}
	return u + "/" + proxy.ProxyPrefix + "/" + key + subPath
}

// contractNoRedirectClient returns an http.Client that does not follow
// redirects, so redirect responses can be inspected directly.
func contractNoRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// contractDialWS dials the WSBridge as a client and returns the
// connection. The caller is responsible for closing it.
func contractDialWS(t *testing.T, httpURL, key, subPath string, opts *websocket.DialOptions) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, contractWSURL(httpURL, key, subPath), opts)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	return c
}

// contractWSRoundTrip sends a single message and reads a single
// response.
func contractWSRoundTrip(t *testing.T, c *websocket.Conn, msgType websocket.MessageType, payload []byte) (websocket.MessageType, []byte) {
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

// contractWaitRegistryZero polls the registry until Count() reaches zero
// or the timeout elapses. WebSocket teardown is asynchronous.
func contractWaitRegistryZero(t *testing.T, r *proxy.Registry, timeout time.Duration) {
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

// contractDecodeEcho decodes an /echo JSON response body.
func contractDecodeEcho(t *testing.T, body io.Reader) contractEchoResponse {
	t.Helper()
	var got contractEchoResponse
	if err := json.NewDecoder(body).Decode(&got); err != nil {
		t.Fatalf("decode echo response: %v", err)
	}
	return got
}

// =====================================================================
// HTTP proxy contract tests
// =====================================================================

func TestProxyParity_HTTP(t *testing.T) {
	t.Run("method forwarding", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		for _, method := range []string{
			http.MethodGet, http.MethodPost, http.MethodPut,
			http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions,
		} {
			t.Run(method, func(t *testing.T) {
				var body io.Reader
				if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
					body = strings.NewReader("payload")
				}
				req, err := http.NewRequest(method, contractProxyURL(srv.URL, "k", "/echo"), body)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want 200", resp.StatusCode)
				}
				// HEAD and OPTIONS may not have a JSON body.
				if method == http.MethodHead || method == http.MethodOptions {
					return
				}
				got := contractDecodeEcho(t, resp.Body)
				if got.Method != method {
					t.Fatalf("method = %q, want %q", got.Method, method)
				}
			})
		}
	})

	t.Run("path forwarding", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		resp, err := http.Get(contractProxyURL(srv.URL, "k", "/echo"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got := contractDecodeEcho(t, resp.Body)
		// The subpath /echo (after /app/{key}) must be forwarded to the
		// upstream as /echo.
		if got.Path != "/echo" {
			t.Fatalf("path = %q, want /echo", got.Path)
		}
	})

	t.Run("query forwarding", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		resp, err := http.Get(contractProxyURL(srv.URL, "k", "/echo?foo=bar&baz=qux"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got := contractDecodeEcho(t, resp.Body)
		if got.Query["foo"] != "bar" || got.Query["baz"] != "qux" {
			t.Fatalf("query = %v, want foo=bar baz=qux", got.Query)
		}
	})

	t.Run("header forwarding", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		req, err := http.NewRequest(http.MethodGet, contractProxyURL(srv.URL, "k", "/echo"), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Custom", "custom-value")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got := contractDecodeEcho(t, resp.Body)
		// Custom header forwarded.
		if got.Headers["X-Custom"] != "custom-value" {
			t.Fatalf("X-Custom = %q, want custom-value", got.Headers["X-Custom"])
		}
		// X-Forwarded-* must be present.
		if got.Headers["X-Forwarded-Proto"] != "http" {
			t.Fatalf("X-Forwarded-Proto = %q, want http", got.Headers["X-Forwarded-Proto"])
		}
		if got.Headers["X-Forwarded-For"] == "" {
			t.Fatal("X-Forwarded-For not set")
		}
		if got.Headers["X-Forwarded-Host"] == "" {
			t.Fatal("X-Forwarded-Host not set")
		}
		// X-Forwarded-Host is set to the incoming Host header value or
		// "localhost" as a fallback (Go's Header map does not include
		// the Host key, so the proxy defaults to "localhost").
		if got.Headers["X-Forwarded-Host"] != "localhost" {
			t.Fatalf("X-Forwarded-Host = %q, want localhost", got.Headers["X-Forwarded-Host"])
		}
	})

	t.Run("body forwarding", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		// Text body.
		resp, err := http.Post(contractProxyURL(srv.URL, "k", "/echo"), "text/plain", strings.NewReader("hello body"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got := contractDecodeEcho(t, resp.Body)
		if got.Body != "hello body" {
			t.Fatalf("text body = %q, want %q", got.Body, "hello body")
		}

		// JSON body.
		jsonBody := `{"key":"value","n":42}`
		resp2, err := http.Post(contractProxyURL(srv.URL, "k", "/echo"), "application/json", strings.NewReader(jsonBody))
		if err != nil {
			t.Fatal(err)
		}
		defer resp2.Body.Close()
		got2 := contractDecodeEcho(t, resp2.Body)
		if got2.Body != jsonBody {
			t.Fatalf("json body = %q, want %q", got2.Body, jsonBody)
		}

		// Binary body (valid UTF-8 bytes — non-UTF-8 bytes do not
		// survive JSON round-trip through the /echo endpoint).
		binBody := []byte{0x00, 0x01, 0x02, 0x7e, 0x7f, 0x20}
		resp3, err := http.Post(contractProxyURL(srv.URL, "k", "/echo"), "application/octet-stream", bytes.NewReader(binBody))
		if err != nil {
			t.Fatal(err)
		}
		defer resp3.Body.Close()
		got3 := contractDecodeEcho(t, resp3.Body)
		if got3.Body != string(binBody) {
			t.Fatalf("binary body = %q, want %q", got3.Body, string(binBody))
		}
	})

	t.Run("status codes", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		for _, code := range []int{
			http.StatusOK, http.StatusCreated, http.StatusNoContent,
			http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError,
		} {
			t.Run(fmt.Sprintf("%d", code), func(t *testing.T) {
				resp, err := http.Get(contractProxyURL(srv.URL, "k", fmt.Sprintf("/status?code=%d", code)))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != code {
					t.Fatalf("status = %d, want %d", resp.StatusCode, code)
				}
			})
		}
	})

	t.Run("binary payload", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		resp, err := http.Get(contractProxyURL(srv.URL, "k", "/binary/png"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("Content-Type = %q, want image/png", resp.Header.Get("Content-Type"))
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		expected, _ := hex.DecodeString(contractMinimalPNGHex)
		if !bytes.Equal(data, expected) {
			t.Fatalf("binary payload length = %d, want %d", len(data), len(expected))
		}
	})

	t.Run("stream chunks", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		resp, err := http.Get(contractProxyURL(srv.URL, "k", "/stream?chunks=5&delay=20"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			expected := fmt.Sprintf("chunk %d", i)
			if !strings.Contains(string(body), expected) {
				t.Fatalf("body %q does not contain %q", body, expected)
			}
		}
	})

	t.Run("redirects", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)
		client := contractNoRedirectClient()

		// The upstream /redirect endpoint returns a Location pointing at
		// an absolute localhost URL. The proxy must rewrite it to
		// /app/{key}/...
		upstreamBase := fmt.Sprintf("http://localhost:%d/echo", port)
		redirectURL := contractProxyURL(srv.URL, "k", fmt.Sprintf("/redirect?to=%s&code=302", url.QueryEscape(upstreamBase)))

		for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect} {
			t.Run(fmt.Sprintf("%d", code), func(t *testing.T) {
				u := contractProxyURL(srv.URL, "k", fmt.Sprintf("/redirect?to=%s&code=%d", url.QueryEscape(upstreamBase), code))
				resp, err := client.Get(u)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != code {
					t.Fatalf("status = %d, want %d", resp.StatusCode, code)
				}
				loc := resp.Header.Get("Location")
				want := "/" + proxy.ProxyPrefix + "/k/echo"
				if loc != want {
					t.Fatalf("Location = %q, want %q", loc, want)
				}
			})
		}
		// Reference redirectURL to avoid unused-variable lint in case
		// the loop above is refactored.
		_ = redirectURL
	})

	t.Run("cookies", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		resp, err := http.Get(contractProxyURL(srv.URL, "k", "/set-cookie?name=sess&value=abc&path=/"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		cookies := resp.Header.Values("Set-Cookie")
		if len(cookies) == 0 {
			t.Fatal("no Set-Cookie headers returned")
		}
		// The cookie path must be scoped under /app/{key}.
		wantPath := "Path=/app/k"
		found := false
		for _, c := range cookies {
			if strings.Contains(c, wantPath) {
				found = true
			}
			// Domain attribute must be dropped.
			if strings.Contains(strings.ToLower(c), "domain=") {
				t.Fatalf("Set-Cookie %q still contains Domain attribute", c)
			}
		}
		if !found {
			t.Fatalf("no Set-Cookie with Path=/app/k; got %v", cookies)
		}
	})

	t.Run("auth injection", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		// Instance configured with Basic Auth.
		inst := contractRunningInstance("k", port, contractBasicAuthConfig("admin", "secret"))
		magicAuth := auth.NewMagicAuthServiceWithSecret("test-secret")
		srv := newContractProxyServer(t, magicAuth, inst)

		// Create a valid magic admin token + cookie for instance "k".
		token, _ := magicAuth.CreateToken("k", time.Now())
		cookieName := magicAuth.CookieName("k")
		cookieHeader := cookieName + "=" + url.QueryEscape(token)

		req, err := http.NewRequest(http.MethodGet, contractProxyURL(srv.URL, "k", "/echo"), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", cookieHeader)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got := contractDecodeEcho(t, resp.Body)
		// The proxy must inject the instance's Basic Auth header.
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))
		if got.Headers["Authorization"] != expectedAuth {
			t.Fatalf("Authorization = %q, want %q", got.Headers["Authorization"], expectedAuth)
		}
	})

	t.Run("status endpoint", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractAPIServer(t, "", "", nil, inst)

		resp, err := http.Get(contractProxyURL(srv.URL, "k", "/status"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var got map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["instanceKey"] != "k" {
			t.Fatalf("instanceKey = %v, want k", got["instanceKey"])
		}
		if got["status"] != "running" {
			t.Fatalf("status = %v, want running", got["status"])
		}
		if got["proxyPath"] != "app/k" {
			t.Fatalf("proxyPath = %v, want app/k", got["proxyPath"])
		}
		// Port must be present.
		portVal, ok := got["port"].(float64)
		if !ok || int(portVal) != port {
			t.Fatalf("port = %v, want %d", got["port"], port)
		}
	})

	t.Run("health endpoint", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractAPIServer(t, "", "", nil, inst)

		resp, err := http.Get(contractProxyURL(srv.URL, "k", "/health"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var got map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["instanceKey"] != "k" {
			t.Fatalf("instanceKey = %v, want k", got["instanceKey"])
		}
		if got["healthy"] != true {
			t.Fatalf("healthy = %v, want true", got["healthy"])
		}
	})

	t.Run("instance not found", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		resp, err := http.Get(contractProxyURL(srv.URL, "missing", "/echo"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("instance not running", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		running := contractRunningInstance("k", port, "")
		stopped := contractStoppedInstance("stopped")
		srv := newContractProxyServer(t, nil, running, stopped)

		resp, err := http.Get(contractProxyURL(srv.URL, "stopped", "/echo"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", resp.StatusCode)
		}
		var got map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["instanceKey"] != "stopped" {
			t.Fatalf("instanceKey = %v, want stopped", got["instanceKey"])
		}
	})
}

// =====================================================================
// WebSocket proxy contract tests
// =====================================================================

func TestProxyParity_WebSocket(t *testing.T) {
	t.Run("text echo", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv, _, registry := newContractWSBridgeServer(t, nil, inst)

		c := contractDialWS(t, srv.URL, "k", "/ws", nil)
		defer c.CloseNow()

		rt, data := contractWSRoundTrip(t, c, websocket.MessageText, []byte("hello text"))
		if rt != websocket.MessageText {
			t.Fatalf("response type = %v, want MessageText", rt)
		}
		if string(data) != "hello text" {
			t.Fatalf("response = %q, want %q", data, "hello text")
		}

		c.Close(websocket.StatusNormalClosure, "done")
		contractWaitRegistryZero(t, registry, 3*time.Second)
	})

	t.Run("binary echo", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv, _, registry := newContractWSBridgeServer(t, nil, inst)

		c := contractDialWS(t, srv.URL, "k", "/ws", nil)
		defer c.CloseNow()

		payload := []byte{0x00, 0x01, 0x02, 0xff, 0xfe}
		rt, data := contractWSRoundTrip(t, c, websocket.MessageBinary, payload)
		if rt != websocket.MessageBinary {
			t.Fatalf("response type = %v, want MessageBinary", rt)
		}
		if !bytes.Equal(data, payload) {
			t.Fatalf("response = %x, want %x", data, payload)
		}

		c.Close(websocket.StatusNormalClosure, "done")
		contractWaitRegistryZero(t, registry, 3*time.Second)
	})

	t.Run("query forwarding", func(t *testing.T) {
		// Upstream records the request URL so the test can assert the
		// query was forwarded.
		var seenQuery atomic.Value // string
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
			seenQuery.Store(r.URL.RawQuery)
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				return
			}
			defer c.Close(websocket.StatusNormalClosure, "")
			c.SetReadLimit(1 << 20)
			contractEchoLoop(r.Context(), c)
		})
		mux.Handle("/app/k/", http.StripPrefix("/app/k", mux))
		upstream := httptest.NewServer(mux)
		t.Cleanup(upstream.Close)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv, _, registry := newContractWSBridgeServer(t, nil, inst)

		c := contractDialWS(t, srv.URL, "k", "/ws?foo=bar&n=42", nil)
		defer c.CloseNow()

		_, _ = contractWSRoundTrip(t, c, websocket.MessageText, []byte("ping"))

		got := seenQuery.Load()
		if got != "foo=bar&n=42" {
			t.Fatalf("upstream query = %v, want %q", got, "foo=bar&n=42")
		}

		c.Close(websocket.StatusNormalClosure, "done")
		contractWaitRegistryZero(t, registry, 3*time.Second)
	})

	t.Run("close code and reason", func(t *testing.T) {
		// Upstream closes with a specific code/reason; the bridge must
		// forward that close frame to the client.
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				return
			}
			c.Close(websocket.StatusNormalClosure, "normal")
		})
		mux.Handle("/app/k/", http.StripPrefix("/app/k", mux))
		upstream := httptest.NewServer(mux)
		t.Cleanup(upstream.Close)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv, _, registry := newContractWSBridgeServer(t, nil, inst)

		c := contractDialWS(t, srv.URL, "k", "/ws", nil)
		defer c.CloseNow()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _, err := c.Read(ctx)
		if err == nil {
			t.Fatal("expected Read to return an error after upstream close")
		}
		status := websocket.CloseStatus(err)
		if status != websocket.StatusNormalClosure {
			t.Fatalf("close status = %v, want %v (StatusNormalClosure)", status, websocket.StatusNormalClosure)
		}
		var ce websocket.CloseError
		if errors.As(err, &ce) && ce.Reason != "normal" {
			t.Fatalf("close reason = %q, want %q", ce.Reason, "normal")
		}

		contractWaitRegistryZero(t, registry, 3*time.Second)
	})

	t.Run("upstream disconnect", func(t *testing.T) {
		// Upstream closes the connection immediately after the handshake.
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				return
			}
			c.Close(websocket.StatusGoingAway, "restarting")
		})
		mux.Handle("/app/k/", http.StripPrefix("/app/k", mux))
		upstream := httptest.NewServer(mux)
		t.Cleanup(upstream.Close)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv, _, registry := newContractWSBridgeServer(t, nil, inst)

		c := contractDialWS(t, srv.URL, "k", "/ws", nil)
		defer c.CloseNow()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _, err := c.Read(ctx)
		if err == nil {
			t.Fatal("expected Read to return an error after upstream disconnect")
		}

		contractWaitRegistryZero(t, registry, 3*time.Second)
	})

	t.Run("concurrent clients", func(t *testing.T) {
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv, _, registry := newContractWSBridgeServer(t, nil, inst)

		const n = 8
		var wg sync.WaitGroup
		errCh := make(chan error, n)
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func(i int) {
				defer wg.Done()
				c := contractDialWS(t, srv.URL, "k", "/ws", nil)
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

		contractWaitRegistryZero(t, registry, 5*time.Second)
	})
}
