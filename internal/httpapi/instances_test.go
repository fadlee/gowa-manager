package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
			life := &fakeLifecycleRoutes{status: InstanceStatus{ID: 1, Name: "test-instance", Status: "running", Port: &port, PID: 4321, Uptime: 10}}
			rec := serveInstanceRequest(newFakeInstanceService(instance), life, nil, http.MethodPost, "/api/instances/1/"+action, nil)
			assertStatus(t, rec, http.StatusOK)
			assertBodyFields(t, rec, map[string]any{"id": float64(1), "status": "running", "pid": float64(4321), "uptime": float64(10)})
		})
	}

	t.Run("status returns injected lifecycle status", func(t *testing.T) {
		life := &fakeLifecycleRoutes{status: InstanceStatus{ID: 1, Name: "test-instance", Status: "running", Port: &port, PID: 4321, Uptime: 10}}
		rec := serveInstanceRequest(newFakeInstanceService(instance), life, nil, http.MethodGet, "/api/instances/1/status", nil)
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"status": "running"})
	})

	t.Run("lifecycle runtime not ready maps to 503", func(t *testing.T) {
		life := &fakeLifecycleRoutes{err: instances.ErrRuntimeNotReady}
		rec := serveInstanceRequest(newFakeInstanceService(instance), life, nil, http.MethodPost, "/api/instances/1/start", nil)
		assertStatus(t, rec, http.StatusServiceUnavailable)
		assertBodyFields(t, rec, map[string]any{"success": false})
	})

	t.Run("admin-link returns plain link without basic auth", func(t *testing.T) {
		rec := serveInstanceRequest(newFakeInstanceService(instance), nil, nil, http.MethodPost, "/api/instances/1/admin-link", nil)
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec, map[string]any{"url": "/app/TESTKEY1/"})
	})

	t.Run("admin-link returns autologin link with expiry when basic auth exists", func(t *testing.T) {
		service := newFakeInstanceService(withInstance(instance, func(i *instances.Instance) {
			i.Config = `{"flags":{"basicAuth":[{"username":"admin","password":"secret"}]}}`
		}))
		rec := serveInstanceRequest(service, nil, nil, http.MethodPost, "/api/instances/1/admin-link", nil)
		assertStatus(t, rec, http.StatusOK)
		body := decodeBody(t, rec)
		url, _ := body["url"].(string)
		if !strings.HasPrefix(url, "/app/TESTKEY1/?autologin=") {
			t.Fatalf("url = %q", url)
		}
		if _, err := time.Parse(time.RFC3339Nano, body["expiresAt"].(string)); err != nil {
			t.Fatalf("expiresAt is not RFC3339: %v", err)
		}
	})

	t.Run("test-connection returns fake connection payload", func(t *testing.T) {
		conn := &fakeConnectionTester{result: instances.ConnectionTestResult{OK: true, Status: 200, Message: "Connection successful. The instance responded to GET /devices.", Body: `{"devices":[]}`}}
		rec := serveInstanceRequest(newFakeInstanceService(withInstance(instance, func(i *instances.Instance) { i.Status = "running" })), nil, nil, http.MethodPost, "/api/instances/1/test-connection", nil, withConnectionTester(conn))
		assertStatus(t, rec, http.StatusOK)
		assertBodyFields(t, rec, map[string]any{"ok": true, "status": float64(200), "body": `{"devices":[]}`})
	})

	t.Run("not found maps to legacy 404 shape", func(t *testing.T) {
		service := newFakeInstanceService(instance)
		service.err = instances.ErrNotFound
		rec := serveInstanceRequest(service, nil, nil, http.MethodGet, "/api/instances/999", nil)
		assertStatus(t, rec, http.StatusNotFound)
		assertJSON(t, rec, map[string]any{"error": "Instance not found", "success": false})
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
