package observability

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// --- SanitizeRoute -------------------------------------------------------

func TestSanitizeRoute(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"health unchanged", "/api/health", "/api/health"},
		{"ready unchanged", "/api/ready", "/api/ready"},
		{"instances root", "/api/instances", "/api/instances"},
		{"numeric instance id collapsed", "/api/instances/123", "/api/instances/{id}"},
		{"numeric instance id with action", "/api/instances/42/start", "/api/instances/{id}/start"},
		{"app key collapsed", "/app/mykey123", "/app/{key}"},
		{"app key with status", "/app/mykey123/status", "/app/{key}/status"},
		{"app key with ws", "/app/mykey123/ws", "/app/{key}/ws"},
		{"app key nested path", "/app/abc-def-123/foo/bar", "/app/{key}/foo/bar"},
		{"system route unchanged", "/api/system/ports", "/api/system/ports"},
		{"root unchanged", "/", "/"},
		{"empty", "", ""},
		{"trailing slash on instances", "/api/instances/", "/api/instances/"},
		{"versions route unchanged", "/api/versions", "/api/versions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeRoute(tc.path)
			if got != tc.want {
				t.Fatalf("SanitizeRoute(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestSanitizeRouteNoRawIDsOrKeys(t *testing.T) {
	// Raw instance keys/IDs must never survive sanitization.
	raw := []string{"mykey123", "42", "abc-def-456"}
	for _, r := range raw {
		got := SanitizeRoute("/app/" + r + "/status")
		if strings.Contains(got, r) {
			t.Fatalf("sanitized route %q still contains raw value %q", got, r)
		}
		got = SanitizeRoute("/api/instances/" + r + "/start")
		if strings.Contains(got, r) {
			t.Fatalf("sanitized route %q still contains raw value %q", got, r)
		}
	}
}

// --- RecordRequest -------------------------------------------------------

func TestRecordRequest(t *testing.T) {
	m := NewMetrics(true)
	m.RecordRequest("/api/health", 1.5)
	m.RecordRequest("/api/health", 2.5)
	m.RecordRequest("/api/instances/{id}", 10)

	out := captureText(t, m)

	if !strings.Contains(out, `gowa_requests_total{route="/api/health"} 2`) {
		t.Fatalf("expected 2 requests for /api/health, got:\n%s", out)
	}
	if !strings.Contains(out, `gowa_requests_total{route="/api/instances/{id}"} 1`) {
		t.Fatalf("expected 1 request for /api/instances/{id}, got:\n%s", out)
	}
	// Average latency: (1.5+2.5)/2 = 2.0
	if !strings.Contains(out, `gowa_request_latency_ms_avg{route="/api/health"} 2`) {
		t.Fatalf("expected avg latency 2 for /api/health, got:\n%s", out)
	}
}

func TestRecordRequestSanitizesRoute(t *testing.T) {
	m := NewMetrics(true)
	m.RecordRequest("/api/instances/999", 1)
	m.RecordRequest("/app/secretkey/ws", 1)

	out := captureText(t, m)
	if strings.Contains(out, "999") {
		t.Fatalf("raw ID leaked into metrics:\n%s", out)
	}
	if strings.Contains(out, "secretkey") {
		t.Fatalf("raw key leaked into metrics:\n%s", out)
	}
	if !strings.Contains(out, `/api/instances/{id}`) {
		t.Fatalf("expected sanitized route, got:\n%s", out)
	}
	if !strings.Contains(out, `/app/{key}/ws`) {
		t.Fatalf("expected sanitized route, got:\n%s", out)
	}
}

// --- Lifecycle / DB / Scheduler counters ---------------------------------

func TestRecordStartFailureAndRestart(t *testing.T) {
	m := NewMetrics(true)
	m.RecordStartFailure()
	m.RecordStartFailure()
	m.RecordStartRestart()

	out := captureText(t, m)
	if !strings.Contains(out, "gowa_start_failures_total 2") {
		t.Fatalf("expected 2 start failures, got:\n%s", out)
	}
	if !strings.Contains(out, "gowa_start_restarts_total 1") {
		t.Fatalf("expected 1 restart, got:\n%s", out)
	}
}

func TestRecordSQLiteBusyAndError(t *testing.T) {
	m := NewMetrics(true)
	m.RecordSQLiteBusy()
	m.RecordSQLiteBusy()
	m.RecordSQLiteError()

	out := captureText(t, m)
	if !strings.Contains(out, "gowa_sqlite_busy_total 2") {
		t.Fatalf("expected 2 sqlite busy, got:\n%s", out)
	}
	if !strings.Contains(out, "gowa_sqlite_errors_total 1") {
		t.Fatalf("expected 1 sqlite error, got:\n%s", out)
	}
}

func TestRecordSchedulerFailure(t *testing.T) {
	m := NewMetrics(true)
	m.RecordSchedulerFailure()
	m.RecordSchedulerFailure()
	m.RecordSchedulerFailure()

	out := captureText(t, m)
	if !strings.Contains(out, "gowa_scheduler_failures_total 3") {
		t.Fatalf("expected 3 scheduler failures, got:\n%s", out)
	}
}

// --- Gauges --------------------------------------------------------------

func TestSetActiveProcesses(t *testing.T) {
	m := NewMetrics(true)
	m.SetActiveProcesses(5)
	out := captureText(t, m)
	if !strings.Contains(out, "gowa_active_processes 5") {
		t.Fatalf("expected 5 active processes, got:\n%s", out)
	}
}

func TestIncDecActiveProcesses(t *testing.T) {
	m := NewMetrics(true)
	m.IncActiveProcesses()
	m.IncActiveProcesses()
	m.DecActiveProcesses()
	out := captureText(t, m)
	if !strings.Contains(out, "gowa_active_processes 1") {
		t.Fatalf("expected 1 active process, got:\n%s", out)
	}
}

func TestIncDecActiveProxyReq(t *testing.T) {
	m := NewMetrics(true)
	m.IncActiveProxyReq()
	m.IncActiveProxyReq()
	m.IncActiveProxyReq()
	m.DecActiveProxyReq()
	out := captureText(t, m)
	if !strings.Contains(out, "gowa_active_proxy_requests 2") {
		t.Fatalf("expected 2 active proxy requests, got:\n%s", out)
	}
}

func TestSetActiveWebSockets(t *testing.T) {
	m := NewMetrics(true)
	m.SetActiveWebSockets(7)
	out := captureText(t, m)
	if !strings.Contains(out, "gowa_active_websockets 7") {
		t.Fatalf("expected 7 active websockets, got:\n%s", out)
	}
}

// --- Runtime -------------------------------------------------------------

func TestRecordRuntime(t *testing.T) {
	m := NewMetrics(true)
	m.RecordRuntime()
	out := captureText(t, m)
	if !strings.Contains(out, "gowa_goroutines ") {
		t.Fatalf("expected goroutines metric, got:\n%s", out)
	}
	if !strings.Contains(out, "gowa_alloc_bytes ") {
		t.Fatalf("expected alloc bytes metric, got:\n%s", out)
	}
	if !strings.Contains(out, "gowa_sys_bytes ") {
		t.Fatalf("expected sys bytes metric, got:\n%s", out)
	}
}

// --- WriteText format ----------------------------------------------------

func TestWriteTextContainsHelpAndType(t *testing.T) {
	m := NewMetrics(true)
	m.RecordRequest("/api/health", 1)
	out := captureText(t, m)
	required := []string{
		"# HELP gowa_requests_total",
		"# TYPE gowa_requests_total counter",
		"# HELP gowa_request_latency_ms_avg",
		"# TYPE gowa_request_latency_ms_avg gauge",
		"# HELP gowa_active_processes",
		"# TYPE gowa_active_processes gauge",
		"# HELP gowa_start_failures_total",
		"# TYPE gowa_start_failures_total counter",
		"# HELP gowa_active_websockets",
		"# TYPE gowa_active_websockets gauge",
		"# HELP gowa_goroutines",
		"# TYPE gowa_goroutines gauge",
		"# HELP gowa_alloc_bytes",
		"# TYPE gowa_alloc_bytes gauge",
	}
	for _, r := range required {
		if !strings.Contains(out, r) {
			t.Fatalf("missing %q in output:\n%s", r, out)
		}
	}
}

// --- Handler -------------------------------------------------------------

func TestHandlerDisabledReturns404(t *testing.T) {
	m := NewMetrics(false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled handler status = %d, want 404", rec.Code)
	}
}

func TestHandlerEnabledLoopbackReturns200(t *testing.T) {
	m := NewMetrics(true)
	m.RecordRequest("/api/health", 1)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled loopback status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "gowa_requests_total") {
		t.Fatalf("expected metrics in body, got:\n%s", body)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "# HELP") {
		t.Fatalf("expected body to start with HELP line, got:\n%s", body)
	}
}

func TestHandlerEnabledIPv6LoopbackReturns200(t *testing.T) {
	m := NewMetrics(true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "[::1]:1234"
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ipv6 loopback status = %d, want 200", rec.Code)
	}
}

func TestHandlerEnabledNonLoopbackReturns403(t *testing.T) {
	m := NewMetrics(true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "192.168.1.5:1234"
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status = %d, want 403", rec.Code)
	}
}

func TestHandlerEnabledNonLoopbackIPv6Returns403(t *testing.T) {
	m := NewMetrics(true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "[2001:db8::1]:1234"
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback ipv6 status = %d, want 403", rec.Code)
	}
}

// --- Security: no credentials/tokens/config ------------------------------

func TestMetricsNoSensitiveData(t *testing.T) {
	m := NewMetrics(true)
	m.RecordRequest("/api/health", 1)
	out := captureText(t, m)
	// No credential-like tokens should ever appear. These are representative
	// sensitive values that must never leak into metrics output.
	sensitive := []string{"password", "token", "secret", "admin", "Bearer", "apikey"}
	for _, s := range sensitive {
		if strings.Contains(strings.ToLower(out), strings.ToLower(s)) {
			t.Fatalf("metrics output contains sensitive term %q:\n%s", s, out)
		}
	}
}

func TestMetricsNoFilePaths(t *testing.T) {
	m := NewMetrics(true)
	out := captureText(t, m)
	// Metrics must not expose filesystem paths (e.g. data dirs).
	if strings.Contains(out, "/data/") || strings.Contains(out, `C:\`) || strings.Contains(out, "./data") {
		t.Fatalf("metrics output contains a file path:\n%s", out)
	}
}

// --- Bounded cardinality -------------------------------------------------

func TestMetricsBoundedCardinality(t *testing.T) {
	m := NewMetrics(true)
	// Record many distinct raw instance IDs/keys; all must collapse.
	for i := 0; i < 100; i++ {
		m.RecordRequest("/api/instances/"+itoa(i), 1)
		m.RecordRequest("/app/key"+itoa(i)+"/ws", 1)
	}
	out := captureText(t, m)
	// There should be exactly one line for /api/instances/{id} and one for /app/{key}/ws.
	countID := strings.Count(out, `gowa_requests_total{route="/api/instances/{id}"}`)
	if countID != 1 {
		t.Fatalf("expected 1 collapsed /api/instances/{id} line, got %d:\n%s", countID, out)
	}
	countKey := strings.Count(out, `gowa_requests_total{route="/app/{key}/ws"}`)
	if countKey != 1 {
		t.Fatalf("expected 1 collapsed /app/{key}/ws line, got %d:\n%s", countKey, out)
	}
}

// --- Concurrent access ---------------------------------------------------

func TestMetricsConcurrentAccess(t *testing.T) {
	m := NewMetrics(true)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.RecordRequest("/api/instances/"+itoa(n), 1)
			m.RecordStartFailure()
			m.IncActiveProcesses()
			m.DecActiveProcesses()
			m.IncActiveProxyReq()
			m.DecActiveProxyReq()
			m.RecordSQLiteBusy()
			m.RecordRuntime()
		}(i)
	}
	// Also hammer WriteText concurrently with recorders.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var sb strings.Builder
			_ = m.WriteText(&sb)
		}()
	}
	wg.Wait()
	// Should not panic or race; verify output is valid.
	out := captureText(t, m)
	if !strings.Contains(out, "gowa_requests_total") {
		t.Fatalf("expected metrics after concurrent access, got:\n%s", out)
	}
}

// --- Enabled -------------------------------------------------------------

func TestEnabled(t *testing.T) {
	if NewMetrics(true).Enabled() != true {
		t.Fatal("NewMetrics(true).Enabled() should be true")
	}
	if NewMetrics(false).Enabled() != false {
		t.Fatal("NewMetrics(false).Enabled() should be false")
	}
}

// --- Helpers -------------------------------------------------------------

func captureText(t *testing.T, m *Metrics) string {
	t.Helper()
	var sb strings.Builder
	if err := m.WriteText(&sb); err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}
	return sb.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Ensure net.ParseIP is referenced (avoids unused import in some build configs).
var _ = net.ParseIP
