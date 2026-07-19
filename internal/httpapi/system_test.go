package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fadlee/gowa-manager/internal/system"
)

func TestSystemRoutes(t *testing.T) {
	t.Run("status returns legacy nested system status", func(t *testing.T) {
		service := &fakeSystemService{status: system.SystemStatus{TotalInstances: 3, RunningInstances: 1, StoppedInstances: 2, AllocatedPorts: 2, NextAvailablePort: 8002, Uptime: 1234, ManagerVersion: "v0.1.0"}}
		rec := serveSystemRequest(service, nil, nil, http.MethodGet, "/api/system/status", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"status": "running", "uptime": float64(1234), "managerVersion": "v0.1.0", "instances": map[string]any{"total": float64(3), "running": float64(1), "stopped": float64(2)}, "ports": map[string]any{"allocated": float64(2), "next_available": float64(8002)}})
	})

	t.Run("status service error maps to 500", func(t *testing.T) {
		service := &fakeSystemService{err: errors.New("status failed")}
		rec := serveSystemRequest(service, nil, nil, http.MethodGet, "/api/system/status", nil)
		assertStatus(t, rec, http.StatusInternalServerError)
		assertBodyFields(t, rec, map[string]any{"success": false, "error": "status failed"})
	})

	t.Run("next port returns port", func(t *testing.T) {
		allocator := &fakePortAllocator{next: 8123}
		rec := serveSystemRequest(&fakeSystemService{}, allocator, nil, http.MethodGet, "/api/system/ports/next", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"port": float64(8123)})
	})

	t.Run("next port service error maps to 500", func(t *testing.T) {
		allocator := &fakePortAllocator{err: system.ErrNoAvailablePort}
		rec := serveSystemRequest(&fakeSystemService{}, allocator, nil, http.MethodGet, "/api/system/ports/next", nil)
		assertStatus(t, rec, http.StatusInternalServerError)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("config returns explicit legacy fields", func(t *testing.T) {
		service := &fakeSystemService{config: system.SystemConfig{PortRange: system.PortRange{Min: 8000, Max: 9000}, DataDirectory: `D:\\data`, BinariesDirectory: `D:\\data\\binaries`}}
		rec := serveSystemRequest(service, nil, nil, http.MethodGet, "/api/system/config", nil)
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"data_directory": `D:\\data`, "binaries_directory": `D:\\data\\binaries`})
		assertJSON(t, rec, map[string]any{"port_range": map[string]any{"min": float64(8000), "max": float64(9000)}, "data_directory": `D:\\data`, "binaries_directory": `D:\\data\\binaries`})
	})

	t.Run("port availability validates invalid port", func(t *testing.T) {
		rec := serveSystemRequest(&fakeSystemService{}, nil, nil, http.MethodGet, "/api/system/ports/not-a-port/available", nil)
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("port availability returns instance port availability only", func(t *testing.T) {
		checker := &fakePortChecker{available: true}
		rec := serveSystemRequest(&fakeSystemService{}, nil, checker, http.MethodGet, "/api/system/ports/8080/available", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"port": float64(8080), "available": true})
		if checker.port != 8080 {
			t.Fatalf("checked port = %d, want 8080", checker.port)
		}
	})

	t.Run("auto update status uses injected placeholder adapter", func(t *testing.T) {
		auto := &fakeAutoUpdateService{status: map[string]any{"enabled": true, "checking": false}}
		rec := serveSystemRequest(&fakeSystemService{}, nil, nil, http.MethodGet, "/api/system/auto-update/status", nil, withAutoUpdate(auto))
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"enabled": true, "checking": false})
	})

	t.Run("auto update check errors map to 500", func(t *testing.T) {
		auto := &fakeAutoUpdateService{err: errors.New("check failed")}
		rec := serveSystemRequest(&fakeSystemService{}, nil, nil, http.MethodPost, "/api/system/auto-update/check", nil, withAutoUpdate(auto))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertBodyFields(t, rec, map[string]any{"success": false, "error": "check failed"})
	})

	t.Run("auto update instances returns injected payload", func(t *testing.T) {
		auto := &fakeAutoUpdateService{instances: []AutoUpdateInstance{{ID: 1, Name: "alpha", CurrentVersion: "v1.0.0", AvailableVersion: "v1.1.0", UpdateAvailable: true}}}
		rec := serveSystemRequest(&fakeSystemService{}, nil, nil, http.MethodGet, "/api/system/auto-update/instances", nil, withAutoUpdate(auto))
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, []map[string]any{{"id": float64(1), "name": "alpha", "current_version": "v1.0.0", "available_version": "v1.1.0", "update_available": true}})
	})
}

func serveSystemRequest(service *fakeSystemService, allocator *fakePortAllocator, checker *fakePortChecker, method, path string, body *strings.Reader, opts ...func(*Dependencies)) *httptest.ResponseRecorder {
	if body == nil {
		body = strings.NewReader("")
	}
	deps := Dependencies{System: service, PortAllocator: allocator, PortChecker: checker}
	for _, opt := range opts {
		opt(&deps)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, body)
	New(deps).ServeHTTP(rec, req)
	return rec
}

func withAutoUpdate(auto *fakeAutoUpdateService) func(*Dependencies) {
	return func(deps *Dependencies) { deps.AutoUpdate = auto }
}

type fakeSystemService struct {
	status system.SystemStatus
	config system.SystemConfig
	err    error
}

func (s *fakeSystemService) GetSystemStatus(context.Context) (system.SystemStatus, error) {
	return s.status, s.err
}
func (s *fakeSystemService) GetSystemConfig() (system.SystemConfig, error) { return s.config, s.err }

type fakePortAllocator struct {
	next int
	err  error
}

func (a *fakePortAllocator) Next(context.Context) (int, error) { return a.next, a.err }

type fakePortChecker struct {
	port      int
	available bool
}

func (c *fakePortChecker) IsPortAvailable(port int) bool {
	c.port = port
	return c.available
}

type fakeAutoUpdateService struct {
	status    map[string]any
	check     map[string]any
	instances []AutoUpdateInstance
	err       error
}

func (s *fakeAutoUpdateService) Status(context.Context) (map[string]any, error) {
	return s.status, s.err
}
func (s *fakeAutoUpdateService) Check(context.Context) (map[string]any, error) { return s.check, s.err }
func (s *fakeAutoUpdateService) Instances(context.Context) ([]AutoUpdateInstance, error) {
	return s.instances, s.err
}
