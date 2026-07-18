package instances

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultDeviceCacheTTL = 15 * time.Second
	defaultDeviceTimeout  = 3 * time.Second
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type DeviceClientOptions struct {
	HTTPClient *http.Client
	Clock      Clock
	CacheTTL   time.Duration
	Timeout    time.Duration
}

type DeviceClient struct {
	httpClient *http.Client
	clock      Clock
	cacheTTL   time.Duration
	timeout    time.Duration

	mu    sync.Mutex
	cache map[int64]*deviceCacheEntry
}

type deviceCacheEntry struct {
	mu       sync.Mutex
	response DevicesResponse
	fetched  time.Time
}

type DevicesResponse struct {
	Count     int              `json:"count"`
	Connected bool             `json:"connected"`
	Stale     bool             `json:"stale"`
	Devices   []map[string]any `json:"devices"`
	Source    string           `json:"source"`
	FetchedAt time.Time        `json:"fetchedAt"`
	Error     string           `json:"error,omitempty"`
}

type DeviceSummary struct {
	Count     int       `json:"count"`
	Connected bool      `json:"connected"`
	Stale     bool      `json:"stale"`
	FetchedAt time.Time `json:"fetchedAt"`
	Error     string    `json:"error,omitempty"`
	Source    string    `json:"source"`
}

func NewDeviceClient(options DeviceClientOptions) *DeviceClient {
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	clock := options.Clock
	if clock == nil {
		clock = realClock{}
	}
	cacheTTL := options.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = defaultDeviceCacheTTL
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultDeviceTimeout
	}
	return &DeviceClient{httpClient: client, clock: clock, cacheTTL: cacheTTL, timeout: timeout, cache: map[int64]*deviceCacheEntry{}}
}

func (c *DeviceClient) Summary(ctx context.Context, instance Instance) (DeviceSummary, error) {
	response, err := c.Fetch(ctx, instance)
	return DeviceSummary{Count: response.Count, Connected: response.Connected, Stale: response.Stale, FetchedAt: response.FetchedAt, Error: response.Error, Source: response.Source}, err
}

func (c *DeviceClient) Fetch(ctx context.Context, instance Instance) (DevicesResponse, error) {
	if !isRunningWithPort(instance) {
		return DevicesResponse{Devices: []map[string]any{}, Source: "not-running"}, nil
	}
	entry := c.entry(instance.ID)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	now := c.clock.Now()
	if !entry.fetched.IsZero() && now.Sub(entry.fetched) < c.cacheTTL {
		cached := entry.response
		cached.Source = "cache"
		cached.Stale = false
		cached.Error = ""
		return cached, nil
	}

	response, err := c.fetchLive(ctx, instance, now)
	if err != nil {
		if !entry.fetched.IsZero() {
			cached := entry.response
			cached.Source = "cache"
			cached.Stale = true
			cached.Error = err.Error()
			return cached, err
		}
		response.Source = "live"
		response.Error = err.Error()
		return response, err
	}
	entry.response = response
	entry.fetched = now
	return response, nil
}

func (c *DeviceClient) ClearCache(instanceID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, instanceID)
}

func (c *DeviceClient) ClearAllCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[int64]*deviceCacheEntry{}
}

func (c *DeviceClient) entry(instanceID int64) *deviceCacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.cache[instanceID]
	if entry == nil {
		entry = &deviceCacheEntry{}
		c.cache[instanceID] = entry
	}
	return entry
}

func (c *DeviceClient) fetchLive(ctx context.Context, instance Instance, fetchedAt time.Time) (DevicesResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, devicesURL(instance), nil)
	if err != nil {
		return DevicesResponse{}, err
	}
	applyCommonHeaders(req, instance)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DevicesResponse{FetchedAt: fetchedAt}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return DevicesResponse{FetchedAt: fetchedAt}, fmt.Errorf("GOWA API returned status %d", resp.StatusCode)
	}
	var body any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return DevicesResponse{FetchedAt: fetchedAt}, err
	}
	devices, err := normalizeDevices(body)
	if err != nil {
		return DevicesResponse{FetchedAt: fetchedAt}, err
	}
	return DevicesResponse{Count: len(devices), Connected: len(devices) > 0, Devices: devices, Source: "live", FetchedAt: fetchedAt}, nil
}

func normalizeDevices(value any) ([]map[string]any, error) {
	switch typed := value.(type) {
	case []any:
		return recordsFromArray(typed)
	case map[string]any:
		for _, key := range []string{"devices", "data", "results", "sessions", "accounts"} {
			if nested, ok := typed[key]; ok {
				if devices, ok := normalizeNestedDevices(nested); ok {
					return devices, nil
				}
			}
		}
		if devices, ok := recordsFromObjectMap(typed); ok {
			return devices, nil
		}
	}
	return nil, errors.New("Unexpected devices response shape")
}

func normalizeNestedDevices(value any) ([]map[string]any, bool) {
	switch typed := value.(type) {
	case []any:
		devices, err := recordsFromArray(typed)
		return devices, err == nil
	case map[string]any:
		return recordsFromObjectMap(typed)
	default:
		return nil, false
	}
}

func recordsFromArray(items []any) ([]map[string]any, error) {
	devices := make([]map[string]any, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("Unexpected devices response shape")
		}
		devices = append(devices, record)
	}
	return devices, nil
}

func recordsFromObjectMap(object map[string]any) ([]map[string]any, bool) {
	devices := []map[string]any{}
	for key, value := range object {
		if isDeviceMetadataKey(key) {
			continue
		}
		record, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		devices = append(devices, record)
	}
	return devices, len(devices) > 0
}

func isDeviceMetadataKey(key string) bool {
	switch key {
	case "count", "connected", "success", "status", "message", "error":
		return true
	default:
		return false
	}
}

func isRunningWithPort(instance Instance) bool {
	return strings.ToLower(instance.Status) == "running" && instance.Port != nil
}

func devicesURL(instance Instance) string {
	return fmt.Sprintf("http://localhost:%d/app/%s/devices", *instance.Port, instance.Key)
}

func applyCommonHeaders(req *http.Request, instance Instance) {
	req.Header.Set("Accept", "application/json")
	config := ParseConfig(instance.Config)
	if len(config.Flags.BasicAuth) == 0 {
		return
	}
	auth := config.Flags.BasicAuth[0]
	if auth.Username == "" && auth.Password == "" {
		return
	}
	req.SetBasicAuth(auth.Username, auth.Password)
}
