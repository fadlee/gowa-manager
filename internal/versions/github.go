package versions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultGitHubBaseURL = "https://api.github.com/repos/aldinokemal/go-whatsapp-web-multidevice/releases"

type ReleaseLister interface {
	ListReleases(context.Context, int) ([]GitHubRelease, error)
}

type GitHubClient struct {
	baseURL string
	client  *http.Client
}

func NewGitHubClient(baseURL string, client *http.Client) *GitHubClient {
	if baseURL == "" {
		baseURL = defaultGitHubBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	} else if client.Timeout == 0 {
		copy := *client
		copy.Timeout = 10 * time.Second
		client = &copy
	}
	return &GitHubClient{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (c *GitHubClient) ListReleases(ctx context.Context, limit int) ([]GitHubRelease, error) {
	if limit <= 0 {
		limit = 10
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("per_page", fmt.Sprintf("%d", limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gowa-manager")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github releases status %d", resp.StatusCode)
	}

	var releases []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func SelectAssetForPlatform(assets []GitHubAsset, goos, goarch string) (GitHubAsset, bool) {
	if goos == "windows" && goarch == "amd64" {
		return selectAsset(assets, "windows", "amd64")
	}
	if goos == "linux" && (goarch == "amd64" || goarch == "arm64") {
		return selectAsset(assets, "linux", goarch)
	}
	return GitHubAsset{}, false
}

func selectAsset(assets []GitHubAsset, osName, arch string) (GitHubAsset, bool) {
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, osName) && strings.Contains(name, arch) {
			return asset, true
		}
	}
	return GitHubAsset{}, false
}
