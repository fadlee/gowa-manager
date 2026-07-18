package versions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type fakeReleaseLister struct {
	releases []GitHubRelease
	err      error
	limits   []int
}

func (f *fakeReleaseLister) ListReleases(ctx context.Context, limit int) ([]GitHubRelease, error) {
	f.limits = append(f.limits, limit)
	return f.releases, f.err
}

func TestGetVersionBinaryPathUsesVersionedBinaryAndResolvesLatest(t *testing.T) {
	dataDir := t.TempDir()
	service := NewService(dataDir, nil)
	writeBinary(t, dataDir, "v1.9.0", []byte("old"), time.Now())
	writeBinary(t, dataDir, "v1.10.0", []byte("new"), time.Now())

	wantExplicit := filepath.Join(dataDir, "bin", "versions", "v1.9.0", binaryName())
	if got := service.GetVersionBinaryPath("v1.9.0"); got != wantExplicit {
		t.Fatalf("explicit path = %q, want %q", got, wantExplicit)
	}

	wantLatest := filepath.Join(dataDir, "bin", "versions", "v1.10.0", binaryName())
	if got := service.GetVersionBinaryPath("latest"); got != wantLatest {
		t.Fatalf("latest path = %q, want %q", got, wantLatest)
	}
}

func TestGetVersionBinaryPathLatestFallsBackToLegacyPathWithoutInstalledVersions(t *testing.T) {
	dataDir := t.TempDir()
	service := NewService(dataDir, nil)
	want := filepath.Join(dataDir, "bin", binaryName())
	if got := service.GetVersionBinaryPath("latest"); got != want {
		t.Fatalf("latest path = %q, want legacy %q", got, want)
	}
}

func TestGetInstalledVersionsReturnsMetadataAndSkipsInvalidEntries(t *testing.T) {
	dataDir := t.TempDir()
	service := NewService(dataDir, nil)
	now := time.Now()
	writeBinary(t, dataDir, "v1.2.0", []byte("12345"), now.Add(-time.Hour))
	writeBinary(t, dataDir, "v1.10.0", []byte("1234567"), now)
	mustMkdir(t, filepath.Join(dataDir, "bin", "versions", "latest"))
	mustMkdir(t, filepath.Join(dataDir, "bin", "versions", "v9.9.9"))

	installed, err := service.GetInstalledVersions()
	if err != nil {
		t.Fatalf("GetInstalledVersions() error = %v", err)
	}

	if len(installed) != 2 {
		t.Fatalf("len(installed) = %d, want 2: %+v", len(installed), installed)
	}
	if installed[0].Version != "v1.10.0" || !installed[0].IsLatest || installed[0].Size != 7 || installed[0].InstalledAt.IsZero() {
		t.Fatalf("installed[0] = %+v, want latest v1.10.0 with metadata", installed[0])
	}
	if installed[1].Version != "v1.2.0" || installed[1].IsLatest || installed[1].Size != 5 {
		t.Fatalf("installed[1] = %+v, want v1.2.0 metadata", installed[1])
	}
}

func TestGetAvailableVersionsMergesGitHubReleasesWithInstalledMetadata(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v2.0.0", []byte("installed"), time.Now())
	releases := &fakeReleaseLister{releases: []GitHubRelease{
		{TagName: "v2.0.0"},
		{TagName: "v1.9.0"},
	}}
	service := NewService(dataDir, releases)

	available, err := service.GetAvailableVersions(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetAvailableVersions() error = %v", err)
	}

	if len(releases.limits) != 1 || releases.limits[0] != 2 {
		t.Fatalf("limits = %+v, want [2]", releases.limits)
	}
	if len(available) != 3 {
		t.Fatalf("len(available) = %d, want 3: %+v", len(available), available)
	}
	if available[0].Version != "latest" || !available[0].Installed || !available[0].IsLatest || available[0].Size != int64(len("installed")) {
		t.Fatalf("latest entry = %+v, want installed latest metadata", available[0])
	}
	if available[1].Version != "v2.0.0" || !available[1].Installed || !available[1].IsLatest {
		t.Fatalf("first release = %+v, want installed latest release", available[1])
	}
	if available[2].Version != "v1.9.0" || available[2].Installed || available[2].IsLatest {
		t.Fatalf("second release = %+v, want uninstalled non-latest release", available[2])
	}
}

func TestGetAvailableVersionsDeterminesLatestFromOutOfOrderPublishedAt(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v2.0.0", []byte("installed"), time.Now())
	releases := &fakeReleaseLister{releases: []GitHubRelease{
		{TagName: "v1.9.0", PublishedAt: "2026-01-01T00:00:00Z"},
		{TagName: "v2.0.0", PublishedAt: "2026-02-01T00:00:00Z"},
	}}
	service := NewService(dataDir, releases)

	available, err := service.GetAvailableVersions(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetAvailableVersions() error = %v", err)
	}

	if available[0].Version != "latest" || !available[0].Installed || !available[0].IsLatest || available[0].Size != int64(len("installed")) {
		t.Fatalf("latest entry = %+v, want installed v2.0.0 metadata", available[0])
	}
	if available[1].Version != "v1.9.0" || available[1].IsLatest {
		t.Fatalf("first release = %+v, want non-latest v1.9.0", available[1])
	}
	if available[2].Version != "v2.0.0" || !available[2].Installed || !available[2].IsLatest {
		t.Fatalf("second release = %+v, want installed latest release", available[2])
	}
}

func TestGetAvailableVersionsDeterminesLatestByVersionWhenPublishedAtMissing(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v1.10.0", []byte("installed"), time.Now())
	releases := &fakeReleaseLister{releases: []GitHubRelease{
		{TagName: "v1.9.0"},
		{TagName: "v1.10.0"},
	}}
	service := NewService(dataDir, releases)

	available, err := service.GetAvailableVersions(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetAvailableVersions() error = %v", err)
	}

	if available[0].Version != "latest" || !available[0].Installed {
		t.Fatalf("latest entry = %+v, want installed v1.10.0 metadata", available[0])
	}
	if available[1].Version != "v1.9.0" || available[1].IsLatest {
		t.Fatalf("first release = %+v, want non-latest v1.9.0", available[1])
	}
	if available[2].Version != "v1.10.0" || !available[2].IsLatest {
		t.Fatalf("second release = %+v, want latest v1.10.0", available[2])
	}
}

func TestGetAvailableVersionsLatestAliasUsesLatestReleaseTagPathWhenNotInstalled(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v1.0.0", []byte("installed"), time.Now())
	releases := &fakeReleaseLister{releases: []GitHubRelease{
		{TagName: "v1.0.0", PublishedAt: "2026-01-01T00:00:00Z"},
		{TagName: "v2.0.0", PublishedAt: "2026-02-01T00:00:00Z"},
	}}
	service := NewService(dataDir, releases)

	available, err := service.GetAvailableVersions(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetAvailableVersions() error = %v", err)
	}

	wantPath := filepath.Join(dataDir, "bin", "versions", "v2.0.0", binaryName())
	if available[0].Version != "latest" || available[0].Installed || available[0].Path != wantPath {
		t.Fatalf("latest entry = %+v, want uninstalled alias path %q", available[0], wantPath)
	}
}

func TestGetAvailableVersionsLatestMixedPublishedAtFallsBackToVersion(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v9.9.9", []byte("installed"), time.Now())
	releases := &fakeReleaseLister{releases: []GitHubRelease{
		{TagName: "v1.0.0", PublishedAt: "2026-02-01T00:00:00Z"},
		{TagName: "v9.9.9"},
		{TagName: "v1.0.0", PublishedAt: "2026-01-01T00:00:00Z"},
	}}
	service := NewService(dataDir, releases)

	available, err := service.GetAvailableVersions(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetAvailableVersions() error = %v", err)
	}

	if available[0].Version != "latest" || !available[0].Installed {
		t.Fatalf("latest entry = %+v, want installed v9.9.9 selected by version fallback", available[0])
	}
	if available[1].Version != "v1.0.0" || available[1].IsLatest {
		t.Fatalf("first release = %+v, want non-latest v1.0.0", available[1])
	}
	if available[2].Version != "v9.9.9" || !available[2].IsLatest {
		t.Fatalf("second release = %+v, want latest v9.9.9", available[2])
	}
	if available[3].Version != "v1.0.0" || available[3].IsLatest {
		t.Fatalf("third release = %+v, want non-latest v1.0.0", available[3])
	}
}

func TestGetAvailableVersionsReturnsEmptyOnAPIFailure(t *testing.T) {
	service := NewService(t.TempDir(), &fakeReleaseLister{err: errors.New("api down")})
	available, err := service.GetAvailableVersions(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetAvailableVersions() error = %v", err)
	}
	if len(available) != 0 {
		t.Fatalf("available = %+v, want empty fallback", available)
	}
}

func TestIsVersionAvailableChecksExplicitAndActualLatestRelease(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v2.0.0", []byte("binary"), time.Now())
	service := NewService(dataDir, &fakeReleaseLister{releases: []GitHubRelease{{TagName: "v2.0.0"}}})

	explicit, err := service.IsVersionAvailable(context.Background(), "v2.0.0")
	if err != nil || !explicit {
		t.Fatalf("explicit available = %v, err = %v, want true nil", explicit, err)
	}
	latest, err := service.IsVersionAvailable(context.Background(), "latest")
	if err != nil || !latest {
		t.Fatalf("latest available = %v, err = %v, want true nil", latest, err)
	}
}

func TestIsVersionAvailableLatestDeterminesLatestFromOutOfOrderReleases(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v2.0.0", []byte("binary"), time.Now())
	service := NewService(dataDir, &fakeReleaseLister{releases: []GitHubRelease{
		{TagName: "v1.9.0", PublishedAt: "2026-01-01T00:00:00Z"},
		{TagName: "v2.0.0", PublishedAt: "2026-02-01T00:00:00Z"},
	}})

	latest, err := service.IsVersionAvailable(context.Background(), "latest")
	if err != nil || !latest {
		t.Fatalf("latest available = %v, err = %v, want true nil", latest, err)
	}
}

func TestIsVersionAvailableLatestReturnsFalseOnAPIFailure(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v2.0.0", []byte("binary"), time.Now())
	service := NewService(dataDir, &fakeReleaseLister{err: errors.New("api down")})

	available, err := service.IsVersionAvailable(context.Background(), "latest")
	if err != nil {
		t.Fatalf("IsVersionAvailable() error = %v", err)
	}
	if available {
		t.Fatalf("latest available = true, want false on API failure")
	}
}

func TestVersionPathValidationRejectsTraversal(t *testing.T) {
	dataDir := t.TempDir()
	service := NewService(dataDir, nil)
	outsideDir := filepath.Join(dataDir, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	marker := filepath.Join(outsideDir, "marker.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := service.RemoveVersion(filepath.Join("..", "..", "outside")); err == nil {
		t.Fatalf("RemoveVersion(traversal) error = nil, want error")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("outside marker removed or inaccessible: %v", err)
	}
	if _, err := service.GetVersionBinaryPathSafe(filepath.Join("..", "..", "outside")); err == nil {
		t.Fatalf("GetVersionBinaryPathSafe(traversal) error = nil, want error")
	}
	available, err := service.IsVersionAvailable(context.Background(), filepath.Join("..", "..", "outside"))
	if err == nil || available {
		t.Fatalf("IsVersionAvailable(traversal) = %v, %v; want false error", available, err)
	}
}

func TestGetVersionsSizeReturnsInstalledSizes(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v1.0.0", []byte("123"), time.Now())
	writeBinary(t, dataDir, "v2.0.0", []byte("12345"), time.Now())
	service := NewService(dataDir, nil)

	sizes, err := service.GetVersionsSize()
	if err != nil {
		t.Fatalf("GetVersionsSize() error = %v", err)
	}
	if sizes["v1.0.0"] != 3 || sizes["v2.0.0"] != 5 {
		t.Fatalf("sizes = %+v, want v1.0.0=3 v2.0.0=5", sizes)
	}
}

func TestRemoveVersionRejectsLatestAndActiveVersion(t *testing.T) {
	dataDir := t.TempDir()
	service := NewService(dataDir, nil)
	service.ActiveVersion = func() string { return "v1.0.0" }
	writeBinary(t, dataDir, "v1.0.0", []byte("binary"), time.Now())

	if err := service.RemoveVersion("latest"); err == nil {
		t.Fatalf("RemoveVersion(latest) error = nil, want error")
	}
	if err := service.RemoveVersion("v1.0.0"); err == nil {
		t.Fatalf("RemoveVersion(active) error = nil, want error")
	}
}

func TestCleanupKeepsNewestByInstalledAtAndProtectsActiveVersion(t *testing.T) {
	dataDir := t.TempDir()
	service := NewService(dataDir, nil)
	service.ActiveVersion = func() string { return "v1.0.0" }
	base := time.Now()
	writeBinary(t, dataDir, "v1.0.0", []byte("active"), base.Add(-3*time.Hour))
	writeBinary(t, dataDir, "v2.0.0", []byte("newest"), base)
	writeBinary(t, dataDir, "v1.5.0", []byte("old"), base.Add(-2*time.Hour))

	removed, err := service.Cleanup(1)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "v1.5.0" {
		t.Fatalf("removed = %+v, want [v1.5.0]", removed)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "versions", "v1.0.0", binaryName())); err != nil {
		t.Fatalf("active binary removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "versions", "v2.0.0", binaryName())); err != nil {
		t.Fatalf("newest binary removed: %v", err)
	}
}

func writeBinary(t *testing.T, dataDir, version string, contents []byte, installedAt time.Time) {
	t.Helper()
	path := filepath.Join(dataDir, "bin", "versions", version, binaryName())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chtimes(path, installedAt, installedAt); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "gowa.exe"
	}
	return "gowa"
}
