package contract

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/proxy"
)

// This file contains security regression tests that verify the Go proxy
// and auth implementation resist common attack classes: SSRF, header
// injection, path traversal, token tampering/replay, oversized
// payloads, secret leakage in logs, and management endpoint
// authentication.
//
// Tests are organized as subtests of TestSecurityContracts.

// =====================================================================
// Security contract tests
// =====================================================================

func TestSecurityContracts(t *testing.T) {
	t.Run("SSRF payloads", func(t *testing.T) {
		// The TargetResolver constructs the upstream URL from a hardcoded
		// scheme ("http") and host ("localhost:{port}"). Only the path
		// component of the request is forwarded. These subtests verify
		// that malicious request paths cannot redirect the upstream
		// connection to an unintended host.
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		repo := newContractFakeRepo(inst)
		resolver := proxy.NewTargetResolver(repo)

		ssrfPaths := []string{
			"http://evil.com/etc/passwd",
			"https://evil.com/admin",
			"file:///etc/passwd",
			"//evil.com/",
			"http://localhost:9999/admin",
		}
		for _, malicious := range ssrfPaths {
			t.Run(malicious, func(t *testing.T) {
				target, err := resolver.ResolveTarget(context.Background(), "k", malicious)
				if err != nil {
					// Rejection is acceptable.
					return
				}
				// The target host must always be localhost:{port}; the
				// scheme must always be http.
				if target.URL.Scheme != "http" {
					t.Fatalf("scheme = %q, want http (SSRF via scheme)", target.URL.Scheme)
				}
				if target.URL.Host != fmt.Sprintf("localhost:%d", port) {
					t.Fatalf("host = %q, want localhost:%d (SSRF via host)", target.URL.Host, port)
				}
				// The path must NOT contain an authority component.
				if strings.Contains(target.URL.Path, "evil.com") {
					t.Fatalf("path = %q contains evil.com (SSRF)", target.URL.Path)
				}
			})
		}

		// An instance key containing path separators or URL schemes must
		// not resolve to a different target. The key is looked up in the
		// repository; a key with "../" simply won't be found → 404.
		t.Run("malicious instance key", func(t *testing.T) {
			maliciousKeys := []string{
				"../..",
				"http://evil.com",
				"file://etc",
				"localhost:9999",
			}
			for _, key := range maliciousKeys {
				_, err := resolver.ResolveTarget(context.Background(), key, "/echo")
				if err == nil {
					t.Fatalf("malicious key %q unexpectedly resolved", key)
				}
			}
		})
	})

	t.Run("CRLF header injection", func(t *testing.T) {
		// A bare CR embedded in a header value must not split into a
		// separate header line. Go's HTTP stack either rejects the
		// request (400 / connection close) or treats the CR as part of
		// the value — it must NOT create a new "Injected-Header" header
		// that reaches the upstream.
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		u, _ := url.Parse(srv.URL)
		conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Embed a bare CR (not followed by LF) inside the header value.
		rawReq := "GET /app/k/echo HTTP/1.1\r\n" +
			"Host: " + u.Host + "\r\n" +
			"X-Custom: value\rInjected-Header: pwned\r\n" +
			"Connection: close\r\n" +
			"\r\n"
		_, _ = conn.Write([]byte(rawReq))

		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			// A read error is acceptable — the server may close the
			// connection after detecting the invalid header.
			return
		}
		defer resp.Body.Close()

		// If the request was forwarded (200), verify the upstream did
		// NOT see a separate "Injected-Header" header.
		if resp.StatusCode == http.StatusOK {
			var got contractEchoResponse
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatalf("decode echo: %v", err)
			}
			if _, ok := got.Headers["Injected-Header"]; ok {
				t.Fatal("CRLF header injection succeeded: upstream saw a separate Injected-Header")
			}
		}
		// Any non-200 status (400, etc.) is also acceptable — the
		// request was rejected.
	})

	t.Run("path traversal", func(t *testing.T) {
		// A request path with ../ sequences must not escape the proxy
		// prefix to reach unintended paths. Go's url.Parse normalizes
		// dot segments, so /app/k/../../etc/passwd resolves to
		// /etc/passwd — which is NOT under /app/ and thus returns 404
		// from the proxy (instance not found), never reaching the
		// upstream or the local filesystem.
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		traversalPaths := []string{
			"/app/k/../../etc/passwd",
			"/app/k/../../../etc/shadow",
			"/app/k/..%2f..%2fetc/passwd",
		}
		for _, p := range traversalPaths {
			t.Run(p, func(t *testing.T) {
				// Use a raw request to avoid the HTTP client normalizing
				// the path before we can observe the proxy's behaviour.
				u, _ := url.Parse(srv.URL)
				conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				rawReq := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", p, u.Host)
				_, _ = conn.Write([]byte(rawReq))
				resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
				if err != nil {
					t.Fatalf("read response: %v", err)
				}
				defer resp.Body.Close()
				// The proxy must NOT forward the request to the upstream
				// (which would return 200 from /echo). It must return 404
				// (not under proxy prefix) or 400 (invalid path).
				if resp.StatusCode == http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("path traversal %s reached upstream (status 200, body=%q)", p, body)
				}
			})
		}
	})

	t.Run("malformed Basic Auth", func(t *testing.T) {
		// auth.ValidateBasicAuth must handle malformed Authorization
		// headers gracefully (return false, never panic).
		malformed := []string{
			"",
			"Basic",
			"Basic ",
			"Bearer abcdef",
			"Basic !!!notbase64!!!",
			"Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")),
			"Basic " + base64.StdEncoding.EncodeToString([]byte("user")),
			"basic " + base64.StdEncoding.EncodeToString([]byte("admin:password")), // lowercase scheme
		}
		for _, header := range malformed {
			if auth.ValidateBasicAuth(header, "admin", "password") {
				t.Fatalf("malformed header %q unexpectedly validated", header)
			}
		}
		// A well-formed header must still validate.
		valid := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:password"))
		if !auth.ValidateBasicAuth(valid, "admin", "password") {
			t.Fatal("valid header rejected")
		}
	})

	t.Run("token tampering", func(t *testing.T) {
		svc := auth.NewMagicAuthServiceWithSecret("test-secret")
		token, _ := svc.CreateToken("k", time.Now())
		if !svc.ValidateToken(token, "k", time.Now()) {
			t.Fatal("valid token rejected")
		}

		// Tamper with the signature (last character).
		tampered := token[:len(token)-1]
		last := token[len(token)-1]
		if last == 'A' {
			tampered += "B"
		} else {
			tampered += "A"
		}
		if svc.ValidateToken(tampered, "k", time.Now()) {
			t.Fatal("tampered token unexpectedly validated")
		}

		// Tamper with the payload.
		if len(token) > 10 {
			tamperedPayload := token[:5]
			if token[5] == 'A' {
				tamperedPayload += "B"
			} else {
				tamperedPayload += "A"
			}
			tamperedPayload += token[6:]
			if svc.ValidateToken(tamperedPayload, "k", time.Now()) {
				t.Fatal("payload-tampered token unexpectedly validated")
			}
		}

		// Truncated token.
		if svc.ValidateToken(token[:5], "k", time.Now()) {
			t.Fatal("truncated token unexpectedly validated")
		}

		// Token validated with a different secret must fail.
		other := auth.NewMagicAuthServiceWithSecret("different-secret")
		if other.ValidateToken(token, "k", time.Now()) {
			t.Fatal("token validated against a different secret")
		}
	})

	t.Run("token replay across keys", func(t *testing.T) {
		svc := auth.NewMagicAuthServiceWithSecret("test-secret")
		tokenA, _ := svc.CreateToken("instance-a", time.Now())
		tokenB, _ := svc.CreateToken("instance-b", time.Now())

		// Token for key A must not validate for key B.
		if svc.ValidateToken(tokenA, "instance-b", time.Now()) {
			t.Fatal("token for instance-a validated against instance-b")
		}
		// Token for key B must not validate for key A.
		if svc.ValidateToken(tokenB, "instance-a", time.Now()) {
			t.Fatal("token for instance-b validated against instance-a")
		}
		// Each token validates for its own key.
		if !svc.ValidateToken(tokenA, "instance-a", time.Now()) {
			t.Fatal("token for instance-a rejected for instance-a")
		}
		if !svc.ValidateToken(tokenB, "instance-b", time.Now()) {
			t.Fatal("token for instance-b rejected for instance-b")
		}

		// HasValidCookie must also enforce key binding.
		cookieA := svc.CookieName("instance-a") + "=" + url.QueryEscape(tokenA)
		if svc.HasValidCookie(cookieA, "instance-b", time.Now()) {
			t.Fatal("cookie for instance-a validated against instance-b")
		}
	})

	t.Run("oversized payload", func(t *testing.T) {
		// A large request body must be streamed through the proxy
		// without causing an OOM or panic. The proxy uses
		// httputil.ReverseProxy which streams the body.
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractProxyServer(t, nil, inst)

		// 2 MB body of ASCII characters — large enough to verify
		// streaming, small enough to keep the test fast. Using ASCII
		// avoids JSON encoding expansion that would occur with
		// non-UTF-8 bytes echoed through the /echo endpoint.
		size := 2 * 1024 * 1024
		body := bytes.Repeat([]byte("A"), size)
		resp, err := http.Post(contractProxyURL(srv.URL, "k", "/echo"), "application/octet-stream", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		got := contractDecodeEcho(t, resp.Body)
		if len(got.Body) != size {
			t.Fatalf("body length = %d, want %d", len(got.Body), size)
		}
	})

	t.Run("logs without secrets", func(t *testing.T) {
		// Verify that proxy error responses and auth error paths do not
		// leak credentials. The proxy's writeProxyError uses fixed
		// strings; the 503 response includes the instanceKey but never
		// the instance config (which may contain Basic Auth creds).
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		// Instance with Basic Auth credentials in its config.
		inst := contractRunningInstance("k", port, contractBasicAuthConfig("secretuser", "secretpass"))
		srv := newContractProxyServer(t, nil, inst)

		// Request a stopped instance → 503 error. The error body must
		// not contain the credentials from the running instance's
		// config.
		stopped := contractStoppedInstance("stopped")
		stopped.Config = contractBasicAuthConfig("otheruser", "otherpass")
		srv2 := newContractProxyServer(t, nil, inst, stopped)

		resp, err := http.Get(contractProxyURL(srv2.URL, "stopped", "/echo"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		for _, secret := range []string{"secretpass", "otherpass", "secretuser", "otheruser"} {
			if strings.Contains(bodyStr, secret) {
				t.Fatalf("503 error body leaked secret %q: %s", secret, bodyStr)
			}
		}

		// The not-found error body must not leak credentials either.
		resp2, err := http.Get(contractProxyURL(srv.URL, "nonexistent", "/echo"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp2.Body.Close()
		body2, _ := io.ReadAll(resp2.Body)
		bodyStr2 := string(body2)
		for _, secret := range []string{"secretpass", "secretuser"} {
			if strings.Contains(bodyStr2, secret) {
				t.Fatalf("404 error body leaked secret %q: %s", secret, bodyStr2)
			}
		}

		// ValidateBasicAuth must not panic or return secrets. It returns
		// only a bool, so there is no secret leakage path by design.
		_ = auth.ValidateBasicAuth("Basic "+base64.StdEncoding.EncodeToString([]byte("secretuser:secretpass")), "wrong", "wrong")
	})

	t.Run("management endpoints require auth", func(t *testing.T) {
		// /api/instances without credentials must return 401.
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractAPIServer(t, "manager-admin", "manager-pass", nil, inst)

		resp, err := http.Get(srv.URL + "/api/instances")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		// WWW-Authenticate challenge must be present.
		if resp.Header.Get("WWW-Authenticate") == "" {
			t.Fatal("WWW-Authenticate header missing")
		}

		// With valid credentials, the request must NOT return 401. The
		// protected mux has no instance routes wired (deps.Instances is
		// nil), so it returns 404 — but that is past the auth check.
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/instances", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("manager-admin:manager-pass")))
		resp2, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode == http.StatusUnauthorized {
			t.Fatal("valid credentials still returned 401")
		}
	})

	t.Run("proxy routes outside auth", func(t *testing.T) {
		// /app/{key}/ without manager credentials must NOT return 401.
		// Proxy routes are outside the Basic Auth middleware.
		upstream := newContractUpstream(t)
		port := contractUpstreamPort(t, upstream)
		inst := contractRunningInstance("k", port, "")
		srv := newContractAPIServer(t, "manager-admin", "manager-pass", nil, inst)

		// Request the proxy root without credentials.
		resp, err := http.Get(contractProxyURL(srv.URL, "k", "/echo"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			t.Fatal("proxy route returned 401 — proxy routes must be outside manager auth")
		}
		// It should reach the upstream (200) since the instance is
		// running.
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (proxy should forward)", resp.StatusCode)
		}

		// A non-existent instance via the proxy must return 404, not 401.
		resp2, err := http.Get(contractProxyURL(srv.URL, "nonexistent", "/echo"))
		if err != nil {
			t.Fatal(err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode == http.StatusUnauthorized {
			t.Fatal("proxy route for missing instance returned 401 — must be 404")
		}
		if resp2.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp2.StatusCode)
		}
	})
}
