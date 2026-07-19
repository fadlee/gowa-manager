package httpapi

import (
	"net/http"
	"sync/atomic"
)

// ReadinessProbe reports whether startup reconciliation has completed.
// It is used by the /api/ready endpoint so deployment smoke tests can wait
// until the manager has reconciled previously-running instances before
// routing traffic. /api/health remains a simple liveness check and is
// unaffected by readiness state.
type ReadinessProbe interface {
	Ready() bool
}

// AtomicReadiness is a concurrency-safe ReadinessProbe backed by an atomic
// boolean. It is not ready until SetReady is called.
type AtomicReadiness struct {
	ready atomic.Bool
}

// NewReadiness returns a readiness probe that reports not-ready until
// SetReady is invoked.
func NewReadiness() *AtomicReadiness {
	return &AtomicReadiness{}
}

// Ready reports whether reconciliation has finished.
func (r *AtomicReadiness) Ready() bool {
	if r == nil {
		return true
	}
	return r.ready.Load()
}

// SetReady marks the probe as ready. This is called once reconciliation
// completes (whether or not every instance restarted successfully); the
// manager is considered ready to serve traffic once it has finished its
// startup work.
func (r *AtomicReadiness) SetReady() {
	if r == nil {
		return
	}
	r.ready.Store(true)
}

func readyHandler(probe ReadinessProbe) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "Method not allowed"})
			return
		}
		if probe.Ready() {
			writeRawJSON(w, http.StatusOK, []byte(`{"message":"GOWA Manager is ready","success":true}`))
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success": false, "message": "reconciling instances"})
	}
}
