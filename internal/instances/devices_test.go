package instances

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestDeviceClientFetchesLiveDevicesAndSummaryFields(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0).UTC()}
	var calls atomic.Int32
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q, want application/json", r.Header.Get("Accept"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":"one"},{"id":"two"}]`))
	})
	defer server.Close()

	client := NewDeviceClient(DeviceClientOptions{HTTPClient: server.Client(), Clock: clock})
	got, err := client.Summary(context.Background(), runningInstance(server, `{"flags":{}}`))
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if got.Count != 2 || !got.Connected || got.Stale || got.Source != "live" || got.Error != "" || !got.FetchedAt.Equal(clock.now) {
		t.Fatalf("Summary() = %#v", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestDeviceClientUsesFreshCacheAndCanClearIt(t *testing.T) {
	clock := &fakeClock{now: time.Unix(200, 0).UTC()}
	var calls atomic.Int32
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"devices":[{"id":"cached"}]}`))
	})
	defer server.Close()
	client := NewDeviceClient(DeviceClientOptions{HTTPClient: server.Client(), Clock: clock, CacheTTL: time.Minute})
	instance := runningInstance(server, `{}`)

	first, err := client.Fetch(context.Background(), instance)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	second, err := client.Fetch(context.Background(), instance)
	if err != nil {
		t.Fatalf("Fetch() cached error = %v", err)
	}
	if second.Source != "cache" || second.Stale || second.Count != first.Count || calls.Load() != 1 {
		t.Fatalf("cached Fetch() = %#v calls=%d", second, calls.Load())
	}
	client.ClearCache(instance.ID)
	if _, err := client.Fetch(context.Background(), instance); err != nil {
		t.Fatalf("Fetch() after ClearCache error = %v", err)
	}
	client.ClearAllCache()
	if _, err := client.Fetch(context.Background(), instance); err != nil {
		t.Fatalf("Fetch() after ClearAllCache error = %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls after clears = %d, want 3", calls.Load())
	}
}

func TestDeviceClientReturnsStaleCacheOnRefreshFailure(t *testing.T) {
	clock := &fakeClock{now: time.Unix(300, 0).UTC()}
	fail := false
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"ok"}]}`))
	})
	defer server.Close()
	client := NewDeviceClient(DeviceClientOptions{HTTPClient: server.Client(), Clock: clock, CacheTTL: time.Millisecond})
	instance := runningInstance(server, `{}`)
	if _, err := client.Fetch(context.Background(), instance); err != nil {
		t.Fatalf("seed Fetch() error = %v", err)
	}
	clock.Advance(time.Second)
	fail = true

	got, err := client.Fetch(context.Background(), instance)
	if err == nil || got.Source != "cache" || !got.Stale || got.Error == "" || got.Count != 1 {
		t.Fatalf("stale Fetch() = %#v err=%v", got, err)
	}
}

func TestDeviceClientNonRunningDoesNotFetch(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected fetch") })
	defer server.Close()
	port := serverPort(t, server)
	client := NewDeviceClient(DeviceClientOptions{HTTPClient: server.Client()})

	got, err := client.Fetch(context.Background(), Instance{ID: 4, Key: "KEY", Status: "Stopped", Port: &port})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got.Count != 0 || got.Connected || got.Stale || got.Source != "not-running" || len(got.Devices) != 0 {
		t.Fatalf("Fetch() = %#v", got)
	}
}

func TestDeviceClientFailuresWithoutCache(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "malformed JSON", body: `{bad`, code: http.StatusOK},
		{name: "unexpected shape", body: `{"count":1,"message":"ok"}`, code: http.StatusOK},
		{name: "HTTP error", body: `fail`, code: http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			})
			defer server.Close()
			client := NewDeviceClient(DeviceClientOptions{HTTPClient: server.Client()})
			got, err := client.Fetch(context.Background(), runningInstance(server, `{}`))
			if err == nil || got.Source != "live" || got.Error == "" || got.Connected {
				t.Fatalf("Fetch() = %#v err=%v", got, err)
			}
		})
	}
}

func TestDeviceClientTimeout(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer server.Close()
	client := NewDeviceClient(DeviceClientOptions{HTTPClient: server.Client(), Timeout: time.Millisecond})
	_, err := client.Fetch(context.Background(), runningInstance(server, `{}`))
	if err == nil || !strings.Contains(err.Error(), "context deadline") {
		t.Fatalf("Fetch() err = %v, want deadline", err)
	}
}

func TestDeviceClientSendsBasicAuthWithColonPassword(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:p:a:ss"))
		if r.Header.Get("Authorization") != want {
			t.Fatalf("Authorization = %q, want %q", r.Header.Get("Authorization"), want)
		}
		_, _ = w.Write([]byte(`{"accounts":{"a":{"id":"a"},"b":{"id":"b"}}}`))
	})
	defer server.Close()
	client := NewDeviceClient(DeviceClientOptions{HTTPClient: server.Client()})
	got, err := client.Fetch(context.Background(), runningInstance(server, `{"flags":{"basicAuth":[{"username":"user","password":"p:a:ss"}]}}`))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got.Count != 2 || !got.Connected {
		t.Fatalf("Fetch() = %#v", got)
	}
}

func TestDeviceClientIgnoresCacheAfterInstanceStoppedOrDeletedReset(t *testing.T) {
	client := NewDeviceClient(DeviceClientOptions{})
	client.ClearCache(1)
	client.ClearAllCache()
}

func newDeviceServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func runningInstance(server *httptest.Server, config string) Instance {
	port := serverPort(nil, server)
	return Instance{ID: 1, Key: "ABC12345", Status: "running", Port: &port, Config: config}
}

func serverPort(t *testing.T, server *httptest.Server) int {
	if t != nil {
		t.Helper()
	}
	parts := strings.Split(server.URL, ":")
	if len(parts) == 0 {
		if t != nil {
			t.Fatal("server URL missing port")
		}
		panic(errors.New("server URL missing port"))
	}
	var port int
	_, err := fmt.Sscanf(parts[len(parts)-1], "%d", &port)
	if err != nil {
		if t != nil {
			t.Fatalf("parse port: %v", err)
		}
		panic(err)
	}
	return port
}
