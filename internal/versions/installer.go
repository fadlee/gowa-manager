package versions

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	maxDownloadSize  = 256 << 20
	maxExtractedSize = 512 << 20
)

type InstallResult struct {
	Version          string
	Path             string
	SHA256           string
	Size             int64
	AlreadyInstalled bool
}

type VersionInstaller struct {
	dataDir       string
	releases      ReleaseLister
	client        *http.Client
	ActiveVersion string
}

func NewInstaller(dataDir string, releases ReleaseLister, client *http.Client) *VersionInstaller {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	} else if client.Timeout == 0 {
		copy := *client
		copy.Timeout = 30 * time.Second
		client = &copy
	}
	if releases == nil {
		releases = NewGitHubClient("", client)
	}
	return &VersionInstaller{dataDir: dataDir, releases: releases, client: client}
}

func (i *VersionInstaller) Install(ctx context.Context, version string) (InstallResult, error) {
	// Resolve the "latest" alias to the actual latest release tag from GitHub,
	// matching the Bun backend behaviour where "latest" fetches /releases/latest
	// and installs whatever tag is returned.
	if version == "latest" {
		releases, err := i.releases.ListReleases(ctx, 1)
		if err != nil {
			return InstallResult{}, fmt.Errorf("failed to resolve latest version: %w", err)
		}
		if len(releases) == 0 {
			return InstallResult{}, errors.New("no releases found to resolve latest version")
		}
		version = latestReleaseTag(releases)
		if version == "" {
			return InstallResult{}, errors.New("could not determine latest version tag")
		}
	}

	finalPath, err := i.versionBinaryPath(version)
	if err != nil {
		return InstallResult{}, err
	}
	if info, err := os.Stat(finalPath); err == nil && !info.IsDir() {
		return InstallResult{Version: version, Path: finalPath, Size: info.Size(), AlreadyInstalled: true}, nil
	}

	releases, err := i.releases.ListReleases(ctx, 20)
	if err != nil {
		return InstallResult{}, err
	}
	asset, ok := findReleaseAsset(releases, version)
	if !ok {
		return InstallResult{}, fmt.Errorf("no release asset for %s on %s/%s", version, runtime.GOOS, runtime.GOARCH)
	}

	versionsDir := i.versionsDir()
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		return InstallResult{}, err
	}
	tmpDir, err := os.MkdirTemp(versionsDir, ".install-"+version+"-")
	if err != nil {
		return InstallResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, "release.zip")
	checksum, _, err := i.download(ctx, asset.BrowserDownloadURL, zipPath)
	if err != nil {
		return InstallResult{}, err
	}
	stageDir := filepath.Join(tmpDir, "stage")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return InstallResult{}, err
	}
	if err := extractZip(zipPath, stageDir); err != nil {
		return InstallResult{}, err
	}

	binaryPath := filepath.Join(stageDir, binaryNameForRuntime())
	if info, err := os.Stat(binaryPath); err != nil || info.IsDir() {
		return InstallResult{}, fmt.Errorf("archive missing expected binary %s", binaryNameForRuntime())
	}
	if runtime.GOOS == "linux" {
		if err := os.Chmod(binaryPath, 0o755); err != nil {
			return InstallResult{}, err
		}
	}
	binaryInfo, err := os.Stat(binaryPath)
	if err != nil {
		return InstallResult{}, err
	}

	finalDir := filepath.Dir(finalPath)
	if err := os.Rename(stageDir, finalDir); err != nil {
		if info, statErr := os.Stat(finalPath); statErr == nil && !info.IsDir() {
			return InstallResult{Version: version, Path: finalPath, Size: info.Size(), AlreadyInstalled: true}, nil
		}
		return InstallResult{}, err
	}
	return InstallResult{Version: version, Path: finalPath, SHA256: checksum, Size: binaryInfo.Size()}, nil
}

func (i *VersionInstaller) Remove(version string) error {
	dir, err := i.versionDir(version)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func (i *VersionInstaller) Cleanup(keepCount int) ([]string, error) {
	installed, err := i.installedVersions()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(installed, func(a, b int) bool { return installed[a].InstalledAt.After(installed[b].InstalledAt) })
	removed := []string{}
	for idx, version := range installed {
		if version.Version == i.ActiveVersion {
			continue
		}
		if idx < keepCount {
			continue
		}
		if err := i.Remove(version.Version); err != nil {
			return removed, err
		}
		removed = append(removed, version.Version)
	}
	return removed, nil
}

func (i *VersionInstaller) download(ctx context.Context, downloadURL, dest string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := i.client.Do(req)
	if err != nil {
		return "", 0, sanitizeDownloadError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("download status %d", resp.StatusCode)
	}
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return "", written, sanitizeDownloadError(err)
	}
	if written > maxDownloadSize {
		return "", written, errors.New("download exceeds maximum size")
	}
	if resp.ContentLength >= 0 && written != resp.ContentLength {
		return "", written, errors.New("download interrupted")
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func extractZip(zipPath, dest string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	base, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	total := uint64(0)
	for _, file := range reader.File {
		if strings.Contains(file.Name, "\\") {
			return fmt.Errorf("archive path contains unsupported separator: %s", file.Name)
		}
		name := filepath.Clean(file.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, "..") || strings.Contains(name, string(filepath.Separator)+".."+string(filepath.Separator)) {
			return fmt.Errorf("archive path escapes staging: %s", file.Name)
		}
		target, err := filepath.Abs(filepath.Join(base, name))
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, target)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("archive path escapes staging: %s", file.Name)
		}
		mode := file.FileInfo().Mode()
		if mode.Type() != 0 && !mode.IsDir() {
			return fmt.Errorf("archive contains unsupported file type: %s", file.Name)
		}
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += file.UncompressedSize64
		if total > maxExtractedSize {
			return errors.New("archive exceeds maximum extracted size")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.Mode())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(in, int64(file.UncompressedSize64)+1))
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
	}
	return nil
}

func findReleaseAsset(releases []GitHubRelease, version string) (GitHubAsset, bool) {
	for _, release := range releases {
		if release.TagName != version {
			continue
		}
		return SelectAssetForPlatform(release.Assets, runtime.GOOS, runtime.GOARCH)
	}
	return GitHubAsset{}, false
}

func sanitizeDownloadError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("download failed: %T", err)
}

func (i *VersionInstaller) installedVersions() ([]VersionInfo, error) {
	entries, err := os.ReadDir(i.versionsDir())
	if errors.Is(err, os.ErrNotExist) {
		return []VersionInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	installed := []VersionInfo{}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".install-") {
			continue
		}
		path := filepath.Join(i.versionsDir(), entry.Name(), binaryNameForRuntime())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		installed = append(installed, VersionInfo{Version: entry.Name(), Path: path, Installed: true, Size: info.Size(), InstalledAt: info.ModTime()})
	}
	return installed, nil
}

func (i *VersionInstaller) versionsDir() string {
	return filepath.Join(i.dataDir, "bin", "versions")
}

func (i *VersionInstaller) versionBinaryPath(version string) (string, error) {
	dir, err := i.versionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, binaryNameForRuntime()), nil
}

func (i *VersionInstaller) versionDir(version string) (string, error) {
	service := NewService(i.dataDir, i.releases)
	return service.versionDir(version)
}
