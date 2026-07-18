package versions

import "time"

type VersionInfo struct {
	Version     string
	Path        string
	Installed   bool
	IsLatest    bool
	Size        int64
	InstalledAt time.Time
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	PublishedAt string        `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
}
