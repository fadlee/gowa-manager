package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadyReturns503BeforeReconciliation(t *testing.T) {
	probe := NewReadiness()
	handler := New(Dependencies{Readiness: probe})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status before ready = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestReadyReturns200AfterReconciliation(t *testing.T) {
	probe := NewReadiness()
	probe.SetReady()
	handler := New(Dependencies{Readiness: probe})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status after ready = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != `{"message":"GOWA Manager is ready","success":true}` {
		t.Fatalf("body = %q", got)
	}
}

func TestReadyRouteAbsentWhenProbeNil(t *testing.T) {
	handler := New(Dependencies{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	handler.ServeHTTP(rec, req)

	// Falls through to the /api/ 404 catch-all.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (route should not be registered)", rec.Code, http.StatusNotFound)
	}
}

func TestReadyRejectsNonGet(t *testing.T) {
	probe := NewReadiness()
	handler := New(Dependencies{Readiness: probe})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ready", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestReadyHealthUnchangedByReadiness(t *testing.T) {
	probe := NewReadiness()
	handler := New(Dependencies{Readiness: probe})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != `{"message":"GOWA Manager API is running","success":true}` {
		t.Fatalf("health body = %q", got)
	}
}
