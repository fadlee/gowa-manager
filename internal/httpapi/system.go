package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fadlee/gowa-manager/internal/system"
)

type SystemService interface {
	GetSystemStatus(context.Context) (system.SystemStatus, error)
	GetSystemConfig() (system.SystemConfig, error)
}

type PortAllocator interface {
	Next(context.Context) (int, error)
}

type PortChecker interface {
	IsPortAvailable(int) bool
}

type AutoUpdateService interface {
	Status(context.Context) (map[string]any, error)
	Check(context.Context) (map[string]any, error)
	Instances(context.Context) ([]AutoUpdateInstance, error)
}

type SystemStatus struct {
	Status         string                `json:"status"`
	Uptime         int64                 `json:"uptime"`
	ManagerVersion string                `json:"managerVersion"`
	Instances      SystemStatusInstances `json:"instances"`
	Ports          SystemStatusPorts     `json:"ports"`
}

type SystemStatusInstances struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Stopped int `json:"stopped"`
}

type SystemStatusPorts struct {
	Allocated     int `json:"allocated"`
	NextAvailable int `json:"next_available"`
}

type SystemConfig struct {
	PortRange         PortRange `json:"port_range"`
	DataDirectory     string    `json:"data_directory"`
	BinariesDirectory string    `json:"binaries_directory"`
}

type PortRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type AutoUpdateInstance struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	CurrentVersion   string `json:"current_version"`
	AvailableVersion string `json:"available_version"`
	UpdateAvailable  bool   `json:"update_available"`
}

type defaultPortChecker struct{}

func (defaultPortChecker) IsPortAvailable(port int) bool { return system.IsPortAvailable(port) }

type noopAutoUpdateService struct{}

func (noopAutoUpdateService) Status(context.Context) (map[string]any, error) {
	return map[string]any{"enabled": false, "checking": false}, nil
}

func (noopAutoUpdateService) Check(context.Context) (map[string]any, error) {
	return map[string]any{"success": true, "message": "Auto-update scheduler not configured"}, nil
}

func (noopAutoUpdateService) Instances(context.Context) ([]AutoUpdateInstance, error) {
	return []AutoUpdateInstance{}, nil
}

func registerSystemRoutes(mux *http.ServeMux, deps Dependencies) {
	h := &systemHandler{service: deps.System, allocator: deps.PortAllocator, checker: deps.PortChecker, autoUpdate: deps.AutoUpdate}
	if h.checker == nil {
		h.checker = defaultPortChecker{}
	}
	if h.autoUpdate == nil {
		h.autoUpdate = noopAutoUpdateService{}
	}
	mux.HandleFunc("/api/system/status", h.status)
	mux.HandleFunc("/api/system/ports/next", h.nextPort)
	mux.HandleFunc("/api/system/config", h.config)
	mux.HandleFunc("/api/system/ports/", h.portRoutes)
	mux.HandleFunc("/api/system/auto-update/status", h.autoUpdateStatus)
	mux.HandleFunc("/api/system/auto-update/check", h.autoUpdateCheck)
	mux.HandleFunc("/api/system/auto-update/instances", h.autoUpdateInstances)
}

type systemHandler struct {
	service    SystemService
	allocator  PortAllocator
	checker    PortChecker
	autoUpdate AutoUpdateService
}

func (h *systemHandler) status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	status, err := h.service.GetSystemStatus(r.Context())
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemStatusResponse(status))
}

func (h *systemHandler) nextPort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	port, err := h.allocator.Next(r.Context())
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Port int `json:"port"`
	}{Port: port})
}

func (h *systemHandler) config(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	config, err := h.service.GetSystemConfig()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemConfigResponse(config))
}

func toSystemStatusResponse(status system.SystemStatus) SystemStatus {
	return SystemStatus{Status: "running", Uptime: status.Uptime, ManagerVersion: status.ManagerVersion, Instances: SystemStatusInstances{Total: status.TotalInstances, Running: status.RunningInstances, Stopped: status.StoppedInstances}, Ports: SystemStatusPorts{Allocated: status.AllocatedPorts, NextAvailable: status.NextAvailablePort}}
}

func toSystemConfigResponse(config system.SystemConfig) SystemConfig {
	return SystemConfig{PortRange: PortRange{Min: config.PortRange.Min, Max: config.PortRange.Max}, DataDirectory: config.DataDirectory, BinariesDirectory: config.BinariesDirectory}
}

func (h *systemHandler) portRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/system/ports/")
	if !strings.HasSuffix(path, "/available") {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "Not found"})
		return
	}
	value := strings.TrimSuffix(path, "/available")
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		writeValidation(w, "Invalid port")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Port      int  `json:"port"`
		Available bool `json:"available"`
	}{Port: port, Available: h.checker.IsPortAvailable(port)})
}

func (h *systemHandler) autoUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := h.autoUpdate.Status(r.Context())
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *systemHandler) autoUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := h.autoUpdate.Check(r.Context())
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *systemHandler) autoUpdateInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := h.autoUpdate.Instances(r.Context())
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func writeHTTPError(w http.ResponseWriter, status int, err error) {
	if err == nil {
		err = errors.New("request failed")
	}
	writeJSON(w, status, map[string]any{"success": false, "error": err.Error()})
}
