// Package upstream is a deterministic HTTP + WebSocket upstream fixture used by
// proxy tests (Tasks 5, 6, 7, 10). It simulates a GOWA instance backend that the
// Go proxy forwards requests to.
//
// This is a test-only fixture and must not appear in production binaries.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// minimalPNG is a valid 1x1 PNG image (67 bytes).
const minimalPNGHex = "89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000d49444154789c6300010000000500010d0a2db40000000049454e44ae426082"

// minimalPDF is a minimal valid PDF document.
const minimalPDF = `%PDF-1.0
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

func main() {
	port, err := parsePort(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run("127.0.0.1:" + strconv.Itoa(port)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run listens on addr, prints the selected port as JSON to stdout, and serves
// the fixture until interrupted by SIGINT/SIGTERM.
func run(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	if err := json.NewEncoder(os.Stdout).Encode(map[string]int{"port": port}); err != nil {
		return err
	}

	server := &http.Server{Handler: newHandler()}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// newHandler returns the HTTP mux with all fixture endpoints registered.
func newHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/echo", handleEcho)
	mux.HandleFunc("/stream", handleStream)
	mux.HandleFunc("/redirect", handleRedirect)
	mux.HandleFunc("/set-cookie", handleSetCookie)
	mux.HandleFunc("/json-urls", handleJSONURLs)
	mux.HandleFunc("/binary/png", handleBinaryPNG)
	mux.HandleFunc("/binary/pdf", handleBinaryPDF)
	mux.HandleFunc("/large", handleLarge)
	mux.HandleFunc("/delay", handleDelay)
	mux.HandleFunc("/close", handleClose)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ws", handleWSEcho)
	mux.HandleFunc("/ws/disconnect", handleWSDisconnect)

	return mux
}

// echoResponse is the JSON body returned by /echo.
type echoResponse struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func handleEcho(w http.ResponseWriter, r *http.Request) {
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
}

func handleStream(w http.ResponseWriter, r *http.Request) {
	chunks := queryInt(r, "chunks", 5, 1, 1000)
	delay := queryInt(r, "delay", 100, 0, 60000)

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
}

func handleRedirect(w http.ResponseWriter, r *http.Request) {
	to := queryString(r, "to", "/echo")
	code := queryInt(r, "code", http.StatusFound, 300, 399)
	http.Redirect(w, r, to, code)
}

func handleSetCookie(w http.ResponseWriter, r *http.Request) {
	name := queryString(r, "name", "test")
	value := queryString(r, "value", "cookie")
	path := queryString(r, "path", "/")
	maxAge := queryInt(r, "maxAge", 3600, -1, 86400)

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
}

func handleJSONURLs(w http.ResponseWriter, r *http.Request) {
	// Build absolute URLs using the request host. When no port is present
	// (e.g. ":80" default), append the default HTTP port.
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
}

func handleBinaryPNG(w http.ResponseWriter, r *http.Request) {
	data, err := hex.DecodeString(minimalPNGHex)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func handleBinaryPDF(w http.ResponseWriter, r *http.Request) {
	data := []byte(minimalPDF)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func handleLarge(w http.ResponseWriter, r *http.Request) {
	size := queryInt(r, "size", 1024*1024, 1, 10*1024*1024)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(size))
	w.WriteHeader(http.StatusOK)

	// Write repeating data in 4KB chunks to avoid allocating the whole body.
	const chunkSize = 4096
	buf := make([]byte, chunkSize)
	for i := range buf {
		buf[i] = byte(i % 251) // arbitrary prime-modulus pattern
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
}

func handleDelay(w http.ResponseWriter, r *http.Request) {
	ms := queryInt(r, "ms", 1000, 0, 10000)
	time.Sleep(time.Duration(ms) * time.Millisecond)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"delayed": true,
		"ms":      ms,
	})
}

func handleClose(w http.ResponseWriter, r *http.Request) {
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
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func handleWSEcho(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // test fixture: allow any origin
	})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusInternalError, "")

	ctx := r.Context()
	for {
		msgType, data, err := c.Read(ctx)
		if err != nil {
			// Includes client-initiated close frames; just stop echoing.
			return
		}
		if err := c.Write(ctx, msgType, data); err != nil {
			return
		}
	}
}

func handleWSDisconnect(w http.ResponseWriter, r *http.Request) {
	code := queryInt(r, "code", 4000, 1000, 4999)
	reason := queryString(r, "reason", "forced disconnect")

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	c.Close(websocket.StatusCode(code), reason)
}

// parsePort parses --port / -p from args (supports --port=N and --port N forms).
func parsePort(args []string) (int, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--port=") {
			return strconv.Atoi(strings.TrimPrefix(arg, "--port="))
		}
		if arg == "--port" && i+1 < len(args) {
			return strconv.Atoi(args[i+1])
		}
		if arg == "-p" && i+1 < len(args) {
			return strconv.Atoi(args[i+1])
		}
	}
	return 0, fmt.Errorf("missing --port or -p")
}

// queryString returns the query parameter value or the fallback.
func queryString(r *http.Request, key, fallback string) string {
	if v := r.URL.Query().Get(key); v != "" {
		return v
	}
	return fallback
}

// queryInt parses a query parameter as an int, clamped to [min, max], with a
// fallback default when absent or invalid.
func queryInt(r *http.Request, key string, fallback, min, max int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
