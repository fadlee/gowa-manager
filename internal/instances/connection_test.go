package instances

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestConnectionTesterOKIncludesStatusBodyAndMessage(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/ABC12345/devices" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(`{"devices":[]}`))
	})
	defer server.Close()
	tester := NewConnectionTester(ConnectionTesterOptions{HTTPClient: server.Client()})

	got := tester.Test(context.Background(), runningInstance(server, `{}`))
	if !got.OK || got.Status != http.StatusOK || got.Message != "Successfully connected to the GOWA API." || got.Body != `{"devices":[]}` {
		t.Fatalf("Test() = %#v", got)
	}
}

func TestConnectionTesterFailureIncludesStatusAndDefaultEmptyBody(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer server.Close()
	tester := NewConnectionTester(ConnectionTesterOptions{HTTPClient: server.Client()})

	got := tester.Test(context.Background(), runningInstance(server, `{}`))
	if got.OK || got.Status != http.StatusUnauthorized || got.Message != "Failed to connect to the GOWA API." || got.Body != "No response body." {
		t.Fatalf("Test() = %#v", got)
	}
}

func TestConnectionTesterBoundsBodyToSixHundredCharacters(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 700)))
	})
	defer server.Close()
	tester := NewConnectionTester(ConnectionTesterOptions{HTTPClient: server.Client()})

	got := tester.Test(context.Background(), runningInstance(server, `{}`))
	if len(got.Body) != 603 || !strings.HasSuffix(got.Body, "...") {
		t.Fatalf("body len/suffix = %d/%q", len(got.Body), got.Body[len(got.Body)-3:])
	}
}

func TestConnectionTesterUnavailableInstanceDoesNotFetch(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected fetch") })
	defer server.Close()
	port := serverPort(t, server)
	tester := NewConnectionTester(ConnectionTesterOptions{HTTPClient: server.Client()})

	got := tester.Test(context.Background(), Instance{ID: 2, Key: "ABC12345", Status: "stopped", Port: &port})
	if got.OK || got.Message != "Instance is not running. Start it before testing the GOWA API connection." || got.Status != 0 {
		t.Fatalf("Test() = %#v", got)
	}
}

func TestConnectionTesterSendsOptionalBasicAuthAndDoesNotLeakSecretOnError(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:p:a:ss"))
		if r.Header.Get("Authorization") != want {
			t.Fatalf("Authorization = %q, want %q", r.Header.Get("Authorization"), want)
		}
		http.Error(w, "secret p:a:ss", http.StatusForbidden)
	})
	defer server.Close()
	tester := NewConnectionTester(ConnectionTesterOptions{HTTPClient: server.Client()})

	got := tester.Test(context.Background(), runningInstance(server, `{"flags":{"basicAuth":[{"username":"user","password":"p:a:ss"}]}}`))
	if got.OK || strings.Contains(got.Message, "p:a:ss") {
		t.Fatalf("Test() = %#v", got)
	}
}

func TestConnectionTesterTimeoutDoesNotLeakSecrets(t *testing.T) {
	server := newDeviceServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer server.Close()
	tester := NewConnectionTester(ConnectionTesterOptions{HTTPClient: server.Client(), Timeout: time.Millisecond})
	got := tester.Test(context.Background(), runningInstance(server, `{"flags":{"basicAuth":[{"username":"secret-user","password":"secret-pass"}]}}`))
	if got.OK || got.Message == "" || strings.Contains(got.Message, "secret") {
		t.Fatalf("Test() = %#v", got)
	}
}
