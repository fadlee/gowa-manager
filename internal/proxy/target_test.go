package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/fadlee/gowa-manager/internal/instances"
)

// fakeRepo is a minimal instances.Repository for proxy tests. Only
// FindByKey is exercised by target resolution; the other methods panic
// so accidental misuse is caught loudly.
type fakeRepo struct {
	items map[string]instances.Instance
	err   error
}

func newFakeRepo(items ...instances.Instance) *fakeRepo {
	m := make(map[string]instances.Instance, len(items))
	for _, i := range items {
		m[i.Key] = i
	}
	return &fakeRepo{items: m}
}

func (r *fakeRepo) List(context.Context) ([]instances.Instance, error) {
	panic("List not used by proxy tests")
}
func (r *fakeRepo) FindByID(context.Context, int64) (instances.Instance, error) {
	panic("FindByID not used by proxy tests")
}
func (r *fakeRepo) FindByKey(_ context.Context, key string) (instances.Instance, error) {
	if r.err != nil {
		return instances.Instance{}, r.err
	}
	inst, ok := r.items[key]
	if !ok {
		return instances.Instance{}, instances.ErrNotFound
	}
	return inst, nil
}
func (r *fakeRepo) Create(context.Context, instances.CreateInput) (instances.Instance, error) {
	panic("Create not used by proxy tests")
}
func (r *fakeRepo) Update(context.Context, instances.UpdateInput) (instances.Instance, error) {
	panic("Update not used by proxy tests")
}
func (r *fakeRepo) UpdateStatus(context.Context, int64, string, *string) (instances.Instance, error) {
	panic("UpdateStatus not used by proxy tests")
}
func (r *fakeRepo) ClearError(context.Context, int64) (instances.Instance, error) {
	panic("ClearError not used by proxy tests")
}
func (r *fakeRepo) UpdatePort(context.Context, int64, *int) error {
	panic("UpdatePort not used by proxy tests")
}
func (r *fakeRepo) Delete(context.Context, int64) error {
	panic("Delete not used by proxy tests")
}

func intPtr(v int) *int { return &v }

// ---- IsInstanceAvailable ----

func TestIsInstanceAvailable(t *testing.T) {
	cases := []struct {
		name string
		inst instances.Instance
		want bool
	}{
		{"running with port", instances.Instance{Status: "running", Port: intPtr(8080)}, true},
		{"stopped with port", instances.Instance{Status: "stopped", Port: intPtr(8080)}, false},
		{"running without port", instances.Instance{Status: "running", Port: nil}, false},
		{"stopped without port", instances.Instance{Status: "stopped", Port: nil}, false},
		{"empty status", instances.Instance{Status: "", Port: intPtr(8080)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsInstanceAvailable(c.inst); got != c.want {
				t.Fatalf("IsInstanceAvailable(%+v) = %v, want %v", c.inst, got, c.want)
			}
		})
	}
}

// ---- ResolveTarget: availability / lookup ----

func TestResolveTarget_InstanceNotFound(t *testing.T) {
	r := NewTargetResolver(newFakeRepo())
	_, err := r.ResolveTarget(context.Background(), "missing", "/")
	if !errors.Is(err, instances.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveTarget_NotRunning(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "stopped", Port: intPtr(8080)}
	r := NewTargetResolver(newFakeRepo(inst))
	_, err := r.ResolveTarget(context.Background(), "k", "/")
	if err == nil {
		t.Fatal("expected error for stopped instance, got nil")
	}
}

func TestResolveTarget_NoPort(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: nil}
	r := NewTargetResolver(newFakeRepo(inst))
	_, err := r.ResolveTarget(context.Background(), "k", "/")
	if err == nil {
		t.Fatal("expected error for running instance without port, got nil")
	}
}

func TestResolveTarget_RepoError(t *testing.T) {
	dbErr := errors.New("db down")
	r := NewTargetResolver(&fakeRepo{err: dbErr})
	_, err := r.ResolveTarget(context.Background(), "k", "/")
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected db error, got %v", err)
	}
}

// ---- ResolveTarget: localhost target only ----

func TestResolveTarget_LocalhostOnly(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(8080)}
	r := NewTargetResolver(newFakeRepo(inst))
	tgt, err := r.ResolveTarget(context.Background(), "k", "/foo")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL.Scheme != "http" {
		t.Fatalf("scheme = %q, want http", tgt.URL.Scheme)
	}
	if tgt.URL.Hostname() != "localhost" {
		t.Fatalf("hostname = %q, want localhost", tgt.URL.Hostname())
	}
	if tgt.URL.Port() != "8080" {
		t.Fatalf("port = %q, want 8080", tgt.URL.Port())
	}
	if tgt.URL.Path != "/foo" {
		t.Fatalf("path = %q, want /foo", tgt.URL.Path)
	}
	if tgt.Instance.Key != "k" {
		t.Fatalf("instance key = %q, want k", tgt.Instance.Key)
	}
}

// ---- ResolveTarget: root and wildcard paths ----

func TestResolveTarget_RootPath(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(9000)}
	r := NewTargetResolver(newFakeRepo(inst))
	tgt, err := r.ResolveTarget(context.Background(), "k", "/")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL.Path != "/" {
		t.Fatalf("path = %q, want /", tgt.URL.Path)
	}
}

func TestResolveTarget_WildcardPath(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(9000)}
	r := NewTargetResolver(newFakeRepo(inst))
	tgt, err := r.ResolveTarget(context.Background(), "k", "/a/b/c/d/e/f")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL.Path != "/a/b/c/d/e/f" {
		t.Fatalf("path = %q, want /a/b/c/d/e/f", tgt.URL.Path)
	}
}

// ---- ResolveTarget: query preservation ----

func TestResolveTarget_QueryPreserved(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(9000)}
	r := NewTargetResolver(newFakeRepo(inst))
	tgt, err := r.ResolveTarget(context.Background(), "k", "/search?q=hello&page=2")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL.RawQuery != "q=hello&page=2" {
		t.Fatalf("rawquery = %q, want q=hello&page=2", tgt.URL.RawQuery)
	}
}

// ---- ResolveTarget: /app/{key} prefix handling ----

func TestResolveTarget_ProxyPrefixStripped(t *testing.T) {
	// The caller is expected to pass the path AFTER /app/{key}. The
	// resolver must not double-prefix; it appends the path verbatim to
	// the localhost target.
	inst := instances.Instance{Key: "mykey", Status: "running", Port: intPtr(7000)}
	r := NewTargetResolver(newFakeRepo(inst))
	tgt, err := r.ResolveTarget(context.Background(), "mykey", "/admin/settings")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL.Path != "/admin/settings" {
		t.Fatalf("path = %q, want /admin/settings (no /app/mykey prefix)", tgt.URL.Path)
	}
}

// ---- SSRF rejection tests ----

func TestSSRF_RequestPathCannotSelectScheme(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(8080)}
	r := NewTargetResolver(newFakeRepo(inst))
	// A path that tries to override the scheme must still target localhost.
	tgt, err := r.ResolveTarget(context.Background(), "k", "https://evil.com/admin")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL.Scheme != "http" {
		t.Fatalf("scheme = %q, want http (request must not override)", tgt.URL.Scheme)
	}
	if tgt.URL.Hostname() != "localhost" {
		t.Fatalf("hostname = %q, want localhost", tgt.URL.Hostname())
	}
	if tgt.URL.Port() != "8080" {
		t.Fatalf("port = %q, want 8080", tgt.URL.Port())
	}
}

func TestSSRF_RequestPathCannotSelectHost(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(8080)}
	r := NewTargetResolver(newFakeRepo(inst))
	tgt, err := r.ResolveTarget(context.Background(), "k", "//evil.com:9999/admin")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL.Hostname() != "localhost" {
		t.Fatalf("hostname = %q, want localhost", tgt.URL.Hostname())
	}
	if tgt.URL.Port() != "8080" {
		t.Fatalf("port = %q, want 8080 (request must not override)", tgt.URL.Port())
	}
}

func TestSSRF_RequestPathCannotSelectPort(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(8080)}
	r := NewTargetResolver(newFakeRepo(inst))
	tgt, err := r.ResolveTarget(context.Background(), "k", "/admin")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL.Port() != "8080" {
		t.Fatalf("port = %q, want 8080 from DB only", tgt.URL.Port())
	}
}

func TestSSRF_InvalidDBPortRejected(t *testing.T) {
	cases := []struct {
		name string
		port *int
	}{
		{"zero", intPtr(0)},
		{"negative", intPtr(-1)},
		{"too large", intPtr(70000)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inst := instances.Instance{Key: "k", Status: "running", Port: c.port}
			r := NewTargetResolver(newFakeRepo(inst))
			_, err := r.ResolveTarget(context.Background(), "k", "/")
			if err == nil {
				t.Fatalf("expected error for invalid port %d, got nil", *c.port)
			}
		})
	}
}

func TestSSRF_EmptyKeyRejected(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(8080)}
	r := NewTargetResolver(newFakeRepo(inst))
	_, err := r.ResolveTarget(context.Background(), "", "/")
	if err == nil {
		t.Fatal("expected error for empty instance key, got nil")
	}
}

// ---- ProxyPrefix constant ----

func TestProxyPrefixConstant(t *testing.T) {
	if ProxyPrefix != "app" {
		t.Fatalf("ProxyPrefix = %q, want app", ProxyPrefix)
	}
}

// Ensure Target.URL is a real *url.URL usable by callers.
func TestResolveTarget_URLUsable(t *testing.T) {
	inst := instances.Instance{Key: "k", Status: "running", Port: intPtr(8080)}
	r := NewTargetResolver(newFakeRepo(inst))
	tgt, err := r.ResolveTarget(context.Background(), "k", "/foo?bar=baz")
	if err != nil {
		t.Fatalf("ResolveTarget error = %v", err)
	}
	if tgt.URL == nil {
		t.Fatal("Target.URL is nil")
	}
	if got := tgt.URL.String(); got != "http://localhost:8080/foo?bar=baz" {
		t.Fatalf("URL.String() = %q, want http://localhost:8080/foo?bar=baz", got)
	}
}
