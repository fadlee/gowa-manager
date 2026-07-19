// Package observability provides a safe, opt-in metrics collector and
// HTTP endpoint for Go canary observability.
//
// The metrics endpoint is disabled by default. When enabled it is
// reachable only from loopback addresses (127.0.0.1 / ::1) and exposes a
// stable Prometheus text exposition format. All route labels are
// sanitized via SanitizeRoute so that raw instance IDs/keys never appear
// in the output, keeping label cardinality bounded. No credentials,
// tokens, config values, or filesystem paths are emitted.
package observability

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// latencyTracker accumulates a running average of request latency for a
// single route label. It stores only a count and sum (not every sample)
// to avoid unbounded memory growth.
type latencyTracker struct {
	count int64
	sumMs float64
}

// average returns the running average latency in milliseconds.
func (l *latencyTracker) average() float64 {
	if l.count == 0 {
		return 0
	}
	return l.sumMs / float64(l.count)
}

// Metrics is a thread-safe collector of process, request, and lifecycle
// metrics. The zero value is not ready for use; construct one with
// NewMetrics.
type Metrics struct {
	mu sync.Mutex

	// Counters (by route label — bounded by SanitizeRoute).
	requestCount map[string]int64
	latency      map[string]*latencyTracker

	// Counters (scalar).
	startFailures     int64
	startRestarts     int64
	sqliteBusy        int64
	sqliteErrors      int64
	schedulerFailures int64

	// Gauges.
	activeProcesses  int64
	activeProxyReqs  int64
	activeWebSockets int64

	// Runtime gauges.
	goroutines int64
	allocBytes uint64
	sysBytes   uint64

	enabled bool
}

// NewMetrics returns a ready-to-use Metrics collector. When enabled is
// false the HTTP handler returns 404 and WriteText emits only zero
// values; recording calls are still safe but have no observable effect
// beyond internal state.
func NewMetrics(enabled bool) *Metrics {
	return &Metrics{
		requestCount: make(map[string]int64),
		latency:      make(map[string]*latencyTracker),
		enabled:      enabled,
	}
}

// Enabled reports whether the metrics endpoint is enabled.
func (m *Metrics) Enabled() bool {
	return m != nil && m.enabled
}

// RecordRequest increments the request counter for the given route and
// accumulates the request latency. The route is sanitized via
// SanitizeRoute before being used as a label, ensuring bounded
// cardinality.
func (m *Metrics) RecordRequest(route string, latencyMs float64) {
	if m == nil {
		return
	}
	label := SanitizeRoute(route)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestCount[label]++
	lt := m.latency[label]
	if lt == nil {
		lt = &latencyTracker{}
		m.latency[label] = lt
	}
	lt.count++
	lt.sumMs += latencyMs
}

// RecordStartFailure increments the instance start-failure counter.
func (m *Metrics) RecordStartFailure() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startFailures++
}

// RecordStartRestart increments the instance restart counter.
func (m *Metrics) RecordStartRestart() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startRestarts++
}

// RecordSQLiteBusy increments the SQLite busy-retry counter.
func (m *Metrics) RecordSQLiteBusy() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sqliteBusy++
}

// RecordSQLiteError increments the SQLite error counter.
func (m *Metrics) RecordSQLiteError() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sqliteErrors++
}

// RecordSchedulerFailure increments the scheduler failure counter.
func (m *Metrics) RecordSchedulerFailure() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.schedulerFailures++
}

// SetActiveProcesses sets the running-instance gauge to n.
func (m *Metrics) SetActiveProcesses(n int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeProcesses = n
}

// IncActiveProcesses increments the running-instance gauge by one.
func (m *Metrics) IncActiveProcesses() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeProcesses++
}

// DecActiveProcesses decrements the running-instance gauge by one. It
// never goes below zero.
func (m *Metrics) DecActiveProcesses() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeProcesses > 0 {
		m.activeProcesses--
	}
}

// IncActiveProxyReq increments the active HTTP-proxy-request gauge.
func (m *Metrics) IncActiveProxyReq() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeProxyReqs++
}

// DecActiveProxyReq decrements the active HTTP-proxy-request gauge. It
// never goes below zero.
func (m *Metrics) DecActiveProxyReq() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeProxyReqs > 0 {
		m.activeProxyReqs--
	}
}

// SetActiveWebSockets sets the active WebSocket-connection gauge to n.
// This is intended to be fed from the proxy registry's Count method.
func (m *Metrics) SetActiveWebSockets(n int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeWebSockets = n
}

// RecordRuntime snapshots goroutine count and memory statistics from the
// Go runtime into the corresponding gauges.
func (m *Metrics) RecordRuntime() {
	if m == nil {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goroutines = int64(runtime.NumGoroutine())
	m.allocBytes = ms.Alloc
	m.sysBytes = ms.Sys
}

// WriteText writes all metrics to w in Prometheus text exposition format.
// The output contains only aggregate counters/gauges and sanitized route
// labels — never raw IDs, keys, credentials, or config values.
func (m *Metrics) WriteText(w io.Writer) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	// Snapshot under the lock to avoid partial reads.
	snap := m.snapshot()
	m.mu.Unlock()

	var b strings.Builder

	// Request counters (sorted by route for stable output).
	routes := make([]string, 0, len(snap.requestCount))
	for r := range snap.requestCount {
		routes = append(routes, r)
	}
	sort.Strings(routes)

	b.WriteString("# HELP gowa_requests_total Total HTTP requests by route.\n")
	b.WriteString("# TYPE gowa_requests_total counter\n")
	for _, r := range routes {
		fmt.Fprintf(&b, "gowa_requests_total{route=%q} %d\n", r, snap.requestCount[r])
	}

	b.WriteString("# HELP gowa_request_latency_ms_avg Average request latency in ms by route.\n")
	b.WriteString("# TYPE gowa_request_latency_ms_avg gauge\n")
	for _, r := range routes {
		fmt.Fprintf(&b, "gowa_request_latency_ms_avg{route=%q} %s\n", r, formatFloat(snap.latency[r].average()))
	}

	b.WriteString("# HELP gowa_active_processes Current running GOWA instances.\n")
	b.WriteString("# TYPE gowa_active_processes gauge\n")
	fmt.Fprintf(&b, "gowa_active_processes %d\n", snap.activeProcesses)

	b.WriteString("# HELP gowa_active_proxy_requests Current in-flight HTTP proxy requests.\n")
	b.WriteString("# TYPE gowa_active_proxy_requests gauge\n")
	fmt.Fprintf(&b, "gowa_active_proxy_requests %d\n", snap.activeProxyReqs)

	b.WriteString("# HELP gowa_active_websockets Current WebSocket proxy connections.\n")
	b.WriteString("# TYPE gowa_active_websockets gauge\n")
	fmt.Fprintf(&b, "gowa_active_websockets %d\n", snap.activeWebSockets)

	b.WriteString("# HELP gowa_start_failures_total Total instance start failures.\n")
	b.WriteString("# TYPE gowa_start_failures_total counter\n")
	fmt.Fprintf(&b, "gowa_start_failures_total %d\n", snap.startFailures)

	b.WriteString("# HELP gowa_start_restarts_total Total instance restarts.\n")
	b.WriteString("# TYPE gowa_start_restarts_total counter\n")
	fmt.Fprintf(&b, "gowa_start_restarts_total %d\n", snap.startRestarts)

	b.WriteString("# HELP gowa_sqlite_busy_total Total SQLite busy retries.\n")
	b.WriteString("# TYPE gowa_sqlite_busy_total counter\n")
	fmt.Fprintf(&b, "gowa_sqlite_busy_total %d\n", snap.sqliteBusy)

	b.WriteString("# HELP gowa_sqlite_errors_total Total SQLite errors.\n")
	b.WriteString("# TYPE gowa_sqlite_errors_total counter\n")
	fmt.Fprintf(&b, "gowa_sqlite_errors_total %d\n", snap.sqliteErrors)

	b.WriteString("# HELP gowa_scheduler_failures_total Total scheduler failures.\n")
	b.WriteString("# TYPE gowa_scheduler_failures_total counter\n")
	fmt.Fprintf(&b, "gowa_scheduler_failures_total %d\n", snap.schedulerFailures)

	b.WriteString("# HELP gowa_goroutines Current goroutine count.\n")
	b.WriteString("# TYPE gowa_goroutines gauge\n")
	fmt.Fprintf(&b, "gowa_goroutines %d\n", snap.goroutines)

	b.WriteString("# HELP gowa_alloc_bytes Current heap allocation in bytes.\n")
	b.WriteString("# TYPE gowa_alloc_bytes gauge\n")
	fmt.Fprintf(&b, "gowa_alloc_bytes %d\n", snap.allocBytes)

	b.WriteString("# HELP gowa_sys_bytes Current total system memory obtained from OS in bytes.\n")
	b.WriteString("# TYPE gowa_sys_bytes gauge\n")
	fmt.Fprintf(&b, "gowa_sys_bytes %d\n", snap.sysBytes)

	_, err := w.Write([]byte(b.String()))
	return err
}

// metricsSnapshot is a point-in-time copy of all metric values used to
// render text output without holding the lock during I/O.
type metricsSnapshot struct {
	requestCount      map[string]int64
	latency           map[string]*latencyTracker
	startFailures     int64
	startRestarts     int64
	sqliteBusy        int64
	sqliteErrors      int64
	schedulerFailures int64
	activeProcesses   int64
	activeProxyReqs   int64
	activeWebSockets  int64
	goroutines        int64
	allocBytes        uint64
	sysBytes          uint64
}

func (m *Metrics) snapshot() metricsSnapshot {
	snap := metricsSnapshot{
		requestCount:      make(map[string]int64, len(m.requestCount)),
		latency:           make(map[string]*latencyTracker, len(m.latency)),
		startFailures:     m.startFailures,
		startRestarts:     m.startRestarts,
		sqliteBusy:        m.sqliteBusy,
		sqliteErrors:      m.sqliteErrors,
		schedulerFailures: m.schedulerFailures,
		activeProcesses:   m.activeProcesses,
		activeProxyReqs:   m.activeProxyReqs,
		activeWebSockets:  m.activeWebSockets,
		goroutines:        m.goroutines,
		allocBytes:        m.allocBytes,
		sysBytes:          m.sysBytes,
	}
	for k, v := range m.requestCount {
		snap.requestCount[k] = v
	}
	for k, v := range m.latency {
		cp := *v
		snap.latency[k] = &cp
	}
	return snap
}

// Handler returns an http.HandlerFunc that exposes the metrics endpoint.
// When metrics are disabled it returns 404. When enabled it rejects
// non-loopback remote addresses with 403, refreshes runtime gauges, and
// writes the Prometheus text exposition format.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m == nil || !m.enabled {
			http.NotFound(w, r)
			return
		}
		if !isLoopback(r.RemoteAddr) {
			http.Error(w, "forbidden: metrics endpoint is loopback-only", http.StatusForbidden)
			return
		}
		m.RecordRuntime()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if err := m.WriteText(w); err != nil {
			http.Error(w, "failed to write metrics", http.StatusInternalServerError)
			return
		}
	}
}

// isLoopback reports whether the remote address (host:port form) resolves
// to a loopback IP (127.0.0.1 or ::1).
func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// No port — try parsing the whole string as an IP.
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// formatFloat renders a float in Prometheus exposition style: up to a few
// decimal places, trimming trailing zeros.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// SanitizeRoute collapses variable path segments into bounded
// placeholders so that raw instance IDs/keys never become Prometheus
// labels. Examples:
//
//	/api/instances/123        -> /api/instances/{id}
//	/api/instances/42/start   -> /api/instances/{id}/start
//	/app/mykey/status         -> /app/{key}/status
//	/app/mykey/ws             -> /app/{key}/ws
//	/api/health               -> /api/health (unchanged)
//
// Numeric segments become {id}; non-numeric segments following /app/ or
// /api/instances/ become {key} or {id} respectively. This keeps the set
// of route labels small and bounded.
func SanitizeRoute(path string) string {
	if path == "" {
		return ""
	}
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(segments) == 0 || (len(segments) == 1 && segments[0] == "") {
		return path
	}

	for i, seg := range segments {
		if seg == "" {
			continue
		}
		if isNumeric(seg) {
			segments[i] = "{id}"
			continue
		}
		// Collapse the variable segment after known collection prefixes.
		if i > 0 {
			prev := segments[i-1]
			switch prev {
			case "app":
				segments[i] = "{key}"
			case "instances":
				segments[i] = "{id}"
			}
		}
	}
	return "/" + strings.Join(segments, "/")
}

// isNumeric reports whether s consists solely of ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
