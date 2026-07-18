package versions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestGitHubClientFetchesReleasesWithLimitAndHeaders(t *testing.T) {
	var gotPerPage string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPerPage = r.URL.Query().Get("per_page")
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatalf("Accept header = %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("User-Agent") == "" {
			t.Fatalf("User-Agent header is empty")
		}
		_ = json.NewEncoder(w).Encode([]GitHubRelease{
			{TagName: "v1.2.0", PublishedAt: "2026-01-02T03:04:05Z", Assets: []GitHubAsset{{Name: assetNameForRuntime(), BrowserDownloadURL: "https://example.test/gowa", Size: 42}}},
		})
	}))
	t.Cleanup(server.Close)

	client := NewGitHubClient(server.URL, server.Client())
	releases, err := client.ListReleases(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListReleases() error = %v", err)
	}

	if gotPerPage != strconv.Itoa(7) {
		t.Fatalf("per_page = %q, want 7", gotPerPage)
	}
	if len(releases) != 1 || releases[0].TagName != "v1.2.0" {
		t.Fatalf("releases = %+v, want v1.2.0", releases)
	}
}

func TestGitHubClientReturnsErrorForAPIStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	client := NewGitHubClient(server.URL, server.Client())
	if _, err := client.ListReleases(context.Background(), 1); err == nil {
		t.Fatalf("ListReleases() error = nil, want error")
	}
}

func TestSelectAssetForPlatform(t *testing.T) {
	assets := []GitHubAsset{
		{Name: "gowa-linux-arm64.tar.gz", BrowserDownloadURL: "arm64"},
		{Name: "gowa-windows-amd64.zip", BrowserDownloadURL: "win"},
		{Name: "gowa-linux-amd64.tar.gz", BrowserDownloadURL: "linux"},
	}

	tests := []struct {
		name string
		goos string
		arch string
		want string
		ok   bool
	}{
		{name: "linux amd64", goos: "linux", arch: "amd64", want: "linux", ok: true},
		{name: "linux arm64", goos: "linux", arch: "arm64", want: "arm64", ok: true},
		{name: "windows amd64", goos: "windows", arch: "amd64", want: "win", ok: true},
		{name: "windows arm64 unsupported", goos: "windows", arch: "arm64"},
		{name: "darwin amd64 unsupported", goos: "darwin", arch: "amd64"},
		{name: "linux arm unsupported", goos: "linux", arch: "arm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset, ok := SelectAssetForPlatform(assets, tt.goos, tt.arch)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if asset.BrowserDownloadURL != tt.want {
				t.Fatalf("download URL = %q, want %q", asset.BrowserDownloadURL, tt.want)
			}
		})
	}
}

func assetNameForRuntime() string {
	return "gowa-linux-amd64.tar.gz"
}
