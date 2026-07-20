package versions

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallerDownloadsSelectsAssetExtractsAndRenamesAtomically(t *testing.T) {
	dataDir := t.TempDir()
	zipBody := makeZip(t, map[string][]byte{binaryName(): []byte("installed-binary")})
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write(zipBody)
	}))
	t.Cleanup(server.Close)

	installer := NewInstaller(dataDir, &fakeReleaseLister{releases: []GitHubRelease{{TagName: "v1.2.3", Assets: []GitHubAsset{
		{Name: "gowa-linux-amd64.zip", BrowserDownloadURL: server.URL},
		{Name: assetNameForRuntimeInstaller(), BrowserDownloadURL: server.URL},
	}}}}, server.Client())

	result, err := installer.Install(context.Background(), "v1.2.3")
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Version != "v1.2.3" || result.Path != filepath.Join(dataDir, "bin", "versions", "v1.2.3", binaryName()) || result.SHA256 == "" || result.Size != int64(len("installed-binary")) {
		t.Fatalf("result = %+v, want version path checksum and installed binary size", result)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	contents, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("ReadFile(installed) error = %v", err)
	}
	if string(contents) != "installed-binary" {
		t.Fatalf("installed contents = %q", contents)
	}
	if runtime.GOOS == "linux" {
		info, err := os.Stat(result.Path)
		if err != nil {
			t.Fatalf("Stat(installed) error = %v", err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("installed mode = %v, want executable bit", info.Mode())
		}
	}
	assertNoInstallerTemps(t, dataDir)
}

func TestInstallerIsIdempotentWhenVersionBinaryExists(t *testing.T) {
	dataDir := t.TempDir()
	writeBinary(t, dataDir, "v1.2.3", []byte("existing"), time.Now())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("installer redownloaded existing version")
	}))
	t.Cleanup(server.Close)
	installer := NewInstaller(dataDir, &fakeReleaseLister{releases: []GitHubRelease{{TagName: "v1.2.3", Assets: []GitHubAsset{{Name: assetNameForRuntimeInstaller(), BrowserDownloadURL: server.URL}}}}}, server.Client())

	result, err := installer.Install(context.Background(), "v1.2.3")
	if err != nil {
		t.Fatalf("Install(existing) error = %v", err)
	}
	if !result.AlreadyInstalled {
		t.Fatalf("AlreadyInstalled = false, want true")
	}
	contents, err := os.ReadFile(result.Path)
	if err != nil || string(contents) != "existing" {
		t.Fatalf("installed contents = %q, err = %v; want existing", contents, err)
	}
}

func TestInstallerResolvesLatestAliasToActualTag(t *testing.T) {
	dataDir := t.TempDir()
	zipBody := makeZip(t, map[string][]byte{binaryName(): []byte("latest-binary")})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBody)
	}))
	t.Cleanup(server.Close)

	// The release list returns v9.0.0 as the latest. Installing "latest"
	// should resolve to v9.0.0 and place the binary under that tag's directory.
	installer := NewInstaller(dataDir, &fakeReleaseLister{releases: []GitHubRelease{
		{TagName: "v9.0.0", Assets: []GitHubAsset{{Name: assetNameForRuntimeInstaller(), BrowserDownloadURL: server.URL}}},
		{TagName: "v8.11.0", Assets: []GitHubAsset{{Name: assetNameForRuntimeInstaller(), BrowserDownloadURL: server.URL}}},
	}}, server.Client())

	result, err := installer.Install(context.Background(), "latest")
	if err != nil {
		t.Fatalf("Install(latest) error = %v", err)
	}
	if result.Version != "v9.0.0" {
		t.Fatalf("result.Version = %q, want v9.0.0", result.Version)
	}
	wantPath := filepath.Join(dataDir, "bin", "versions", "v9.0.0", binaryName())
	if result.Path != wantPath {
		t.Fatalf("result.Path = %q, want %q", result.Path, wantPath)
	}
	contents, err := os.ReadFile(result.Path)
	if err != nil || string(contents) != "latest-binary" {
		t.Fatalf("installed contents = %q, err = %v; want latest-binary", contents, err)
	}
}

func TestInstallerLatestAliasFailsWhenNoReleases(t *testing.T) {
	dataDir := t.TempDir()
	installer := NewInstaller(dataDir, &fakeReleaseLister{releases: nil}, nil)

	if _, err := installer.Install(context.Background(), "latest"); err == nil {
		t.Fatalf("Install(latest) with no releases error = nil, want error")
	}
}

func TestInstallerRejectsZipSlipAndCleansStaging(t *testing.T) {
	dataDir := t.TempDir()
	outside := filepath.Join(dataDir, "pwned")
	installer := installerWithZip(t, dataDir, makeZip(t, map[string][]byte{filepath.Join("..", "pwned"): []byte("bad"), binaryName(): []byte("ok")}))

	if _, err := installer.Install(context.Background(), "v1.2.3"); err == nil {
		t.Fatalf("Install(zip slip) error = nil, want error")
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "versions", "v1.2.3", binaryName())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final binary stat err = %v, want not exist", err)
	}
	assertNoInstallerTemps(t, dataDir)
}

func TestInstallerRejectsWindowsStyleZipSlipAndCleansStaging(t *testing.T) {
	dataDir := t.TempDir()
	outside := filepath.Join(dataDir, "evil")
	installer := installerWithZip(t, dataDir, makeZip(t, map[string][]byte{"..\\evil": []byte("bad"), binaryName(): []byte("ok")}))

	if _, err := installer.Install(context.Background(), "v1.2.3"); err == nil {
		t.Fatalf("Install(windows zip slip) error = nil, want error")
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "versions", "v1.2.3", binaryName())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final binary stat err = %v, want not exist", err)
	}
	assertNoInstallerTemps(t, dataDir)
}

func TestInstallerRejectsSpecialZipEntriesAndCleansStaging(t *testing.T) {
	dataDir := t.TempDir()
	installer := installerWithZip(t, dataDir, makeZipWithSymlink(t, "link", "target"))

	if _, err := installer.Install(context.Background(), "v1.2.3"); err == nil {
		t.Fatalf("Install(symlink entry) error = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "versions", "v1.2.3", binaryName())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final binary stat err = %v, want not exist", err)
	}
	assertNoInstallerTemps(t, dataDir)
}

func TestInstallerRejectsMissingExpectedBinaryNameAndCleansStaging(t *testing.T) {
	dataDir := t.TempDir()
	installer := installerWithZip(t, dataDir, makeZip(t, map[string][]byte{"not-gowa": []byte("nope")}))

	if _, err := installer.Install(context.Background(), "v1.2.3"); err == nil {
		t.Fatalf("Install(missing binary) error = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "versions", "v1.2.3", binaryName())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final binary stat err = %v, want not exist", err)
	}
	assertNoInstallerTemps(t, dataDir)
}

func TestInstallerCleansInterruptedDownloadAndDoesNotExposeToken(t *testing.T) {
	dataDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("partial"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(server.Close)
	installer := NewInstaller(dataDir, &fakeReleaseLister{releases: []GitHubRelease{{TagName: "v1.2.3", Assets: []GitHubAsset{{Name: assetNameForRuntimeInstaller(), BrowserDownloadURL: server.URL + "?token=secret-token"}}}}}, server.Client())

	_, err := installer.Install(context.Background(), "v1.2.3")
	if err == nil {
		t.Fatalf("Install(interrupted) error = nil, want error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked token: %v", err)
	}
	assertNoInstallerTemps(t, dataDir)
}

func TestInstallerRemoveAndCleanupKeepCount(t *testing.T) {
	dataDir := t.TempDir()
	installer := NewInstaller(dataDir, nil, nil)
	base := time.Now()
	writeBinary(t, dataDir, "v1.0.0", []byte("old"), base.Add(-3*time.Hour))
	writeBinary(t, dataDir, "v2.0.0", []byte("keep"), base)
	writeBinary(t, dataDir, "v1.5.0", []byte("remove"), base.Add(-2*time.Hour))

	if err := installer.Remove("v1.0.0"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "versions", "v1.0.0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed dir stat err = %v, want not exist", err)
	}
	removed, err := installer.Cleanup(1)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "v1.5.0" {
		t.Fatalf("removed = %+v, want [v1.5.0]", removed)
	}
}

func TestInstallerCleanupProtectsActiveVersionWithSmallKeepCount(t *testing.T) {
	dataDir := t.TempDir()
	installer := NewInstaller(dataDir, nil, nil)
	installer.ActiveVersion = "v1.0.0"
	base := time.Now()
	writeBinary(t, dataDir, "v1.0.0", []byte("active"), base.Add(-3*time.Hour))
	writeBinary(t, dataDir, "v2.0.0", []byte("newest"), base)
	writeBinary(t, dataDir, "v1.5.0", []byte("old"), base.Add(-2*time.Hour))

	removed, err := installer.Cleanup(1)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "v1.5.0" {
		t.Fatalf("removed = %+v, want [v1.5.0]", removed)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "versions", "v1.0.0", binaryName())); err != nil {
		t.Fatalf("active binary removed: %v", err)
	}
}

func installerWithZip(t *testing.T, dataDir string, zipBody []byte) *VersionInstaller {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBody)
	}))
	t.Cleanup(server.Close)
	return NewInstaller(dataDir, &fakeReleaseLister{releases: []GitHubRelease{{TagName: "v1.2.3", Assets: []GitHubAsset{{Name: assetNameForRuntimeInstaller(), BrowserDownloadURL: server.URL}}}}}, server.Client())
}

func makeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "release-*.zip")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	zw := zip.NewWriter(tmp)
	for name, contents := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip Create(%q) error = %v", name, err)
		}
		if _, err := w.Write(contents); err != nil {
			t.Fatalf("zip Write(%q) error = %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close() error = %v", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	body, err := io.ReadAll(tmp)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return body
}

func makeZipWithSymlink(t *testing.T, name, target string) []byte {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "release-*.zip")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	zw := zip.NewWriter(tmp)
	header := &zip.FileHeader{Name: name}
	header.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatalf("zip CreateHeader(%q) error = %v", name, err)
	}
	if _, err := w.Write([]byte(target)); err != nil {
		t.Fatalf("zip Write(%q) error = %v", name, err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close() error = %v", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	body, err := io.ReadAll(tmp)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return body
}

func assetNameForRuntimeInstaller() string {
	if runtime.GOOS == "windows" {
		return "gowa-windows-amd64.zip"
	}
	return "gowa-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
}

func assertNoInstallerTemps(t *testing.T, dataDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dataDir, "bin", "versions", ".install-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("installer temp paths remain: %v", matches)
	}
}
