package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
)

func TestInstanceRoutes(t *testing.T) {
	baseTime := "2026-01-01T00:00:00.000Z"
	port := 19500
	instance := instances.Instance{ID: 1, Key: "TESTKEY1", Name: "test-instance", Port: &port, Status: "stopped", Config: "{}", GOWAVersion: "latest", CreatedAt: baseTime, UpdatedAt: baseTime}

	t.Run("list returns legacy instance array", func(t *testing.T) {
		service := newFakeInstanceService(instance)
		rec := serveInstanceRequest(service, nil, nil, http.MethodGet, "/api/instances/", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, []map[string]any{{
			"id": float64(1), "key": "TESTKEY1", "name": "test-instance", "port": float64(19500), "status": "stopped", "config": "{}", "gowa_version": "latest", "error_message": nil, "created_at": baseTime, "updated_at": baseTime,
		}})
	})

	t.Run("devices returns fake device payload", func(t *testing.T) {
		device := &fakeDeviceClient{response: instances.DevicesResponse{Count: 1, Connected: true, Devices: []map[string]any{{"id": "device-1"}}, Source: "live"}}
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, device, http.MethodGet, "/api/instances/1/devices", nil)
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"count": float64(1), "connected": true, "source": "live"})
	})

	t.Run("detail returns explicit legacy field names", func(t *testing.T) {
		errMsg := "boom"
		service := newFakeInstanceService(withInstance(instance, func(i *instances.Instance) { i.ErrorMessage = &errMsg }))
		rec := serveInstanceRequest(service, nil, nil, http.MethodGet, "/api/instances/1", nil)
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"gowa_version": "latest", "error_message": "boom", "created_at": baseTime, "updated_at": baseTime})
		if strings.Contains(rec.Body.String(), "GOWAVersion") || strings.Contains(rec.Body.String(), "ErrorMessage") {
			t.Fatalf("response leaked Go field names: %s", rec.Body.String())
		}
	})

	t.Run("create passes raw malformed config and returns 201", func(t *testing.T) {
		service := newFakeInstanceService(instance)
		body := `{"name":"created","config":"{bad json","gowa_version":"latest"}`
		rec := serveInstanceRequest(service, nil, nil, http.MethodPost, "/api/instances/", strings.NewReader(body))
		assertStatus(t, rec, http.StatusCreated)
		if service.created.Config == nil || *service.created.Config != "{bad json" {
			t.Fatalf("Create config = %#v, want raw malformed config", service.created.Config)
		}
	})

	t.Run("create accepts empty body like legacy fallback", func(t *testing.T) {
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, nil, http.MethodPost, "/api/instances/", nil)
		assertStatus(t, rec, http.StatusCreated)
	})

	t.Run("create rejects malformed JSON", func(t *testing.T) {
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, nil, http.MethodPost, "/api/instances/", strings.NewReader(`{"name":`))
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("create validates name length", func(t *testing.T) {
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, nil, http.MethodPost, "/api/instances/", strings.NewReader(`{"name":"`+strings.Repeat("x", 101)+`"}`))
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("update validates invalid ID", func(t *testing.T) {
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, nil, http.MethodPut, "/api/instances/not-a-number", strings.NewReader(`{"name":"new"}`))
		assertStatus(t, rec, http.StatusBadRequest)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("update returns updated instance", func(t *testing.T) {
		service := newFakeInstanceService(withInstance(instance, func(i *instances.Instance) { i.Name = "updated" }))
		rec := serveInstanceRequest(service, nil, nil, http.MethodPut, "/api/instances/1", strings.NewReader(`{"name":"updated"}`))
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"name": "updated"})
	})

	t.Run("delete returns legacy success message", func(t *testing.T) {
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, nil, http.MethodDelete, "/api/instances/1", nil)
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"success": true, "message": "Instance deleted successfully"})
	})

	t.Run("reset-data returns legacy success message", func(t *testing.T) {
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, nil, http.MethodPost, "/api/instances/1/reset-data", nil)
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"success": true, "message": "Instance data reset successfully"})
	})

	for _, action := range []string{"start", "stop", "kill", "restart"} {
		t.Run(action+" returns injected lifecycle status", func(t *testing.T) {
			pid := 4321
			life := &fakeLifecycleRoutes{status: InstanceStatus{ID: 1, Name: "test-instance", Status: "running", Port: &port, PID: &pid, Uptime: 10}}
			rec := serveInstanceRequest(newFakeInstanceService(instance), life, nil, http.MethodPost, "/api/instances/1/"+action, nil)
			assertStatus(t, rec, http.StatusOK)
			assertBodyFields(t, rec, map[string]any{"id": float64(1), "status": "running", "pid": float64(4321), "uptime": float64(10)})
		})
	}

	for _, action := range []string{"stop", "kill"} {
		t.Run(action+" stopped response includes null pid", func(t *testing.T) {
			life := &fakeLifecycleRoutes{status: InstanceStatus{ID: 1, Name: "test-instance", Status: "stopped", Port: &port}}
			rec := serveInstanceRequest(newFakeInstanceService(instance), life, nil, http.MethodPost, "/api/instances/1/"+action, nil)
			assertStatus(t, rec, http.StatusOK)
			assertBodyFields(t, rec, map[string]any{"status": "stopped", "pid": nil})
		})
	}

	t.Run("status returns injected lifecycle status", func(t *testing.T) {
		pid := 4321
		life := &fakeLifecycleRoutes{status: InstanceStatus{ID: 1, Name: "test-instance", Status: "running", Port: &port, PID: &pid, Uptime: 10}}
		rec := serveInstanceRequest(newFakeInstanceService(instance), life, nil, http.MethodGet, "/api/instances/1/status", nil)
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"status": "running"})
	})

	t.Run("lifecycle generic error maps to sanitized 500", func(t *testing.T) {
		life := &fakeLifecycleRoutes{err: errors.New("process failed")}
		rec := serveInstanceRequest(newFakeInstanceService(instance), life, nil, http.MethodPost, "/api/instances/1/start", nil)
		assertStatus(t, rec, http.StatusInternalServerError)
		assertJSON(t, rec, map[string]any{"error": "process failed", "success": false})
	})

	t.Run("lifecycle runtime not ready maps to 503", func(t *testing.T) {
		life := &fakeLifecycleRoutes{err: instances.ErrRuntimeNotReady}
		rec := serveInstanceRequest(newFakeInstanceService(instance), life, nil, http.MethodPost, "/api/instances/1/start", nil)
		assertStatus(t, rec, http.StatusServiceUnavailable)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("test-connection returns fake connection payload", func(t *testing.T) {
		conn := &fakeConnectionTester{result: instances.ConnectionTestResult{OK: true, Status: 200, Message: "Connection successful. The instance responded to GET /devices.", Body: `{"devices":[]}`}}
		rec := serveInstanceRequest(newFakeInstanceService(withInstance(instance, func(i *instances.Instance) { i.Status = "running" })), nil, nil, http.MethodPost, "/api/instances/1/test-connection", nil, withConnectionTester(conn))
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"ok": true, "status": float64(200), "body": `{"devices":[]}`})
	})

	t.Run("test-connection uses default tester without lazy handler mutation", func(t *testing.T) {
		h := &instanceHandler{service: newFakeInstanceService(instance)}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/instances/1/test-connection", nil)
		h.testConnection(rec, req, 1)
		assertStatus(t, rec, http.StatusServiceUnavailable)
		if h.connection != nil {
			t.Fatal("testConnection lazily mutated handler connection")
		}
	})

	t.Run("test-connection handles concurrent default tester requests", func(t *testing.T) {
		service := newFakeInstanceService(withInstance(instance, func(i *instances.Instance) { i.Status = "running" }))
		handler := New(Dependencies{Instances: service})
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/api/instances/1/test-connection", nil)
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
				}
			}()
		}
		wg.Wait()
	})

	t.Run("not found maps to legacy 404 shape", func(t *testing.T) {
		service := newFakeInstanceService(instance)
		service.err = instances.ErrNotFound
		rec := serveInstanceRequest(service, nil, nil, http.MethodGet, "/api/instances/999", nil)
		assertStatus(t, rec, http.StatusNotFound)
		assertJSON(t, rec, map[string]any{"error": "Instance not found", "success": false})
	})
}

func TestAdminLink(t *testing.T) {
	baseTime := "2026-01-01T00:00:00.000Z"
	port := 19500
	instance := instances.Instance{ID: 1, Key: "TESTKEY1", Name: "test-instance", Port: &port, Status: "stopped", Config: "{}", GOWAVersion: "latest", CreatedAt: baseTime, UpdatedAt: baseTime}
	basicAuthConfig := `{"flags":{"basicAuth":[{"username":"admin","password":"secret"}]}}`

	t.Run("returns plain link without basic auth and omits expiresAt", func(t *testing.T) {
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, nil, http.MethodPost, "/api/instances/1/admin-link", nil)
		assertStatus(t, rec, http.StatusOK)
		body := decodeBody(t, rec)
		if body["url"] != "/app/TESTKEY1/" {
			t.Fatalf("url = %#v, want /app/TESTKEY1/", body["url"])
		}
		if _, ok := body["expiresAt"]; ok {
			t.Fatalf("unexpected expiresAt in %v", body)
		}
	})

	t.Run("returns 503 for basic auth without issuer", func(t *testing.T) {
		service := newFakeInstanceService(withInstance(instance, func(i *instances.Instance) { i.Config = basicAuthConfig }))
		rec := serveInstanceRequest(service, nil, nil, http.MethodPost, "/api/instances/1/admin-link", nil)
		assertStatus(t, rec, http.StatusServiceUnavailable)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("returns issuer URL and expiry when basic auth exists", func(t *testing.T) {
		expiresAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		issuer := &fakeAdminLinkIssuer{link: AdminLink{URL: "/app/TESTKEY1/?autologin=issued-token", ExpiresAt: &expiresAt}}
		service := newFakeInstanceService(withInstance(instance, func(i *instances.Instance) { i.Config = basicAuthConfig }))
		rec := serveInstanceRequest(service, nil, nil, http.MethodPost, "/api/instances/1/admin-link", nil, withAdminLinkIssuer(issuer))
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"url": "/app/TESTKEY1/?autologin=issued-token", "expiresAt": "2026-01-01T12:00:00Z"})
		if issuer.called != 1 {
			t.Fatalf("issuer called %d times, want 1", issuer.called)
		}
	})

	t.Run("returns 404 for missing instance", func(t *testing.T) {
		service := newFakeInstanceService(instance)
		service.err = instances.ErrNotFound
		rec := serveInstanceRequest(service, nil, nil, http.MethodPost, "/api/instances/999/admin-link", nil)
		assertStatus(t, rec, http.StatusNotFound)
		assertJSON(t, rec, map[string]any{"error": "Instance not found", "success": false})
	})

	t.Run("mints real token via MagicAuthService when basic auth exists", func(t *testing.T) {
		magic := auth.NewMagicAuthServiceWithSecret("test-secret")
		issuer := NewMagicAdminLinkIssuer(magic)
		authedInstance := withInstance(instance, func(i *instances.Instance) { i.Config = basicAuthConfig })
		service := newFakeInstanceService(authedInstance)
		rec := serveInstanceRequest(service, nil, nil, http.MethodPost, "/api/instances/1/admin-link", nil, withAdminLinkIssuer(issuer))
		assertStatus(t, rec, http.StatusOK)

		body := decodeBody(t, rec)
		rawURL, ok := body["url"].(string)
		if !ok {
			t.Fatalf("missing url field in %v", body)
		}
		wantPrefix := "/app/TESTKEY1/?autologin="
		if !strings.HasPrefix(rawURL, wantPrefix) {
			t.Fatalf("url = %q, want prefix %q", rawURL, wantPrefix)
		}
		token := strings.TrimPrefix(rawURL, wantPrefix)
		if token == "" {
			t.Fatalf("autologin token is empty in url %q", rawURL)
		}
		// The token must be URL-decodable and validate against the service.
		decoded, err := url.QueryUnescape(token)
		if err != nil {
			t.Fatalf("autologin token not URL-escaped: %v", err)
		}
		if !magic.ValidateToken(decoded, authedInstance.Key, time.Now()) {
			t.Fatalf("autologin token %q failed validation", decoded)
		}

		expiresAtStr, ok := body["expiresAt"].(string)
		if !ok {
			t.Fatalf("missing expiresAt field in %v", body)
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtStr)
		if err != nil {
			t.Fatalf("expiresAt %q is not RFC3339: %v", expiresAtStr, err)
		}
		// Token lifetime is centralized in auth (60s). The expiry must be
		// roughly 60 seconds in the future and not in the past.
		now := time.Now()
		if expiresAt.Before(now) {
			t.Fatalf("expiresAt %v is in the past", expiresAt)
		}
		if got := expiresAt.Sub(now); got < 30*time.Second || got > 60*time.Second {
			t.Fatalf("expiresAt delta = %v, want ~60s", got)
		}
	})

	t.Run("manager auth protects the admin-link route", func(t *testing.T) {
		service := newFakeInstanceService(instance)
		deps := Dependencies{Instances: service, AdminUsername: "manager", AdminPassword: "secret"}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/instances/1/admin-link", nil)
		New(deps).ServeHTTP(rec, req)
		assertStatus(t, rec, http.StatusUnauthorized)
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("expected WWW-Authenticate challenge header")
		}
		assertBodyFields(t, rec, map[string]any{"success": false, "error": "Unauthorized"})
	})

	t.Run("manager auth allows admin-link with valid credentials", func(t *testing.T) {
		service := newFakeInstanceService(instance)
		deps := Dependencies{Instances: service, AdminUsername: "manager", AdminPassword: "secret"}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/instances/1/admin-link", nil)
		req.SetBasicAuth("manager", "secret")
		New(deps).ServeHTTP(rec, req)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"url": "/app/TESTKEY1/"})
	})
}

func serveInstanceRequest(service *fakeInstanceService, lifecycle *fakeLifecycleRoutes, device *fakeDeviceClient, method, path string, body *strings.Reader, opts ...func(*Dependencies)) *httptest.ResponseRecorder {
	if body == nil {
		body = strings.NewReader("")
	}
	deps := Dependencies{Instances: service, InstanceLifecycle: lifecycle, DeviceClient: device}
	for _, opt := range opts {
		opt(&deps)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, body)
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		req.Header.Set("Content-Type", "application/json")
	}
	New(deps).ServeHTTP(rec, req)
	return rec
}

func withConnectionTester(tester *fakeConnectionTester) func(*Dependencies) {
	return func(deps *Dependencies) { deps.ConnectionTester = tester }
}

func withAdminLinkIssuer(issuer AdminLinkIssuer) func(*Dependencies) {
	return func(deps *Dependencies) { deps.AdminLinkIssuer = issuer }
}

type fakeInstanceService struct {
	instance instances.Instance
	err      error
	created  instances.CreateRequest
	updated  instances.UpdateRequest
}

func newFakeInstanceService(instance instances.Instance) *fakeInstanceService {
	return &fakeInstanceService{instance: instance}
}

func (s *fakeInstanceService) List(context.Context) ([]instances.Instance, error) {
	return []instances.Instance{s.instance}, s.err
}
func (s *fakeInstanceService) Get(context.Context, int64) (instances.Instance, error) {
	return s.instance, s.err
}
func (s *fakeInstanceService) Create(_ context.Context, request instances.CreateRequest) (instances.Instance, error) {
	s.created = request
	return s.instance, s.err
}
func (s *fakeInstanceService) Update(_ context.Context, _ int64, request instances.UpdateRequest) (instances.Instance, error) {
	s.updated = request
	return s.instance, s.err
}
func (s *fakeInstanceService) Delete(context.Context, int64) error    { return s.err }
func (s *fakeInstanceService) ResetData(context.Context, int64) error { return s.err }

type fakeLifecycleRoutes struct {
	status InstanceStatus
	err    error
}

func (l *fakeLifecycleRoutes) Start(context.Context, int64) (InstanceStatus, error) {
	return l.status, l.err
}
func (l *fakeLifecycleRoutes) Stop(context.Context, int64) (InstanceStatus, error) {
	return l.status, l.err
}
func (l *fakeLifecycleRoutes) Kill(context.Context, int64) (InstanceStatus, error) {
	return l.status, l.err
}
func (l *fakeLifecycleRoutes) Restart(context.Context, int64) (InstanceStatus, error) {
	return l.status, l.err
}
func (l *fakeLifecycleRoutes) Status(context.Context, int64) (InstanceStatus, error) {
	return l.status, l.err
}

type fakeDeviceClient struct {
	response instances.DevicesResponse
	err      error
}

func (c *fakeDeviceClient) Fetch(context.Context, instances.Instance) (instances.DevicesResponse, error) {
	return c.response, c.err
}

type fakeConnectionTester struct {
	result instances.ConnectionTestResult
}

func (t *fakeConnectionTester) Test(context.Context, instances.Instance) instances.ConnectionTestResult {
	return t.result
}

type fakeAdminLinkIssuer struct {
	link   AdminLink
	err    error
	called int
}

func (i *fakeAdminLinkIssuer) CreateAdminLink(context.Context, instances.Instance) (AdminLink, error) {
	i.called++
	return i.link, i.err
}

func withInstance(instance instances.Instance, mutate func(*instances.Instance)) instances.Instance {
	mutate(&instance)
	return instance
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, want, rec.Body.String())
	}
}

func assertJSON(t *testing.T, rec *httptest.ResponseRecorder, want any) {
	t.Helper()
	var got any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	wantBytes, _ := json.Marshal(want)
	var normalizedWant any
	_ = json.Unmarshal(wantBytes, &normalizedWant)
	gotBytes, _ := json.Marshal(got)
	normalizedWantBytes, _ := json.Marshal(normalizedWant)
	if string(gotBytes) != string(normalizedWantBytes) {
		t.Fatalf("body = %s, want %s", gotBytes, normalizedWantBytes)
	}
}

func assertBodyFields(t *testing.T, rec *httptest.ResponseRecorder, fields map[string]any) {
	t.Helper()
	body := decodeBody(t, rec)
	for key, want := range fields {
		got, ok := body[key]
		if !ok {
			t.Fatalf("missing field %q in %v", key, body)
		}
		if got != want {
			t.Fatalf("field %q = %#v, want %#v in %v", key, got, want, body)
		}
	}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return body
}
