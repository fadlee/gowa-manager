package versions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var versionTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9.-]*)?$`)

type Installer interface {
	Install(context.Context, string) error
}

type Service struct {
	dataDir       string
	releases      ReleaseLister
	ActiveVersion func() string
	installer     Installer
}

func NewService(dataDir string, releases ReleaseLister) *Service {
	if releases == nil {
		releases = NewGitHubClient("", nil)
	}
	return &Service{dataDir: dataDir, releases: releases}
}

func (s *Service) GetVersionBinaryPath(version string) string {
	path, err := s.GetVersionBinaryPathSafe(version)
	if err != nil {
		return filepath.Join(s.versionsDir(), "invalid", binaryNameForRuntime())
	}
	return path
}

func (s *Service) GetVersionBinaryPathSafe(version string) (string, error) {
	if version == "latest" {
		if latest := s.resolveLatestVersion(); latest != "" {
			version = latest
		} else {
			return filepath.Join(s.dataDir, "bin", binaryNameForRuntime()), nil
		}
	}
	return s.versionBinaryPath(version)
}

func (s *Service) GetInstalledVersions() ([]VersionInfo, error) {
	entries, err := os.ReadDir(s.versionsDir())
	if errors.Is(err, os.ErrNotExist) {
		return []VersionInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	latest := s.resolveLatestVersionFromEntries(entries)
	installed := []VersionInfo{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "latest" {
			continue
		}
		path := filepath.Join(s.versionsDir(), entry.Name(), binaryNameForRuntime())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		installed = append(installed, VersionInfo{Version: entry.Name(), Path: path, Installed: true, IsLatest: entry.Name() == latest, Size: info.Size(), InstalledAt: info.ModTime()})
	}
	sort.Slice(installed, func(i, j int) bool { return compareVersions(installed[i].Version, installed[j].Version) > 0 })
	return installed, nil
}

func (s *Service) GetAvailableVersions(ctx context.Context, limit int) ([]VersionInfo, error) {
	releases, err := s.releases.ListReleases(ctx, limit)
	if err != nil || len(releases) == 0 {
		return []VersionInfo{}, nil
	}
	installed, err := s.GetInstalledVersions()
	if err != nil {
		return nil, err
	}
	installedByVersion := map[string]VersionInfo{}
	for _, version := range installed {
		installedByVersion[version.Version] = version
	}

	result := []VersionInfo{}
	latestTag := latestReleaseTag(releases)
	latestInfo, latestInstalled := installedByVersion[latestTag]
	latestPath, err := s.versionBinaryPath(latestTag)
	if err != nil {
		return nil, err
	}
	result = append(result, VersionInfo{Version: "latest", Path: latestPath, Installed: latestInstalled, IsLatest: true, Size: latestInfo.Size, InstalledAt: latestInfo.InstalledAt})
	for _, release := range releases {
		info, ok := installedByVersion[release.TagName]
		entry := VersionInfo{Version: release.TagName, Path: s.GetVersionBinaryPath(release.TagName), Installed: ok, IsLatest: release.TagName == latestTag}
		if ok {
			entry.Size = info.Size
			entry.InstalledAt = info.InstalledAt
		}
		result = append(result, entry)
	}
	return result, nil
}

func (s *Service) IsVersionAvailable(ctx context.Context, version string) (bool, error) {
	if version == "latest" {
		releases, err := s.releases.ListReleases(ctx, 10)
		if err != nil || len(releases) == 0 {
			return false, nil
		}
		path, err := s.versionBinaryPath(latestReleaseTag(releases))
		if err != nil {
			return false, err
		}
		_, err = os.Stat(path)
		return err == nil, nil
	}
	path, err := s.versionBinaryPath(version)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	return err == nil, nil
}

func (s *Service) GetVersionsSize() (map[string]int64, error) {
	installed, err := s.GetInstalledVersions()
	if err != nil {
		return nil, err
	}
	sizes := map[string]int64{}
	for _, version := range installed {
		if version.Size > 0 {
			sizes[version.Version] = version.Size
		}
	}
	return sizes, nil
}

func (s *Service) RemoveVersion(version string) error {
	if version == "latest" {
		return errors.New("cannot remove the latest version alias")
	}
	path, err := s.versionDir(version)
	if err != nil {
		return err
	}
	if s.activeVersion() == version {
		return fmt.Errorf("cannot remove active version %s", version)
	}
	return os.RemoveAll(path)
}

func (s *Service) Cleanup(keepCount int) ([]string, error) {
	installed, err := s.GetInstalledVersions()
	if err != nil {
		return nil, err
	}
	active := s.activeVersion()
	sort.SliceStable(installed, func(i, j int) bool { return installed[i].InstalledAt.After(installed[j].InstalledAt) })
	removed := []string{}
	kept := 0
	for _, version := range installed {
		if version.Version == active {
			continue
		}
		if kept < keepCount {
			kept++
			continue
		}
		if err := s.RemoveVersion(version.Version); err != nil {
			return removed, err
		}
		removed = append(removed, version.Version)
	}
	return removed, nil
}

func (s *Service) versionsDir() string {
	return filepath.Join(s.dataDir, "bin", "versions")
}

func (s *Service) versionBinaryPath(version string) (string, error) {
	dir, err := s.versionDir(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, binaryNameForRuntime()), nil
}

func (s *Service) versionDir(version string) (string, error) {
	if err := validateVersionTag(version); err != nil {
		return "", err
	}
	base, err := filepath.Abs(s.versionsDir())
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(base, version))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("version path escapes versions directory: %q", version)
	}
	return path, nil
}

func validateVersionTag(version string) error {
	if version == "" {
		return errors.New("version cannot be empty")
	}
	if filepath.IsAbs(version) || strings.ContainsAny(version, `/\`) || strings.Contains(version, "..") {
		return fmt.Errorf("invalid version tag: %q", version)
	}
	if !versionTagPattern.MatchString(version) {
		return fmt.Errorf("invalid version tag: %q", version)
	}
	return nil
}

func (s *Service) resolveLatestVersion() string {
	entries, err := os.ReadDir(s.versionsDir())
	if err != nil {
		return ""
	}
	return s.resolveLatestVersionFromEntries(entries)
}

func (s *Service) resolveLatestVersionFromEntries(entries []os.DirEntry) string {
	versions := []string{}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "latest" {
			if _, err := os.Stat(filepath.Join(s.versionsDir(), entry.Name(), binaryNameForRuntime())); err == nil {
				versions = append(versions, entry.Name())
			}
		}
	}
	if len(versions) == 0 {
		return ""
	}
	sort.Slice(versions, func(i, j int) bool { return compareVersions(versions[i], versions[j]) > 0 })
	return versions[0]
}

func (s *Service) activeVersion() string {
	if s.ActiveVersion == nil {
		return ""
	}
	return s.ActiveVersion()
}

func binaryNameForRuntime() string {
	if runtime.GOOS == "windows" {
		return "gowa.exe"
	}
	return "gowa"
}

func compareVersions(a, b string) int {
	pa := versionParts(a)
	pb := versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		av, bv := 0, 0
		if i < len(pa) {
			av = pa[i]
		}
		if i < len(pb) {
			bv = pb[i]
		}
		if av != bv {
			return av - bv
		}
	}
	return strings.Compare(a, b)
}

func latestReleaseTag(releases []GitHubRelease) string {
	if len(releases) == 0 {
		return ""
	}
	latest := releases[0]
	latestPublishedAt := parsePublishedAt(latest.PublishedAt)
	for _, release := range releases[1:] {
		publishedAt := parsePublishedAt(release.PublishedAt)
		if !publishedAt.IsZero() || !latestPublishedAt.IsZero() {
			if latestPublishedAt.IsZero() || (!publishedAt.IsZero() && publishedAt.After(latestPublishedAt)) {
				latest = release
				latestPublishedAt = publishedAt
			}
			continue
		}
		if compareVersions(release.TagName, latest.TagName) > 0 {
			latest = release
		}
	}
	return latest.TagName
}

func parsePublishedAt(value string) time.Time {
	publishedAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return publishedAt
}

func versionParts(version string) []int {
	parts := []int{}
	current := 0
	inNumber := false
	for _, r := range version {
		if r >= '0' && r <= '9' {
			current = current*10 + int(r-'0')
			inNumber = true
			continue
		}
		if inNumber {
			parts = append(parts, current)
			current = 0
			inNumber = false
		}
	}
	if inNumber {
		parts = append(parts, current)
	}
	return parts
}
