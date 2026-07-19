package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/fadlee/gowa-manager/internal/versions"
)

var (
	ErrVersionConflict = errors.New("version conflict")
	ErrVersionInvalid  = errors.New("invalid version")
)

type VersionService interface {
	GetInstalledVersions() ([]versions.VersionInfo, error)
	GetAvailableVersions(context.Context, int) ([]versions.VersionInfo, error)
	IsVersionAvailable(context.Context, string) (bool, error)
	GetVersionBinaryPath(string) string
	GetVersionsSize() (map[string]int64, error)
	RemoveVersion(context.Context, string) error
	Cleanup(context.Context, int) ([]string, error)
}

type VersionInstaller interface {
	Install(context.Context, string) (versions.InstallResult, error)
}

type VersionInfo struct {
	Version     string    `json:"version"`
	Path        string    `json:"path"`
	Installed   bool      `json:"installed"`
	IsLatest    bool      `json:"isLatest"`
	Size        int64     `json:"size"`
	InstalledAt time.Time `json:"installedAt"`
}

func registerVersionRoutes(mux *http.ServeMux, deps Dependencies) {
	h := &versionHandler{service: deps.Versions, installer: deps.VersionInstaller}
	mux.HandleFunc("/api/system/versions/installed", h.installed)
	mux.HandleFunc("/api/system/versions/available", h.available)
	mux.HandleFunc("/api/system/versions/install", h.install)
	mux.HandleFunc("/api/system/versions/usage", h.usage)
	mux.HandleFunc("/api/system/versions/cleanup", h.cleanup)
	mux.HandleFunc("/api/system/versions/", h.routes)
}

type versionHandler struct {
	service   VersionService
	installer VersionInstaller
}

func (h *versionHandler) installed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := h.service.GetInstalledVersions()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toVersionInfoResponses(items))
}

func (h *versionHandler) available(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeValidation(w, "Invalid limit")
			return
		}
		limit = parsed
	}
	items, err := h.service.GetAvailableVersions(r.Context(), limit)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toVersionInfoResponses(items))
}

func toVersionInfoResponses(items []versions.VersionInfo) []VersionInfo {
	out := make([]VersionInfo, 0, len(items))
	for _, item := range items {
		out = append(out, VersionInfo{Version: item.Version, Path: item.Path, Installed: item.Installed, IsLatest: item.IsLatest, Size: item.Size, InstalledAt: item.InstalledAt})
	}
	return out
}

func (h *versionHandler) install(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeValidation(w, "Invalid JSON")
		return
	}
	if strings.TrimSpace(body.Version) == "" {
		writeValidation(w, "version is required")
		return
	}
	if isNilVersionInstaller(h.installer) {
		writeHTTPError(w, http.StatusServiceUnavailable, errors.New("version installer not ready"))
		return
	}
	_, err := h.installer.Install(r.Context(), body.Version)
	if err != nil {
		h.writeVersionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{Success: true, Message: "Successfully installed GOWA version " + body.Version})
}

func isNilVersionInstaller(installer VersionInstaller) bool {
	if installer == nil {
		return true
	}
	value := reflect.ValueOf(installer)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func (h *versionHandler) usage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sizes, err := h.service.GetVersionsSize()
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sizes)
}

func (h *versionHandler) cleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	keepCount := 3
	if r.Body != nil {
		var body struct {
			KeepCount *int `json:"keepCount"`
		}
		if err := decodeJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
			writeValidation(w, "Invalid JSON")
			return
		}
		if body.KeepCount != nil {
			keepCount = *body.KeepCount
		}
	}
	if keepCount < 1 {
		writeValidation(w, "keepCount must be at least 1")
		return
	}
	removed, err := h.service.Cleanup(r.Context(), keepCount)
	if err != nil {
		h.writeVersionError(w, err)
		return
	}
	if removed == nil {
		removed = []string{}
	}
	writeJSON(w, http.StatusOK, struct {
		Success bool     `json:"success"`
		Message string   `json:"message"`
		Removed []string `json:"removed"`
	}{Success: true, Message: "Cleaned up " + strconv.Itoa(len(removed)) + " old versions: " + strings.Join(removed, ", "), Removed: removed})
}

func (h *versionHandler) routes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/system/versions/")
	if strings.HasSuffix(path, "/available") {
		version := strings.TrimSuffix(path, "/available")
		h.versionAvailable(w, r, version)
		return
	}
	if strings.Contains(path, "/") || path == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "Not found"})
		return
	}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if path == "latest" {
		writeValidation(w, "Cannot remove the latest version alias")
		return
	}
	if err := h.service.RemoveVersion(r.Context(), path); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Successfully removed GOWA version " + path})
}

func (h *versionHandler) versionAvailable(w http.ResponseWriter, r *http.Request, version string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if version == "" || strings.Contains(version, "/") {
		writeValidation(w, "Invalid version")
		return
	}
	available, err := h.service.IsVersionAvailable(r.Context(), version)
	if err != nil {
		h.writeVersionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Version   string `json:"version"`
		Available bool   `json:"available"`
		Path      string `json:"path"`
	}{Version: version, Available: available, Path: h.service.GetVersionBinaryPath(version)})
}

func (h *versionHandler) writeVersionError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrVersionConflict) || errors.Is(err, ErrVersionInvalid) || strings.Contains(err.Error(), "cannot remove active version") || strings.Contains(err.Error(), "invalid version") {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	writeHTTPError(w, http.StatusInternalServerError, err)
}
