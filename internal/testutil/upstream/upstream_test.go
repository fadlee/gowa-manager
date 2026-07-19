package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// newTestServer starts an httptest.Server backed by the fixture handler and
// returns the server along with a cleanup function.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newHandler())
	t.Cleanup(srv.Close)
	return srv
}

func TestEcho(t *testing.T) {
	srv := newTestServer(t)

	body := []byte("hello world")
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/echo?foo=bar&baz=qux", bytes.NewReader(body))
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

	if got.Method != http.MethodPost {
		t.Errorf("method = %q, want %q", got.Method, http.MethodPost)
	}
	if got.Path != "/echo" {
		t.Errorf("path = %q, want /echo", got.Path)
	}
	if got.Query["foo"] != "bar" || got.Query["baz"] != "qux" {
		t.Errorf("query = %v, want foo=bar baz=qux", got.Query)
	}
	if got.Headers["X-Custom"] != "custom-value" {
		t.Errorf("headers = %v, want X-Custom=custom-value", got.Headers)
	}
	if got.Body != "hello world" {
		t.Errorf("body = %q, want %q", got.Body, "hello world")
	}
}

func TestEchoMethods(t *testing.T) {
	srv := newTestServer(t)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req, _ := http.NewRequest(method, srv.URL+"/echo", nil)
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
				t.Errorf("method = %q, want %q", got.Method, method)
			}
		})
	}
}

func TestStream(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/stream?chunks=3&delay=10")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	want := "chunk 0\nchunk 1\nchunk 2\n"
	if string(data) != want {
		t.Errorf("stream body = %q, want %q", data, want)
	}
}

func TestRedirect(t *testing.T) {
	srv := newTestServer(t)

	// Use a client that does not follow redirects so we can inspect the 307.
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/redirect?to=/echo&code=307")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want 307", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/echo" {
		t.Errorf("Location = %q, want /echo", loc)
	}
}

func TestRedirectDefault(t *testing.T) {
	srv := newTestServer(t)

	// Use a client that does not follow redirects so we can inspect the 302.
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/redirect")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/echo" {
		t.Errorf("Location = %q, want /echo", loc)
	}
}

func TestSetCookie(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/set-cookie?name=session&value=abc&path=/api&maxAge=60")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != "session" {
		t.Errorf("cookie name = %q, want session", c.Name)
	}
	if c.Value != "abc" {
		t.Errorf("cookie value = %q, want abc", c.Value)
	}
	if c.Path != "/api" {
		t.Errorf("cookie path = %q, want /api", c.Path)
	}
	if c.MaxAge != 60 {
		t.Errorf("cookie maxAge = %d, want 60", c.MaxAge)
	}
}

func TestJSONURLs(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/json-urls")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	// The response must contain absolute URLs beginning with http://.
	self, _ := got["self"].(string)
	if !strings.HasPrefix(self, "http://") {
		t.Errorf("self = %q, want absolute URL", self)
	}
	if !strings.HasSuffix(self, "/json-urls") {
		t.Errorf("self = %q, want to end with /json-urls", self)
	}

	nested, _ := got["nested"].(map[string]any)
	deep, _ := nested["deep"].(string)
	if !strings.HasPrefix(deep, "http://") {
		t.Errorf("nested.deep = %q, want absolute URL", deep)
	}

	endpoints, _ := got["endpoints"].([]any)
	if len(endpoints) != 3 {
		t.Fatalf("endpoints len = %d, want 3", len(endpoints))
	}
	for i, e := range endpoints {
		s, _ := e.(string)
		if !strings.HasPrefix(s, "http://") {
			t.Errorf("endpoints[%d] = %q, want absolute URL", i, s)
		}
	}
}

func TestBinaryPNG(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/binary/png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	// PNG signature: 89 50 4E 47 0D 0A 1A 0A
	wantSig, _ := hex.DecodeString("89504e470d0a1a0a")
	if !bytes.HasPrefix(data, wantSig) {
		t.Errorf("PNG header = %x, want %x", data[:8], wantSig)
	}
}

func TestBinaryPDF(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/binary/pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Errorf("PDF header = %q, want %%PDF- prefix", data[:5])
	}
}

func TestLarge(t *testing.T) {
	srv := newTestServer(t)

	size := 100 * 1024 // 100KB to keep the test fast
	resp, err := http.Get(srv.URL + "/large?size=" + strconv.Itoa(size))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if int(n) != size {
		t.Errorf("body size = %d, want %d", n, size)
	}
}

func TestDelay(t *testing.T) {
	srv := newTestServer(t)

	start := time.Now()
	resp, err := http.Get(srv.URL + "/delay?ms=100")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if elapsed < 90*time.Millisecond {
		t.Errorf("elapsed = %v, want >= ~100ms", elapsed)
	}

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if delayed, _ := got["delayed"].(bool); !delayed {
		t.Errorf("delayed = %v, want true", delayed)
	}
	if ms, _ := got["ms"].(float64); int(ms) != 100 {
		t.Errorf("ms = %v, want 100", ms)
	}
}

func TestClose(t *testing.T) {
	srv := newTestServer(t)

	// /close hijacks and closes the connection without a response. The HTTP
	// client should observe an error or an empty/EOF response.
	resp, err := http.Get(srv.URL + "/close")
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		// A closed connection may yield an empty body or a transport error;
		// either way the response must not contain meaningful content.
		if resp.StatusCode == http.StatusOK && len(body) > 0 {
			t.Errorf("expected closed connection, got status %d body %q", resp.StatusCode, body)
		}
	}
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %q, want ok", got["status"])
	}
}

func TestWSEchoText(t *testing.T) {
	srv := newTestServer(t)
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	msg := []byte("hello websocket")
	if err := c.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}
	gotType, got, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotType != websocket.MessageText {
		t.Errorf("msg type = %v, want text", gotType)
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("echo = %q, want %q", got, msg)
	}
}

func TestWSEchoBinary(t *testing.T) {
	srv := newTestServer(t)
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	msg := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	if err := c.Write(ctx, websocket.MessageBinary, msg); err != nil {
		t.Fatal(err)
	}
	gotType, got, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotType != websocket.MessageBinary {
		t.Errorf("msg type = %v, want binary", gotType)
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("echo = %x, want %x", got, msg)
	}
}

func TestWSDisconnect(t *testing.T) {
	srv := newTestServer(t)
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws/disconnect?code=4042&reason=bye"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	_, _, err = c.Read(ctx)
	if err == nil {
		t.Fatal("expected close error, got nil")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusCode(4042) {
		t.Errorf("close status = %v, want 4042", got)
	}
}
