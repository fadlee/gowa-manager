package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/fadlee/gowa-manager/internal/monitoring"
)

type InstanceService interface {
	List(context.Context) ([]instances.Instance, error)
	Get(context.Context, int64) (instances.Instance, error)
	Create(context.Context, instances.CreateRequest) (instances.Instance, error)
	Update(context.Context, int64, instances.UpdateRequest) (instances.Instance, error)
	Delete(context.Context, int64) error
	ResetData(context.Context, int64) error
}

type InstanceStatus struct {
	ID        int64                 `json:"id"`
	Name      string                `json:"name"`
	Status    string                `json:"status"`
	Port      *int                  `json:"port"`
	PID       *int                  `json:"pid"`
	Uptime    int64                 `json:"uptime,omitempty"`
	Resources *monitoring.Resources `json:"resources,omitempty"`
}

type InstanceLifecycle interface {
	Start(context.Context, int64) (InstanceStatus, error)
	Stop(context.Context, int64) (InstanceStatus, error)
	Kill(context.Context, int64) (InstanceStatus, error)
	Restart(context.Context, int64) (InstanceStatus, error)
	Status(context.Context, int64) (InstanceStatus, error)
}

type InstanceDeviceClient interface {
	Fetch(context.Context, instances.Instance) (instances.DevicesResponse, error)
}

type InstanceConnectionTester interface {
	Test(context.Context, instances.Instance) instances.ConnectionTestResult
}

type AdminLink struct {
	URL       string
	ExpiresAt *time.Time
}

type AdminLinkIssuer interface {
	CreateAdminLink(context.Context, instances.Instance) (AdminLink, error)
}

// magicAdminLinkIssuer adapts *auth.MagicAuthService to the
// AdminLinkIssuer interface. It only uses the instance key to mint a
// short-lived token; credentials are never parsed or exposed in the
// response. Token lifetime is centralized in auth.MagicAuthService.
type magicAdminLinkIssuer struct {
	service *auth.MagicAuthService
}

// NewMagicAdminLinkIssuer returns an AdminLinkIssuer backed by the given
// MagicAuthService. The service must be non-nil.
func NewMagicAdminLinkIssuer(service *auth.MagicAuthService) AdminLinkIssuer {
	return magicAdminLinkIssuer{service: service}
}

func (m magicAdminLinkIssuer) CreateAdminLink(_ context.Context, instance instances.Instance) (AdminLink, error) {
	token, expiresAt := m.service.CreateToken(instance.Key, time.Now())
	link := AdminLink{
		URL:       "/app/" + instance.Key + "/?autologin=" + url.QueryEscape(token),
		ExpiresAt: &expiresAt,
	}
	return link, nil
}

type instanceResponse struct {
	ID           int64   `json:"id"`
	Key          string  `json:"key"`
	Name         string  `json:"name"`
	Port         *int    `json:"port"`
	Status       string  `json:"status"`
	Config       string  `json:"config"`
	GOWAVersion  string  `json:"gowa_version"`
	ErrorMessage *string `json:"error_message"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type instanceBody struct {
	Name        *string `json:"name"`
	Config      *string `json:"config"`
	GOWAVersion *string `json:"gowa_version"`
}

func registerInstanceRoutes(mux *http.ServeMux, deps Dependencies) {
	connection := deps.ConnectionTester
	if connection == nil {
		connection = instances.NewConnectionTester(instances.ConnectionTesterOptions{})
	}
	h := &instanceHandler{service: deps.Instances, lifecycle: deps.InstanceLifecycle, devices: deps.DeviceClient, connection: connection, adminLinks: deps.AdminLinkIssuer}
	mux.HandleFunc("/api/instances", h.collection)
	mux.HandleFunc("/api/instances/", h.routes)
}

type instanceHandler struct {
	service    InstanceService
	lifecycle  InstanceLifecycle
	devices    InstanceDeviceClient
	connection InstanceConnectionTester
	adminLinks AdminLinkIssuer
}

func (h *instanceHandler) collection(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/instances" {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "Not found"})
		return
	}
	h.collectionWithSlash(w, r)
}

func (h *instanceHandler) collectionWithSlash(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.List(r.Context())
		if err != nil {
			h.writeError(w, err, "Failed to list instances", false)
			return
		}
		out := make([]instanceResponse, 0, len(items))
		for _, item := range items {
			out = append(out, toInstanceResponse(item))
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var body instanceBody
		if err := decodeJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
			writeValidation(w, "Invalid JSON")
			return
		}
		if err := validateName(body.Name); err != nil {
			writeValidation(w, err.Error())
			return
		}
		request := instances.CreateRequest{Config: body.Config}
		if body.Name != nil {
			request.Name = *body.Name
		}
		if body.GOWAVersion != nil {
			request.GOWAVersion = *body.GOWAVersion
		}
		created, err := h.service.Create(r.Context(), request)
		if err != nil {
			h.writeError(w, err, "Failed to create instance", true)
			return
		}
		writeJSON(w, http.StatusCreated, toInstanceResponse(created))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *instanceHandler) routes(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/instances/" {
		h.collectionWithSlash(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/instances/"), "/")
	id, err := parseID(parts[0])
	if err != nil {
		writeValidation(w, "Invalid instance ID")
		return
	}
	if len(parts) == 1 {
		h.detail(w, r, id)
		return
	}
	if len(parts) != 2 {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "Not found"})
		return
	}
	switch parts[1] {
	case "devices":
		h.devicesRoute(w, r, id)
	case "reset-data":
		h.resetData(w, r, id)
	case "start":
		h.lifecycleRoute(w, r, id, "start")
	case "stop":
		h.lifecycleRoute(w, r, id, "stop")
	case "kill":
		h.lifecycleRoute(w, r, id, "kill")
	case "restart":
		h.lifecycleRoute(w, r, id, "restart")
	case "status":
		h.lifecycleRoute(w, r, id, "status")
	case "admin-link":
		h.adminLink(w, r, id)
	case "test-connection":
		h.testConnection(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "Not found"})
	}
}

func (h *instanceHandler) detail(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.Get(r.Context(), id)
		if err != nil {
			h.writeError(w, err, "Failed to get instance", false)
			return
		}
		writeJSON(w, http.StatusOK, toInstanceResponse(item))
	case http.MethodPut:
		var body instanceBody
		if err := decodeJSON(r, &body); err != nil {
			writeValidation(w, "Invalid JSON")
			return
		}
		if err := validateName(body.Name); err != nil {
			writeValidation(w, err.Error())
			return
		}
		request := instances.UpdateRequest{Name: body.Name, Config: body.Config, GOWAVersion: body.GOWAVersion}
		updated, err := h.service.Update(r.Context(), id, request)
		if err != nil {
			h.writeError(w, err, "Failed to update instance", false)
			return
		}
		writeJSON(w, http.StatusOK, toInstanceResponse(updated))
	case http.MethodDelete:
		if err := h.service.Delete(r.Context(), id); err != nil {
			h.writeError(w, err, "Failed to delete instance", false)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Instance deleted successfully"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *instanceHandler) devicesRoute(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	item, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, err, "Failed to get instance", false)
		return
	}
	if h.devices == nil {
		h.writeError(w, instances.ErrRuntimeNotReady, "Device client not ready", false)
		return
	}
	resp, err := h.devices.Fetch(r.Context(), item)
	if err != nil && resp.Source == "" {
		h.writeError(w, err, "Failed to fetch devices", false)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *instanceHandler) resetData(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := h.service.ResetData(r.Context(), id); err != nil {
		h.writeError(w, err, "Failed to reset instance data", false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Instance data reset successfully"})
}

func (h *instanceHandler) lifecycleRoute(w http.ResponseWriter, r *http.Request, id int64, action string) {
	if (action == "status" && r.Method != http.MethodGet) || (action != "status" && r.Method != http.MethodPost) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.lifecycle == nil {
		h.writeError(w, instances.ErrRuntimeNotReady, "Instance runtime not ready", false)
		return
	}
	var status InstanceStatus
	var err error
	switch action {
	case "start":
		status, err = h.lifecycle.Start(r.Context(), id)
	case "stop":
		status, err = h.lifecycle.Stop(r.Context(), id)
	case "kill":
		status, err = h.lifecycle.Kill(r.Context(), id)
	case "restart":
		status, err = h.lifecycle.Restart(r.Context(), id)
	case "status":
		status, err = h.lifecycle.Status(r.Context(), id)
	}
	if err != nil {
		h.writeError(w, err, "Failed to "+action+" instance", false)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *instanceHandler) adminLink(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	item, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, err, "Failed to get instance", false)
		return
	}
	if !hasBasicAuth(item.Config) {
		writeJSON(w, http.StatusOK, map[string]any{"url": "/app/" + item.Key + "/"})
		return
	}
	if h.adminLinks == nil {
		h.writeError(w, instances.ErrRuntimeNotReady, "Admin link issuer not ready", false)
		return
	}
	link, err := h.adminLinks.CreateAdminLink(r.Context(), item)
	if err != nil {
		h.writeError(w, err, "Failed to create admin link", false)
		return
	}
	body := map[string]any{"url": link.URL}
	if link.ExpiresAt != nil {
		body["expiresAt"] = link.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *instanceHandler) testConnection(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	item, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, err, "Failed to get instance", false)
		return
	}
	if h.connection == nil {
		h.writeError(w, instances.ErrRuntimeNotReady, "Connection tester not ready", false)
		return
	}
	writeJSON(w, http.StatusOK, h.connection.Test(r.Context(), item))
}

func (h *instanceHandler) writeError(w http.ResponseWriter, err error, fallback string, createOrLifecycle bool) {
	switch {
	case errors.Is(err, instances.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Instance not found", "success": false})
	case errors.Is(err, instances.ErrConflict):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "success": false})
	case errors.Is(err, instances.ErrRuntimeNotReady):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error(), "success": false})
	case createOrLifecycle:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": errorMessage(err, fallback), "success": false})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": errorMessage(err, fallback), "success": false})
	}
}

func toInstanceResponse(item instances.Instance) instanceResponse {
	return instanceResponse{ID: item.ID, Key: item.Key, Name: item.Name, Port: item.Port, Status: item.Status, Config: item.Config, GOWAVersion: item.GOWAVersion, ErrorMessage: item.ErrorMessage, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func decodeJSON(r *http.Request, dest any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid ID")
	}
	return id, nil
}

func validateName(name *string) error {
	if name == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*name)
	if trimmed == "" {
		return errors.New("name must be at least 1 character")
	}
	if len(trimmed) > 100 {
		return errors.New("name must be at most 100 characters")
	}
	return nil
}

func writeValidation(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": message, "success": false})
}

func errorMessage(err error, fallback string) string {
	if err != nil && err.Error() != "" {
		return err.Error()
	}
	return fallback
}

func hasBasicAuth(raw string) bool {
	var config struct {
		Flags struct {
			BasicAuth []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"basicAuth"`
		} `json:"flags"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return false
	}
	if len(config.Flags.BasicAuth) == 0 {
		return false
	}
	auth := config.Flags.BasicAuth[0]
	return auth.Username != "" && auth.Password != ""
}
