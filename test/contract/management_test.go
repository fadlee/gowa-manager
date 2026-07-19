package contract

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

const (
	contractUser = "contract-admin"
	contractPass = "contract-password"
)

func TestManagementParity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	fixture := readFixture(t)
	repoRoot := findRepoRoot(t)
	bun := startBackend(t, ctx, repoRoot, "bun")
	goBackend := startBackend(t, ctx, repoRoot, "go")

	client := &http.Client{Timeout: 10 * time.Second}
	bunCreate := doScenario(t, client, bun, scenario{method: http.MethodPost, path: "/api/instances", body: fixture.createBody()})
	goCreate := doScenario(t, client, goBackend, scenario{method: http.MethodPost, path: "/api/instances", body: fixture.createBody()})
	bunID := jsonNumberID(t, bunCreate.JSONBody)
	goID := jsonNumberID(t, goCreate.JSONBody)

	updateBody := fixture
	updateBody.Name = "contract-alpha-updated"
	updateBody.Config = `{"webhook":"https://example.invalid/gowa/contract-updated","log_level":"info","features":{"devices":false}}`
	updateBody.GOWAVersion = "v9.8.8"

	scenarios := []scenario{
		{name: "health", method: http.MethodGet, path: "/api/health", noAuth: true},
		{name: "list", method: http.MethodGet, path: "/api/instances"},
		{name: "detail", method: http.MethodGet, path: "/api/instances/{id}"},
		{name: "update", method: http.MethodPut, path: "/api/instances/{id}", body: updateBody.createBody()},
		{name: "reset", method: http.MethodPost, path: "/api/instances/{id}/reset-data"},
		{name: "system status", method: http.MethodGet, path: "/api/system/status"},
		{name: "system config", method: http.MethodGet, path: "/api/system/config"},
		{name: "system next port", method: http.MethodGet, path: "/api/system/ports/next"},
		{name: "system port available", method: http.MethodGet, path: "/api/system/ports/65534/available"},
		{name: "versions installed", method: http.MethodGet, path: "/api/system/versions/installed"},
		{name: "versions available", method: http.MethodGet, path: "/api/system/versions/available?limit=1"},
		{name: "versions usage", method: http.MethodGet, path: "/api/system/versions/usage"},
		{name: "install failure", method: http.MethodPost, path: "/api/system/versions/install", body: []byte(`{"version":"not-a-real-contract-version"}`)},
		{name: "cleanup", method: http.MethodPost, path: "/api/system/versions/cleanup", body: []byte(`{"keepCount":1}`)},
		{name: "devices while stopped", method: http.MethodGet, path: "/api/instances/{id}/devices"},
		{name: "test connection failure", method: http.MethodPost, path: "/api/instances/{id}/test-connection"},
		{name: "delete", method: http.MethodDelete, path: "/api/instances/{id}"},
		{name: "detail after delete", method: http.MethodGet, path: "/api/instances/{id}"},
	}

	compareSnapshots(t, "create", bunCreate, bun, goCreate, goBackend)
	for _, sc := range scenarios {
		bunSnap := doScenario(t, client, bun, sc.withID(bunID))
		goSnap := doScenario(t, client, goBackend, sc.withID(goID))
		compareSnapshots(t, sc.name, bunSnap, bun, goSnap, goBackend)
	}

	unauth := doScenario(t, client, bun, scenario{name: "bun auth required", method: http.MethodGet, path: "/api/instances", noAuth: true})
	if unauth.Status != http.StatusUnauthorized {
		t.Fatalf("Bun unauthenticated management status = %d, want 401; body = %#v", unauth.Status, unauth.JSONBody)
	}

	compareSideEffects(t, bun, goBackend)
}

type fixtureInstance struct {
	Name        string `json:"name"`
	Config      string `json:"config"`
	GOWAVersion string `json:"gowa_version"`
}

func (f fixtureInstance) createBody() []byte {
	data, err := json.Marshal(f)
	if err != nil {
		panic(err)
	}
	return data
}

type backend struct {
	name    string
	baseURL string
	dataDir string
	port    int
	cmd     *exec.Cmd
}

type scenario struct {
	name   string
	method string
	path   string
	body   []byte
	noAuth bool
}

func (s scenario) withID(id string) scenario {
	s.path = strings.ReplaceAll(s.path, "{id}", id)
	return s
}

func readFixture(t *testing.T) fixtureInstance {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "instance-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture fixtureInstance
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func startBackend(t *testing.T, ctx context.Context, repoRoot string, name string) backend {
	t.Helper()
	port := freePort(t)
	dataDir, err := os.MkdirTemp("", "gowa-contract-"+name+"-")
	if err != nil {
		t.Fatal(err)
	}
	var cmd *exec.Cmd
	if name == "bun" {
		if _, err := exec.LookPath("bun"); err != nil {
			t.Skipf("bun executable not found; install Bun to run management parity contracts: %v", err)
		}
		cmd = exec.CommandContext(ctx, "bun", "run", "src/index.ts", "--port", fmt.Sprint(port), "--data-dir", dataDir)
	} else {
		cmd = exec.CommandContext(ctx, "go", "run", "./cmd/gowa-manager-go", "--port", fmt.Sprint(port), "--data-dir", dataDir)
	}
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "ADMIN_USERNAME="+contractUser, "ADMIN_PASSWORD="+contractPass, "DATA_DIR="+dataDir, "PORT="+fmt.Sprint(port))
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s backend: %v", name, err)
	}
	be := backend{name: name, baseURL: fmt.Sprintf("http://127.0.0.1:%d", port), dataDir: dataDir, port: port, cmd: cmd}
	t.Cleanup(func() {
		stopBackend(t, be, output.String())
		removeAllEventually(t, dataDir)
	})
	waitForHealth(t, ctx, be, output.String)
	return be
}

func stopBackend(t *testing.T, be backend, output string) {
	t.Helper()
	if be.cmd == nil || be.cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(be.cmd.Process.Pid)).Run()
	} else {
		_ = be.cmd.Process.Signal(os.Interrupt)
	}
	done := make(chan error, 1)
	go func() { done <- be.cmd.Wait() }()
	select {
	case <-time.After(5 * time.Second):
		_ = be.cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	case err := <-done:
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("%s backend exited with %v\n%s", be.name, err, output)
			}
		}
	}
}

func waitForHealth(t *testing.T, ctx context.Context, be backend, output func() string) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context canceled while waiting for %s health: %v\n%s", be.name, ctx.Err(), output())
		default:
		}
		resp, err := client.Get(be.baseURL + "/api/health")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s backend did not become healthy on %s\n%s", be.name, be.baseURL, output())
}

func removeAllEventually(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		err = os.RemoveAll(path)
		if err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Logf("remove temp data dir %s: %v", path, err)
	}
}

func doScenario(t *testing.T, client *http.Client, be backend, sc scenario) Snapshot {
	t.Helper()
	req, err := http.NewRequest(sc.method, be.baseURL+sc.path, bytes.NewReader(sc.body))
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if !sc.noAuth {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(contractUser+":"+contractPass)))
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s %s: %v", be.name, sc.method, sc.path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			decoded = string(body)
		}
	}
	return Snapshot{Status: resp.StatusCode, Headers: resp.Header, JSONBody: decoded}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root containing go.mod")
		}
		dir = parent
	}
}

func compareSnapshots(t *testing.T, name string, bunSnap Snapshot, bun backend, goSnap Snapshot, goBackend backend) {
	t.Helper()
	b := Normalize(bunSnap, Options{Ports: map[int]string{bun.port: "<bun-port>"}})
	g := Normalize(goSnap, Options{Ports: map[int]string{goBackend.port: "<go-port>"}})
	b.Headers = nil
	g.Headers = nil
	b.JSONBody = normalizeContractBody(b.JSONBody)
	g.JSONBody = normalizeContractBody(g.JSONBody)
	if name == "install failure" {
		b.Status = 0
		g.Status = 0
		b.JSONBody = map[string]any{"success": false, "error": "<install-failure>"}
		g.JSONBody = map[string]any{"success": false, "error": "<install-failure>"}
	}
	if !reflect.DeepEqual(b, g) {
		t.Fatalf("%s parity mismatch\nBun: %#v\nGo:  %#v", name, b, g)
	}
}

func normalizeContractBody(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range v {
			if key == "installedAt" || key == "fetchedAt" || key == "size" {
				continue
			}
			if key == "data_directory" || key == "binaries_directory" {
				out[key] = "<path>"
				continue
			}
			if key == "key" {
				out[key] = "<key>"
				continue
			}
			if key == "managerVersion" {
				out[key] = "<manager-version>"
				continue
			}
			if key == "port" {
				out[key] = "<instance-port>"
				continue
			}
			out[key] = normalizeContractBody(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeContractBody(child)
		}
		return out
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			return normalizeEmbeddedConfig(decoded)
		}
		return v
	default:
		return v
	}
}

func normalizeEmbeddedConfig(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range v {
			if key == "basePath" {
				out[key] = "/app/<key>"
				continue
			}
			out[key] = normalizeEmbeddedConfig(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeEmbeddedConfig(child)
		}
		return out
	default:
		return v
	}
}

func jsonNumberID(t *testing.T, body any) string {
	t.Helper()
	object, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("create body is not object: %#v", body)
	}
	id, ok := object["id"].(float64)
	if !ok || id == 0 {
		t.Fatalf("create body missing numeric id: %#v", body)
	}
	return fmt.Sprintf("%.0f", id)
}

func compareSideEffects(t *testing.T, bun backend, goBackend backend) {
	t.Helper()
	if got, want := readRows(t, bun.dataDir), readRows(t, goBackend.dataDir); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized SQLite rows differ\nBun: %#v\nGo:  %#v", got, want)
	}
	if got, want := relativeTree(t, bun.dataDir), relativeTree(t, goBackend.dataDir); !reflect.DeepEqual(got, want) {
		t.Fatalf("relative filesystem trees differ\nBun: %#v\nGo:  %#v", got, want)
	}
}

func readRows(t *testing.T, dataDir string) []map[string]any {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "gowa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("sqlite integrity for %s = %q, %v", dataDir, integrity, err)
	}
	rows, err := db.Query(`SELECT id, key, name, port, status, config, gowa_version, error_message, created_at, updated_at FROM instances ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int
		var key, name, status, config, version, createdAt, updatedAt string
		var port sql.NullInt64
		var errorMessage sql.NullString
		if err := rows.Scan(&id, &key, &name, &port, &status, &config, &version, &errorMessage, &createdAt, &updatedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, map[string]any{"id": id, "key": "<key>", "name": name, "port": "<instance-port>", "status": status, "config": normalizeContractBody(config), "gowa_version": version, "error_message": nullableString(errorMessage), "created_at": "<timestamp>", "updated_at": "<timestamp>"})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func relativeTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	instancesRoot := filepath.Join(root, "instances")
	if _, err := os.Stat(instancesRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	err := filepath.WalkDir(instancesRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == instancesRoot {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func nullableInt(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return int(v.Int64)
}

func nullableString(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}
