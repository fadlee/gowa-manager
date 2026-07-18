package versions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

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
	if version == "latest" {
		if latest := s.resolveLatestVersion(); latest != "" {
			version = latest
		} else {
			return filepath.Join(s.dataDir, "bin", binaryNameForRuntime())
		}
	}
	return filepath.Join(s.versionsDir(), version, binaryNameForRuntime())
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
	latestTag := releases[0].TagName
	latestInfo, latestInstalled := installedByVersion[latestTag]
	result = append(result, VersionInfo{Version: "latest", Path: s.GetVersionBinaryPath("latest"), Installed: latestInstalled, IsLatest: true, Size: latestInfo.Size, InstalledAt: latestInfo.InstalledAt})
	for i, release := range releases {
		info, ok := installedByVersion[release.TagName]
		entry := VersionInfo{Version: release.TagName, Path: s.GetVersionBinaryPath(release.TagName), Installed: ok, IsLatest: i == 0}
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
		releases, err := s.releases.ListReleases(ctx, 1)
		if err != nil || len(releases) == 0 {
			return false, nil
		}
		_, err = os.Stat(s.GetVersionBinaryPath(releases[0].TagName))
		return err == nil, nil
	}
	_, err := os.Stat(s.GetVersionBinaryPath(version))
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
	if s.activeVersion() == version {
		return fmt.Errorf("cannot remove active version %s", version)
	}
	return os.RemoveAll(filepath.Join(s.versionsDir(), version))
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
