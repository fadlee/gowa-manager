package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	handler := New(Dependencies{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"message":"GOWA Manager API is running","success":true}` {
		t.Fatalf("body = %q", got)
	}
}

func TestUnsupportedMethod(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Dependencies{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRecovery(t *testing.T) {
	handler := New(Dependencies{TestPanicRoute: true})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/__panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "goroutine") || strings.Contains(body, "panic") {
		t.Fatalf("response disclosed internals: %s", body)
	}
}

func TestRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Dependencies{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID")
	}
}

func TestCORS(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	New(Dependencies{AllowedOrigins: []string{"http://localhost:5173"}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestAPI404IsJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Dependencies{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("API 404 returned HTML: %s", rec.Body.String())
	}
}
