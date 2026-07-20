package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
)

// ---- test upstream fixture ----
//
// The upstream fixture (internal/testutil/upstream) lives in package main
// and exposes an unexported newHandler, so it cannot be imported directly.
// Instead we replicate the endpoints the proxy tests need on an
// httptest.Server. This is the standard Go pattern for reverse-proxy
// integration tests and exercises real HTTP behavior end to end.

const (
	minimalPNGHex = "89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000d49444154789c6300010000000500010d0a2db40000000049454e44ae426082"
	minimalPDF    = `%PDF-1.0
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/MediaBox[0 0 612 792]/Parent 2 0 R>>endobj
xref
0 4
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
trailer<</Size 4/Root 1 0 R>>
startxref
190
%%EOF
`
)

// echoResponse is the JSON body returned by the /echo upstream endpoint.
type echoResponse struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// newUpstreamServer starts an httptest.Server that mirrors the subset of
// the upstream fixture endpoints used by the proxy tests. The caller is
// responsible for closing the server (t.Cleanup is registered).
func newUpstreamServer(t *testing.T) *httptest.Server {
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
		_ = json.NewEncoder(w).Encode(echoResponse{
			Method:  r.Method,
			Path:    r.URL.Path,
			Query:   query,
			Headers: headers,
			Body:    string(body),
		})
	})

	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		chunks := queryIntDefault(r, "chunks", 5)
		delay := queryIntDefault(r, "delay", 100)
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
		code := queryIntDefault(r, "code", http.StatusFound)
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
		maxAge := queryIntDefault(r, "maxAge", 3600)
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

	mux.HandleFunc("/json-urls", func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if !strings.Contains(host, ":") {
			host = host + ":80"
		}
		base := "http://" + host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"self":      base + "/json-urls",
			"echo":      base + "/echo",
			"redirect":  base + "/redirect?to=" + base + "/echo",
			"nested":    map[string]string{"deep": base + "/deep/path"},
			"endpoints": []string{base + "/a", base + "/b", base + "/c"},
		})
	})

	mux.HandleFunc("/binary/png", func(w http.ResponseWriter, r *http.Request) {
		data, err := hex.DecodeString(minimalPNGHex)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	})

	mux.HandleFunc("/binary/pdf", func(w http.ResponseWriter, r *http.Request) {
		data := []byte(minimalPDF)
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	})

	mux.HandleFunc("/large", func(w http.ResponseWriter, r *http.Request) {
		size := queryIntDefault(r, "size", 1024*1024)
		if size < 1 {
			size = 1
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusOK)
		const chunkSize = 4096
		buf := make([]byte, chunkSize)
		for i := range buf {
			buf[i] = byte(i % 251)
		}
		remaining := size
		for remaining > 0 {
			n := chunkSize
			if n > remaining {
				n = remaining
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			remaining -= n
		}
	})

	mux.HandleFunc("/delay", func(w http.ResponseWriter, r *http.Request) {
		ms := queryIntDefault(r, "ms", 1000)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"delayed": true,
			"ms":      ms,
		})
	})

	mux.HandleFunc("/close", func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		_ = conn.Close()
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// /status?code=N returns the given status code with an empty body.
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		code := queryIntDefault(r, "code", http.StatusOK)
		w.WriteHeader(code)
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

// queryIntDefault parses a query parameter as an int with a fallback.
func queryIntDefault(r *http.Request, key string, fallback int) int {
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

// ---- proxy test harness ----

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

// basicAuthConfig returns an instance Config JSON string with one basicAuth
// entry (user:pass).
func basicAuthConfig(user, pass string) string {
	return fmt.Sprintf(`{"flags":{"basicAuth":[{"username":%q,"password":%q}]}}`, user, pass)
}

// newRunningInstance builds an instances.Instance whose port points at the
// given upstream server and that is marked running.
func newRunningInstance(key string, port int, config string) instances.Instance {
	return instances.Instance{
		ID:     1,
		Key:    key,
		Status: "running",
		Port:   &port,
		Config: config,
	}
}

// newProxyServer wires an HTTPProxy to a fake repository containing the
// given instances, hosts it behind an httptest.Server, and returns both.
// The proxy server is closed automatically via t.Cleanup.
func newProxyServer(t *testing.T, magicAuth *auth.MagicAuthService, transport http.RoundTripper, insts ...instances.Instance) *httptest.Server {
	t.Helper()
	repo := newFakeRepo(insts...)
	proxy := NewHTTPProxy(NewTargetResolver(repo), magicAuth, transport)
	srv := httptest.NewServer(proxy)
	t.Cleanup(srv.Close)
	return srv
}

// proxyURL builds a URL for a proxied request: {proxyServer}/app/{key}{subPath}.
func proxyURL(proxyServer, key, subPath string) string {
	return proxyServer + "/" + ProxyPrefix + "/" + key + subPath
}

// noRedirectClient returns an http.Client that does not follow redirects.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// fastTimeoutTransport returns an http.Transport with a very short
// ResponseHeaderTimeout, used to exercise upstream timeout handling.
func fastTimeoutTransport() *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 1 * time.Second}).DialContext,
		ResponseHeaderTimeout: 50 * time.Millisecond,
		IdleConnTimeout:       90 * time.Second,
	}
}

// ---- 1. All HTTP methods ----

func TestHTTPProxy_Methods(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodDelete, http.MethodPatch,
	} {
		t.Run(method, func(t *testing.T) {
			var body io.Reader
			if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
				body = strings.NewReader("payload")
			}
			req, err := http.NewRequest(method, proxyURL(srv.URL, "k", "/echo"), body)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var got echoResponse
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Method != method {
				t.Fatalf("method = %q, want %q", got.Method, method)
			}
		})
	}
}

// ---- 2. Query parameter forwarding ----

func TestHTTPProxy_QueryForwarded(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/echo?foo=bar&baz=1"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got echoResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Query["foo"] != "bar" || got.Query["baz"] != "1" {
		t.Fatalf("query = %v, want foo=bar baz=1", got.Query)
	}
}

// ---- 3. Request body forwarding ----

func TestHTTPProxy_BodyForwarded(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	body := []byte("hello body")
	req, err := http.NewRequest(http.MethodPost, proxyURL(srv.URL, "k", "/echo"), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got echoResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Body != "hello body" {
		t.Fatalf("body = %q, want %q", got.Body, "hello body")
	}
}

// ---- 4. Header forwarding ----

func TestHTTPProxy_CustomHeaderForwarded(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	req, err := http.NewRequest(http.MethodGet, proxyURL(srv.URL, "k", "/echo"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Custom", "custom-value")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got echoResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Headers["X-Custom"] != "custom-value" {
		t.Fatalf("X-Custom = %q, want custom-value", got.Headers["X-Custom"])
	}
	// X-Forwarded-* must be present.
	if got.Headers["X-Forwarded-Proto"] != "http" {
		t.Errorf("X-Forwarded-Proto = %q, want http", got.Headers["X-Forwarded-Proto"])
	}
	if got.Headers["X-Forwarded-For"] != "localhost" {
		t.Errorf("X-Forwarded-For = %q, want localhost", got.Headers["X-Forwarded-For"])
	}
}

// ---- 5. Explicit authorization preservation ----

func TestHTTPProxy_ExplicitAuthPreserved(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	// Instance has basicAuth configured, but the client sends its own
	// Authorization header which must win.
	inst := newRunningInstance("k", port, basicAuthConfig("admin", "secret"))
	svc := auth.NewMagicAuthServiceWithSecret("s")
	srv := newProxyServer(t, svc, nil, inst)

	req, err := http.NewRequest(http.MethodGet, proxyURL(srv.URL, "k", "/echo"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer client-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got echoResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Headers["Authorization"] != "Bearer client-token" {
		t.Fatalf("Authorization = %q, want Bearer client-token (client auth must win)", got.Headers["Authorization"])
	}
}

// ---- 6. Instance Basic Auth injection with valid magic cookie ----

func TestHTTPProxy_InstanceAuthInjectedWithValidCookie(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, basicAuthConfig("admin", "secret"))
	svc := auth.NewMagicAuthServiceWithSecret("s")
	srv := newProxyServer(t, svc, nil, inst)

	req, err := http.NewRequest(http.MethodGet, proxyURL(srv.URL, "k", "/echo"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", validMagicCookie(t, svc, "k"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got echoResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := "Basic " + base64Std("admin:secret")
	if got.Headers["Authorization"] != want {
		t.Fatalf("Authorization = %q, want %q (instance auth injected)", got.Headers["Authorization"], want)
	}
}

// ---- 7. Instance Basic Auth injection without magic cookie ----

func TestHTTPProxy_NoAuthInjectionWithoutCookie(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, basicAuthConfig("admin", "secret"))
	svc := auth.NewMagicAuthServiceWithSecret("s")
	srv := newProxyServer(t, svc, nil, inst)

	req, err := http.NewRequest(http.MethodGet, proxyURL(srv.URL, "k", "/echo"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// No cookie, no Authorization.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got echoResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got.Headers["Authorization"]; ok && v != "" {
		t.Fatalf("Authorization = %q, want empty (no cookie → no injection)", v)
	}
}

// ---- 8. Binary responses ----

func TestHTTPProxy_BinaryPNG(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/binary/png"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	wantSig, _ := hex.DecodeString("89504e470d0a1a0a")
	if !bytes.HasPrefix(data, wantSig) {
		t.Fatalf("PNG header = %x, want %x", data[:8], wantSig)
	}
}

func TestHTTPProxy_BinaryPDF(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/binary/pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type = %q, want application/pdf", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("PDF header = %q, want %%PDF- prefix", data[:5])
	}
}

// ---- 9. Streaming first-byte timing ----

func TestHTTPProxy_StreamingFirstByte(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	// 3 chunks with 150ms delay → total ~450ms. The first chunk must
	// arrive well before the last (proving progressive flushing).
	start := time.Now()
	resp, err := http.Get(proxyURL(srv.URL, "k", "/stream?chunks=3&delay=150"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	firstByte := time.Duration(0)
	chunkCount := 0
	for scanner.Scan() {
		if chunkCount == 0 {
			firstByte = time.Since(start)
		}
		chunkCount++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if chunkCount != 3 {
		t.Fatalf("chunks = %d, want 3", chunkCount)
	}
	total := time.Since(start)
	if firstByte >= 300*time.Millisecond {
		t.Fatalf("first byte at %v, want < 300ms (progressive flushing)", firstByte)
	}
	if total < 300*time.Millisecond {
		t.Fatalf("total = %v, want >= ~300ms (chunks delayed)", total)
	}
}

// ---- 10. Large bodies without buffering ----

func TestHTTPProxy_LargeBodyStreamed(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	size := 2 * 1024 * 1024 // 2MB
	resp, err := http.Get(proxyURL(srv.URL, "k", "/large?size="+strconv.Itoa(size)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", ct)
	}
	// Read in chunks and verify data flows incrementally (not buffered).
	buf := make([]byte, 4096)
	total := 0
	reads := 0
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			total += n
			reads++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
	}
	if total != size {
		t.Fatalf("total bytes = %d, want %d", total, size)
	}
	if reads < 100 {
		t.Fatalf("reads = %d, want >= 100 (body should stream in chunks, not one buffer)", reads)
	}
}

// ---- 11. Redirects ----

func TestHTTPProxy_RedirectLocationRewritten(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	client := noRedirectClient()
	resp, err := client.Get(proxyURL(srv.URL, "k", "/redirect?to=/echo&code=307"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	want := "/" + ProxyPrefix + "/k/echo"
	if loc != want {
		t.Fatalf("Location = %q, want %q", loc, want)
	}
}

// ---- 12. Cookies ----

func TestHTTPProxy_SetCookiePathRewritten(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/set-cookie?name=session&value=abc&path=/api&maxAge=60"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != "session" || c.Value != "abc" {
		t.Fatalf("cookie = %s=%s, want session=abc", c.Name, c.Value)
	}
	wantPath := "/" + ProxyPrefix + "/k/api"
	if c.Path != wantPath {
		t.Fatalf("cookie path = %q, want %q", c.Path, wantPath)
	}
}

// ---- 13. Upstream status codes ----

func TestHTTPProxy_StatusCodesForwarded(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	for _, code := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			resp, err := http.Get(proxyURL(srv.URL, "k", "/status?code="+strconv.Itoa(code)))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != code {
				t.Fatalf("status = %d, want %d", resp.StatusCode, code)
			}
		})
	}
}

// ---- 14. Timeout distinctions ----

func TestHTTPProxy_UpstreamResponseTimeout(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	// Use a transport with a 50ms ResponseHeaderTimeout; the upstream
	// delays 500ms before sending headers → 502 from the ErrorHandler.
	srv := newProxyServer(t, nil, fastTimeoutTransport(), inst)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/delay?ms=500"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (upstream timeout)", resp.StatusCode)
	}
}

func TestHTTPProxy_ClientCancellationDoesNotReturn502(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	// Cancel the request before the upstream responds. The proxy should
	// NOT write a 502 (the client is gone); it should just abort.
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL(srv.URL, "k", "/delay?ms=500"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Cancel almost immediately.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err = http.DefaultClient.Do(req)
	if err == nil {
		t.Fatal("expected client error from cancelled request, got nil")
	}
	// The error must be a context cancellation, not a 502 body.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// ---- 15. Cancellation mid-stream ----

func TestHTTPProxy_CancellationMidStream(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL(srv.URL, "k", "/stream?chunks=20&delay=100"), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Read one chunk then cancel.
	scanner := bufio.NewScanner(resp.Body)
	if !scanner.Scan() {
		t.Fatal("expected at least one chunk before cancel")
	}
	cancel()

	// Subsequent reads should error out promptly (context cancelled).
	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not abort after client cancellation (hung)")
	}
}

// ---- 16. Unavailable upstream errors ----

func TestHTTPProxy_InstanceNotFoundReturns404(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	// Repository has "k" but we request "missing".
	srv := newProxyServer(t, nil, nil, inst)

	resp, err := http.Get(proxyURL(srv.URL, "missing", "/echo"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
}

func TestHTTPProxy_InstanceNotRunningReturns503(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	stopped := instances.Instance{Key: "k", Status: "stopped", Port: &port}
	srv := newProxyServer(t, nil, nil, stopped)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/echo"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
	if body["instanceKey"] != "k" {
		t.Fatalf("instanceKey = %v, want k", body["instanceKey"])
	}
}

func TestHTTPProxy_UpstreamConnectionRefusedReturns502(t *testing.T) {
	// Point the instance at a port with no server listening.
	inst := newRunningInstance("k", 1, "") // port 1: nothing listening
	srv := newProxyServer(t, nil, nil, inst)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/echo"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (connection refused)", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
}

// ---- 17. JSON URL stripping ----

func TestHTTPProxy_JSONURLsStripped(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/json-urls"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	// All URL-valued fields must be relative paths, not absolute.
	self, _ := got["self"].(string)
	if strings.HasPrefix(self, "http://") {
		t.Errorf("self = %q, want relative path (stripped)", self)
	}
	if !strings.HasSuffix(self, "/json-urls") {
		t.Errorf("self = %q, want to end with /json-urls", self)
	}
	nested, _ := got["nested"].(map[string]any)
	deep, _ := nested["deep"].(string)
	if strings.HasPrefix(deep, "http://") {
		t.Errorf("nested.deep = %q, want relative path (stripped)", deep)
	}
	endpoints, _ := got["endpoints"].([]any)
	for i, e := range endpoints {
		s, _ := e.(string)
		if strings.HasPrefix(s, "http://") {
			t.Errorf("endpoints[%d] = %q, want relative path (stripped)", i, s)
		}
	}
}

// ---- extra: health passthrough and root path ----

func TestHTTPProxy_HealthPassthrough(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	resp, err := http.Get(proxyURL(srv.URL, "k", "/health"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "ok" {
		t.Fatalf("status = %q, want ok", got["status"])
	}
}

func TestHTTPProxy_RootPathForwarded(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	// /app/k/ → upstream root "/".
	resp, err := http.Get(proxyURL(srv.URL, "k", "/"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// The upstream mux returns 404 for "/" (no handler registered), which
	// proves the root path is forwarded verbatim.
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (upstream has no / handler)", resp.StatusCode)
	}
}

// ---- extra: hop-by-hop headers stripped ----

func TestHTTPProxy_HopByHopHeadersStripped(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	req, err := http.NewRequest(http.MethodGet, proxyURL(srv.URL, "k", "/echo"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Proxy-Authorization", "should-not-forward")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got echoResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got.Headers["Proxy-Authorization"]; ok && v != "" {
		t.Errorf("Proxy-Authorization = %q, want stripped", v)
	}
}

// ---- extra: concurrent requests (goroutine safety) ----

func TestHTTPProxy_ConcurrentRequests(t *testing.T) {
	upstream := newUpstreamServer(t)
	port := upstreamPort(t, upstream)
	inst := newRunningInstance("k", port, "")
	srv := newProxyServer(t, nil, nil, inst)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			resp, err := http.Get(proxyURL(srv.URL, "k", "/echo?i="+strconv.Itoa(i)))
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			var got echoResponse
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				errs <- err
				return
			}
			if got.Query["i"] != strconv.Itoa(i) {
				errs <- fmt.Errorf("i = %q, want %d", got.Query["i"], i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// ---- helpers ----

// base64Std returns the standard base64 encoding of s.
func base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
