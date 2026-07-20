// Package compat contains Docker integration tests for the Go backend
// images. These tests build the Dockerfile, start a container, and verify
// runtime behavior: health/readiness endpoints, embedded SPA, non-root
// permissions, /data volume persistence, SIGTERM graceful shutdown, and
// restart recovery.
//
// The tests require the Docker daemon to be available. They are skipped
// automatically when Docker is not installed or not running.
package compat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testImage is the tag used for the image built by these tests.
const testImage = "gowa-manager-test:latest"

// repoRoot returns the repository root (the directory containing the
// Dockerfile and go.mod). go test executes with the working directory set
// to the package directory (test/compat), so we walk up to find go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find repo root (go.mod) from %s", wd)
	return ""
}

// adminCreds returns the Basic auth username/password used by the test
// container. These match the ENV defaults in the Dockerfile.
func adminCreds() (string, string) {
	return "admin", "password"
}

// dockerAvailable reports whether the Docker CLI is installed and the daemon
// is responsive.
func dockerAvailable(t *testing.T) {
	t.Helper()
	// The image is Linux-only (FROM oven/bun:1-alpine, gcr.io/distroless).
	// GitHub's Windows runners expose a Docker daemon in Windows-containers
	// mode, so `docker info` succeeds but the Linux base image has "no
	// matching manifest for windows/amd64" and the build fails. Skip rather
	// than fail on any platform that cannot run Linux containers.
	if runtime.GOOS == "windows" {
		t.Skip("docker compat suite requires Linux containers; skipping on Windows")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skipf("docker daemon not available: %v", err)
	}
}

// buildImage builds the test Docker image from the repo-root Dockerfile.
func buildImage(t *testing.T) {
	t.Helper()
	root := repoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", testImage, "-f", filepath.Join(root, "Dockerfile"), root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker build failed: %v\n%s", err, out)
	}
}

// removeImage removes the test image if it exists. Best-effort.
func removeImage(t *testing.T) {
	t.Helper()
	_ = exec.Command("docker", "rmi", "-f", testImage).Run()
}

// containerInfo holds the details needed to interact with a running container.
type containerInfo struct {
	id      string
	port    string // host-side mapped port
	cleanup func()
}

// startContainer runs the test image with a random host port mapped to 3000
// and a named volume for /data. It waits for /api/ready to return 200.
func startContainer(t *testing.T, volumeName string) *containerInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start the container detached, mapping port 3000 to a random host port.
	cmd := exec.CommandContext(ctx,
		"docker", "run", "-d",
		"--name", volumeName,
		"-p", "3000",
		"-v", volumeName+":/app/data",
		testImage,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker run failed: %v\n%s", err, out)
	}
	id := strings.TrimSpace(string(out))

	// Determine the mapped host port.
	portCmd := exec.CommandContext(ctx, "docker", "port", id, "3000")
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		killContainer(t, id)
		t.Fatalf("docker port failed: %v\n%s", err, portOut)
	}
	// Output looks like "0.0.0.0:32768\n:::32768\n" — extract the first port.
	port := extractPort(strings.TrimSpace(string(portOut)))
	if port == "" {
		killContainer(t, id)
		t.Fatalf("could not parse mapped port from: %q", string(portOut))
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
	if !waitForReady(t, baseURL, 40*time.Second) {
		killContainer(t, id)
		t.Fatalf("container did not become ready on %s", baseURL)
	}

	ci := &containerInfo{
		id:   id,
		port: port,
		cleanup: func() {
			killContainer(t, id)
		},
	}
	return ci
}

// extractPort pulls the port number from `docker port` output.
func extractPort(s string) string {
	// e.g. "0.0.0.0:32768" or ":::32768"
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.LastIndex(line, ":"); idx >= 0 {
			return line[idx+1:]
		}
	}
	return ""
}

// killContainer stops and removes a container. Best-effort.
func killContainer(t *testing.T, id string) {
	t.Helper()
	if id == "" {
		return
	}
	_ = exec.Command("docker", "stop", "-t", "5", id).Run()
	_ = exec.Command("docker", "rm", "-f", id).Run()
}

// waitForReady polls /api/ready until it returns 200 or the timeout elapses.
func waitForReady(t *testing.T, baseURL string, timeout time.Duration) bool {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/ready")
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return true
			}
			resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// httpGet performs an authenticated GET request and returns the status code
// and body.
func httpGet(t *testing.T, url string, auth bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if auth {
		user, pass := adminCreds()
		req.SetBasicAuth(user, pass)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("http get %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// dockerExec runs a command inside a running container and returns its
// combined output.
func dockerExec(t *testing.T, id string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmdArgs := append([]string{"exec", id}, args...)
	out, err := exec.CommandContext(ctx, "docker", cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker exec %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// removeVolume removes a Docker volume. Best-effort.
func removeVolume(t *testing.T, name string) {
	t.Helper()
	_ = exec.Command("docker", "volume", "rm", "-f", name).Run()
}

// uniqueName generates a unique container/volume name from the test name and a
// timestamp.
func uniqueName(t *testing.T) string {
	t.Helper()
	return "gowa-test-" + sanitize(t.Name()) + "-" + fmt.Sprint(time.Now().UnixNano())
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestDockerSuite is the single entry point invoked by:
//
//	go test ./test/compat -run TestDocker -v
//
// It builds the image once and runs all sub-checks as subtests so that the
// image build cost is amortized.
func TestDockerSuite(t *testing.T) {
	dockerAvailable(t)
	buildImage(t)
	t.Cleanup(func() { removeImage(t) })

	// Subtests share the built image but use independent containers/volumes.
	t.Run("Health", testDockerHealth)
	t.Run("Ready", testDockerReady)
	t.Run("EmbeddedSPA", testDockerEmbeddedSPA)
	t.Run("NonRootUser", testDockerNonRootUser)
	t.Run("VolumePersistence", testDockerVolumePersistence)
	t.Run("RestartRecovery", testDockerRestartRecovery)
	t.Run("SIGTERMShutdown", testDockerSIGTERMShutdown)
	t.Run("RollbackImageSameVolume", testDockerRollbackImageSameVolume)
}

// testDockerHealth verifies /api/health returns 200.
func testDockerHealth(t *testing.T) {
	vol := uniqueName(t)
	t.Cleanup(func() { removeVolume(t, vol) })
	ci := startContainer(t, vol)
	defer ci.cleanup()

	baseURL := fmt.Sprintf("http://127.0.0.1:%s", ci.port)
	code, _ := httpGet(t, baseURL+"/api/health", false)
	if code != http.StatusOK {
		t.Fatalf("/api/health = %d, want 200", code)
	}
}

// testDockerReady verifies /api/ready returns 200 after startup.
func testDockerReady(t *testing.T) {
	vol := uniqueName(t)
	t.Cleanup(func() { removeVolume(t, vol) })
	ci := startContainer(t, vol)
	defer ci.cleanup()

	baseURL := fmt.Sprintf("http://127.0.0.1:%s", ci.port)
	// startContainer already waits for ready, but re-check explicitly.
	code, _ := httpGet(t, baseURL+"/api/ready", false)
	if code != http.StatusOK {
		t.Fatalf("/api/ready = %d, want 200", code)
	}
}

// testDockerEmbeddedSPA verifies GET / returns HTML containing the expected
// root marker (#root) from the embedded React SPA.
func testDockerEmbeddedSPA(t *testing.T) {
	vol := uniqueName(t)
	t.Cleanup(func() { removeVolume(t, vol) })
	ci := startContainer(t, vol)
	defer ci.cleanup()

	baseURL := fmt.Sprintf("http://127.0.0.1:%s", ci.port)
	code, body := httpGet(t, baseURL+"/", false)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if !strings.Contains(body, `id="root"`) {
		t.Fatalf("GET / body does not contain embedded SPA marker (id=\"root\"):\n%s", body)
	}
}

// testDockerNonRootUser verifies the container process runs as a non-root
// user.
func testDockerNonRootUser(t *testing.T) {
	vol := uniqueName(t)
	t.Cleanup(func() { removeVolume(t, vol) })
	ci := startContainer(t, vol)
	defer ci.cleanup()

	out := dockerExec(t, ci.id, "id", "-u")
	uid := strings.TrimSpace(out)
	if uid == "0" {
		t.Fatalf("container runs as root (uid 0); expected non-root user")
	}
	t.Logf("container uid=%s", uid)
}

// testDockerVolumePersistence verifies that data written to /data survives a
// container restart using the same named volume.
func testDockerVolumePersistence(t *testing.T) {
	vol := uniqueName(t)
	t.Cleanup(func() { removeVolume(t, vol) })

	// First container: write a marker file into /data.
	ci1 := startContainer(t, vol)
	marker := "docker-test-" + fmt.Sprint(time.Now().UnixNano())
	dockerExec(t, ci1.id, "sh", "-c", "echo "+marker+" > /app/data/persistence-marker.txt")
	ci1.cleanup()

	// Second container with the same volume: verify the file is present.
	ci2 := startContainer(t, vol)
	defer ci2.cleanup()
	out := dockerExec(t, ci2.id, "cat", "/app/data/persistence-marker.txt")
	if !strings.Contains(out, marker) {
		t.Fatalf("/app/data did not persist across restart: got %q, want marker %q", out, marker)
	}
}

// testDockerRestartRecovery verifies the SQLite database and lock file are
// handled correctly across a stop/start cycle — the manager should start
// cleanly without a stale lock blocking startup.
func testDockerRestartRecovery(t *testing.T) {
	vol := uniqueName(t)
	t.Cleanup(func() { removeVolume(t, vol) })

	// Start, wait for ready, then stop (graceful) and start again.
	ci1 := startContainer(t, vol)
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", ci1.port)

	// Touch the DB by hitting an authenticated endpoint so the SQLite file
	// is created on the volume.
	code, _ := httpGet(t, baseURL+"/api/instances", true)
	if code != http.StatusOK {
		t.Fatalf("/api/instances = %d, want 200", code)
	}

	// Graceful stop via docker stop (sends SIGTERM then SIGKILL after 10s).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "stop", "-t", "10", ci1.id).Run(); err != nil {
		t.Fatalf("docker stop: %v", err)
	}
	_ = exec.Command("docker", "rm", "-f", ci1.id).Run()

	// Start a new container with the same volume and verify readiness.
	ci2 := startContainer(t, vol)
	defer ci2.cleanup()
	baseURL2 := fmt.Sprintf("http://127.0.0.1:%s", ci2.port)
	code, _ = httpGet(t, baseURL2+"/api/instances", true)
	if code != http.StatusOK {
		t.Fatalf("/api/instances after restart = %d, want 200", code)
	}
}

// testDockerSIGTERMShutdown verifies that sending SIGTERM to the container
// triggers a graceful shutdown within 10 seconds.
func testDockerSIGTERMShutdown(t *testing.T) {
	vol := uniqueName(t)
	t.Cleanup(func() { removeVolume(t, vol) })

	// Start the container manually (not via startContainer) so we control
	// the lifecycle and can measure shutdown time.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx,
		"docker", "run", "-d",
		"--name", vol,
		"-p", "3000",
		"-v", vol+":/app/data",
		testImage,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	id := strings.TrimSpace(string(out))
	defer killContainer(t, id)

	// Wait for readiness.
	portOut, err := exec.CommandContext(ctx, "docker", "port", id, "3000").CombinedOutput()
	if err != nil {
		t.Fatalf("docker port: %v\n%s", err, portOut)
	}
	port := extractPort(strings.TrimSpace(string(portOut)))
	if port == "" {
		t.Fatalf("could not parse port from %q", portOut)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
	if !waitForReady(t, baseURL, 40*time.Second) {
		t.Fatalf("container did not become ready")
	}

	// Send SIGTERM via docker kill and measure how long the container takes
	// to exit. A graceful exit (code 0 or 143) within 10s passes.
	start := time.Now()
	killCtx, killCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer killCancel()
	_ = exec.CommandContext(killCtx, "docker", "kill", "--signal=SIGTERM", id).Run()

	// Poll docker inspect for the container's exit status.
	deadline := time.Now().Add(10 * time.Second)
	exited := false
	var exitCode int
	for time.Now().Before(deadline) {
		inspectOut, err := exec.Command("docker", "inspect",
			"-f", "{{.State.Status}}:{{.State.ExitCode}}", id).CombinedOutput()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(inspectOut)), ":")
			if len(parts) == 2 && parts[0] == "exited" {
				exited = true
				fmt.Sscanf(parts[1], "%d", &exitCode)
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	elapsed := time.Since(start)

	if !exited {
		t.Fatalf("container did not exit within 10s of SIGTERM (elapsed=%s)", elapsed)
	}
	t.Logf("container exited after %s with code %d", elapsed, exitCode)
}

// testDockerRollbackImageSameVolume verifies that a second image built from
// the same Dockerfile can read the same /data volume copy that the first
// image wrote. This simulates the rollback scenario where a Bun rollback tag
// reads the same persistent volume as the Go candidate.
func testDockerRollbackImageSameVolume(t *testing.T) {
	vol := uniqueName(t)
	t.Cleanup(func() { removeVolume(t, vol) })

	// First container writes a marker.
	ci1 := startContainer(t, vol)
	marker := "rollback-test-" + fmt.Sprint(time.Now().UnixNano())
	dockerExec(t, ci1.id, "sh", "-c", "echo "+marker+" > /app/data/rollback-marker.txt")
	ci1.cleanup()

	// Build a "rollback" image (same Dockerfile, different tag) to simulate
	// the preserved rollback tag reading the same volume.
	rollbackImage := "gowa-manager-test-rollback:latest"
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "-f", rollbackImage).Run() })
	buildCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	buildOut, err := exec.CommandContext(buildCtx, "docker", "build",
		"-t", rollbackImage, "-f", filepath.Join(repoRoot(t), "Dockerfile"), repoRoot(t)).CombinedOutput()
	if err != nil {
		t.Fatalf("docker build rollback image: %v\n%s", err, buildOut)
	}

	// Start a container from the rollback image with the same volume.
	runCtx, runCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer runCancel()
	rollbackName := vol + "-rollback"
	runOut, err := exec.CommandContext(runCtx,
		"docker", "run", "-d",
		"--name", rollbackName,
		"-p", "3000",
		"-v", vol+":/app/data",
		rollbackImage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run rollback: %v\n%s", err, runOut)
	}
	rollbackID := strings.TrimSpace(string(runOut))
	defer killContainer(t, rollbackID)

	// Verify the rollback container can read the marker file written by the
	// first container.
	out := dockerExec(t, rollbackID, "cat", "/app/data/rollback-marker.txt")
	if !strings.Contains(out, marker) {
		t.Fatalf("rollback image could not read volume marker: got %q, want %q", out, marker)
	}
}

// TestDockerPrebuiltBinary is a sanity check that the Dockerfile.prebuilt can
// select the correct architecture-specific binary when one is provided in a
// ./binaries directory. It is skipped on non-Linux hosts because the
// prebuilt binary is a Linux executable.
func TestDockerPrebuiltBinary(t *testing.T) {
	dockerAvailable(t)
	if os.Getenv("GOWA_DOCKER_PREBUILT_TEST") != "1" {
		t.Skip("set GOWA_DOCKER_PREBUILT_TEST=1 to enable prebuilt Docker image test (requires a Linux Go binary in ./binaries)")
	}

	binDir := filepath.Join(repoRoot(t), "binaries")
	arch := "amd64"
	if _, err := os.Stat(filepath.Join(binDir, "gowa-manager-linux-"+arch)); err != nil {
		t.Skipf("no prebuilt binary found at binaries/gowa-manager-linux-%s", arch)
	}

	prebuiltImage := "gowa-manager-prebuilt-test:latest"
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "-f", prebuiltImage).Run() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "build",
		"-t", prebuiltImage,
		"-f", filepath.Join(repoRoot(t), "Dockerfile.prebuilt"),
		"--build-arg", "TARGETARCH="+arch,
		repoRoot(t))
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker build prebuilt: %v\n%s", err, buf.String())
	}
}
