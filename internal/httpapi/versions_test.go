package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/versions"
)

func TestVersionRoutes(t *testing.T) {
	installedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	versionInfo := versions.VersionInfo{Version: "v1.2.3", Path: `/tmp/gowa`, Installed: true, IsLatest: true, Size: 42, InstalledAt: installedAt}

	t.Run("installed returns legacy version fields with installedAt", func(t *testing.T) {
		service := &fakeVersionService{installed: []versions.VersionInfo{versionInfo}}
		rec := serveVersionRequest(service, nil, http.MethodGet, "/api/system/versions/installed", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, []map[string]any{{"version": "v1.2.3", "path": `/tmp/gowa`, "installed": true, "isLatest": true, "size": float64(42), "installedAt": "2026-01-02T03:04:05Z"}})
	})

	t.Run("installed service error maps to 500", func(t *testing.T) {
		service := &fakeVersionService{err: errors.New("list failed")}
		rec := serveVersionRequest(service, nil, http.MethodGet, "/api/system/versions/installed", nil)
		assertStatus(t, rec, http.StatusInternalServerError)
		assertBodyFields(t, rec, map[string]any{"success": false, "error": "list failed"})
	})

	t.Run("available defaults limit to 10", func(t *testing.T) {
		service := &fakeVersionService{available: []versions.VersionInfo{versionInfo}}
		rec := serveVersionRequest(service, nil, http.MethodGet, "/api/system/versions/available", nil)
		assertStatus(t, rec, http.StatusOK)
		if service.availableLimit != 10 {
			t.Fatalf("limit = %d, want 10", service.availableLimit)
		}
	})

	t.Run("available accepts custom limit", func(t *testing.T) {
		service := &fakeVersionService{}
		rec := serveVersionRequest(service, nil, http.MethodGet, "/api/system/versions/available?limit=5", nil)
		assertStatus(t, rec, http.StatusOK)
		if service.availableLimit != 5 {
			t.Fatalf("limit = %d, want 5", service.availableLimit)
		}
	})

	t.Run("available rejects invalid limit", func(t *testing.T) {
		rec := serveVersionRequest(&fakeVersionService{}, nil, http.MethodGet, "/api/system/versions/available?limit=bad", nil)
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("install requires version", func(t *testing.T) {
		rec := serveVersionRequest(&fakeVersionService{}, &fakeVersionInstaller{}, http.MethodPost, "/api/system/versions/install", strings.NewReader(`{}`))
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("install returns legacy success envelope only", func(t *testing.T) {
		installer := &fakeVersionInstaller{result: versions.InstallResult{Version: "v1.2.3", Path: `/tmp/gowa`, SHA256: "abc", Size: 42}}
		rec := serveVersionRequest(&fakeVersionService{}, installer, http.MethodPost, "/api/system/versions/install", strings.NewReader(`{"version":"v1.2.3"}`))
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"success": true, "message": "Successfully installed GOWA version v1.2.3"})
		if installer.version != "v1.2.3" {
			t.Fatalf("installed version = %q, want v1.2.3", installer.version)
		}
	})

	t.Run("delete rejects latest", func(t *testing.T) {
		rec := serveVersionRequest(&fakeVersionService{}, nil, http.MethodDelete, "/api/system/versions/latest", nil)
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("delete returns success envelope", func(t *testing.T) {
		service := &fakeVersionService{}
		rec := serveVersionRequest(service, nil, http.MethodDelete, "/api/system/versions/v1.2.3", nil)
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"success": true, "message": "Successfully removed GOWA version v1.2.3"})
		if service.removed != "v1.2.3" {
			t.Fatalf("removed = %q, want v1.2.3", service.removed)
		}
	})

	t.Run("delete conflict maps to 400", func(t *testing.T) {
		service := &fakeVersionService{err: ErrVersionConflict}
		rec := serveVersionRequest(service, nil, http.MethodDelete, "/api/system/versions/v1.2.3", nil)
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("version available returns available flag with path", func(t *testing.T) {
		service := &fakeVersionService{installed: []versions.VersionInfo{versionInfo}, isAvailable: true}
		rec := serveVersionRequest(service, nil, http.MethodGet, "/api/system/versions/v1.2.3/available", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"version": "v1.2.3", "available": true, "path": `/tmp/gowa`})
	})

	t.Run("usage returns versions size map", func(t *testing.T) {
		service := &fakeVersionService{sizes: map[string]int64{"v1.2.3": 42}}
		rec := serveVersionRequest(service, nil, http.MethodGet, "/api/system/versions/usage", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"v1.2.3": float64(42)})
	})

	t.Run("cleanup defaults keepCount to 3 with optional body", func(t *testing.T) {
		service := &fakeVersionService{cleaned: []string{"v1.0.0"}}
		rec := serveVersionRequest(service, nil, http.MethodPost, "/api/system/versions/cleanup", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"success": true, "message": "Cleaned up 1 old versions: v1.0.0", "removed": []any{"v1.0.0"}})
		if service.keepCount != 3 {
			t.Fatalf("keepCount = %d, want 3", service.keepCount)
		}
	})

	t.Run("cleanup accepts keepCount body", func(t *testing.T) {
		service := &fakeVersionService{}
		rec := serveVersionRequest(service, nil, http.MethodPost, "/api/system/versions/cleanup", strings.NewReader(`{"keepCount":1}`))
		assertStatus(t, rec, http.StatusOK)
		if service.keepCount != 1 {
			t.Fatalf("keepCount = %d, want 1", service.keepCount)
		}
	})

	t.Run("cleanup returns legacy message for empty removed list", func(t *testing.T) {
		service := &fakeVersionService{}
		rec := serveVersionRequest(service, nil, http.MethodPost, "/api/system/versions/cleanup", strings.NewReader(`{"keepCount":1}`))
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"success": true, "message": "Cleaned up 0 old versions: ", "removed": []any{}})
	})

	t.Run("cleanup rejects invalid keepCount", func(t *testing.T) {
		rec := serveVersionRequest(&fakeVersionService{}, nil, http.MethodPost, "/api/system/versions/cleanup", strings.NewReader(`{"keepCount":0}`))
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})
}

func serveVersionRequest(service *fakeVersionService, installer *fakeVersionInstaller, method, path string, body *strings.Reader) *httptest.ResponseRecorder {
	if body == nil {
		body = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, body)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	New(Dependencies{Versions: service, VersionInstaller: installer}).ServeHTTP(rec, req)
	return rec
}

type fakeVersionService struct {
	installed      []versions.VersionInfo
	available      []versions.VersionInfo
	availableLimit int
	isAvailable    bool
	sizes          map[string]int64
	cleaned        []string
	keepCount      int
	removed        string
	err            error
}

func (s *fakeVersionService) GetInstalledVersions() ([]versions.VersionInfo, error) {
	return s.installed, s.err
}
func (s *fakeVersionService) GetAvailableVersions(_ context.Context, limit int) ([]versions.VersionInfo, error) {
	s.availableLimit = limit
	return s.available, s.err
}
func (s *fakeVersionService) IsVersionAvailable(context.Context, string) (bool, error) {
	return s.isAvailable, s.err
}
func (s *fakeVersionService) GetVersionsSize() (map[string]int64, error) { return s.sizes, s.err }
func (s *fakeVersionService) RemoveVersion(_ context.Context, version string) error {
	s.removed = version
	return s.err
}
func (s *fakeVersionService) Cleanup(_ context.Context, keepCount int) ([]string, error) {
	s.keepCount = keepCount
	return s.cleaned, s.err
}

type fakeVersionInstaller struct {
	result  versions.InstallResult
	version string
	err     error
}

func (i *fakeVersionInstaller) Install(_ context.Context, version string) (versions.InstallResult, error) {
	i.version = version
	return i.result, i.err
}
