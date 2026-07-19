// Package httpapi — proxy route handlers. proxy.go wires the HTTP
// reverse proxy (internal/proxy.HTTPProxy) and WebSocket bridge
// (internal/proxy.WSBridge) into the HTTP API server under the
// /app/{key}/* prefix. It also exposes status and health endpoints for
// proxied instances and handles the magic "autologin" query parameter
// that lets a user open an instance's admin UI without a browser Basic
// Auth prompt.
//
// Route precedence (Go 1.22+ ServeMux resolves more specific patterns
// first):
//
//	GET /app/{key}/status      — proxy status JSON
//	GET /app/{key}/health      — health check that pings the instance
//	GET /app/{key}/ws          — WebSocket upgrade (delegated to WSBridge)
//	/app/{key}/{path...}       — catch-all HTTP proxy (all methods)
//	/app/{key}                 — root fallback (all methods)
//
// All /app/* routes are OUTSIDE the manager's Basic Auth middleware.
// They handle their own authentication via magic admin cookies and
// instance-level Basic Auth injected by the proxy layer.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/fadlee/gowa-manager/internal/proxy"
)

// InstanceLookup looks up an instance by its key. It is satisfied by
// instances.Repository and by any minimal stub used in tests.
type InstanceLookup interface {
	FindByKey(ctx context.Context, key string) (instances.Instance, error)
}

// proxyStatusResponse is the JSON shape returned by GET /app/{key}/status.
// It mirrors the Bun ProxyModel.proxyStatus.
type proxyStatusResponse struct {
	InstanceKey  string `json:"instanceKey"`
	InstanceName string `json:"instanceName"`
	Status       string `json:"status"`
	Port         *int   `json:"port"`
	TargetPort   *int   `json:"targetPort"`
	ProxyPath    string `json:"proxyPath"`
}

// proxyHealthResponse is the JSON shape returned by GET /app/{key}/health.
// It mirrors the Bun ProxyModel.healthResponse.
type proxyHealthResponse struct {
	InstanceKey string `json:"instanceKey"`
	Healthy     bool   `json:"healthy"`
	Status      string `json:"status"`
}

// registerProxyRoutes registers the /app/{key}/* proxy routes on the
// given mux. The routes are intentionally registered on the top-level
// mux (not the Basic-Auth-protected sub-mux) so that proxy traffic
// bypasses manager-level authentication. Instance-level auth is handled
// by the proxy layer via magic cookies and Basic Auth injection.
//
// If none of the proxy dependencies are wired the function is a no-op,
// preserving backward compatibility with configurations that do not
// expose the proxy.
func registerProxyRoutes(mux *http.ServeMux, deps Dependencies) {
	if deps.HTTPProxy == nil && deps.WSBridge == nil && deps.InstanceLookup == nil {
		return
	}
	h := &proxyHandler{
		httpProxy:      deps.HTTPProxy,
		wsBridge:       deps.WSBridge,
		magicAuth:      deps.MagicAuth,
		instanceLookup: deps.InstanceLookup,
		healthClient:   &http.Client{Timeout: 5 * time.Second},
	}
	// Go 1.22+ ServeMux: more specific patterns take precedence
	// regardless of registration order. We register from most to
	// least specific for readability.
	mux.HandleFunc("GET /app/{key}/status", h.status)
	mux.HandleFunc("GET /app/{key}/health", h.health)
	mux.HandleFunc("GET /app/{key}/ws", h.websocket)
	mux.HandleFunc("/app/{key}/{path...}", h.proxy)
	mux.HandleFunc("/app/{key}", h.proxy)
}

// proxyHandler holds the dependencies shared across all /app/{key}/*
// route handlers.
type proxyHandler struct {
	httpProxy      *proxy.HTTPProxy
	wsBridge       *proxy.WSBridge
	magicAuth      *auth.MagicAuthService
	instanceLookup InstanceLookup
	healthClient   *http.Client
}

// status handles GET /app/{key}/status. It returns the proxy status
// (port, path, running state) for the instance or 404 when the instance
// is not found.
func (h *proxyHandler) status(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	instance, err := h.lookupInstance(r, key)
	if err != nil {
		writeProxyRouteError(w, err)
		return
	}
	port := instance.Port
	writeJSON(w, http.StatusOK, proxyStatusResponse{
		InstanceKey:  instance.Key,
		InstanceName: instance.Name,
		Status:       instance.Status,
		Port:         port,
		TargetPort:   port,
		ProxyPath:    proxy.ProxyPrefix + "/" + instance.Key,
	})
}

// health handles GET /app/{key}/health. It returns 404 when the
// instance is not found, otherwise 200 with a healthy boolean determined
// by pinging http://localhost:{port}/ with a 5-second timeout.
func (h *proxyHandler) health(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	instance, err := h.lookupInstance(r, key)
	if err != nil {
		writeProxyRouteError(w, err)
		return
	}
	healthy := h.checkInstanceHealth(instance)
	writeJSON(w, http.StatusOK, proxyHealthResponse{
		InstanceKey: instance.Key,
		Healthy:     healthy,
		Status:      instance.Status,
	})
}

// websocket handles GET /app/{key}/ws. It delegates to the WSBridge
// which upgrades the connection and bridges it to the upstream instance
// WebSocket endpoint.
func (h *proxyHandler) websocket(w http.ResponseWriter, r *http.Request) {
	if h.wsBridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "WebSocket proxy not available", "success": false})
		return
	}
	h.wsBridge.ServeWS(w, r)
}

// proxy is the catch-all handler for /app/{key}/{path...} and
// /app/{key}. It handles the magic "autologin" query parameter before
// forwarding to the HTTP reverse proxy.
//
// When ?autologin=<token> is present:
//   - valid token  → 302 redirect to the same URL without the token,
//     with a Set-Cookie header that persists the magic admin cookie;
//   - invalid/expired → 401 with a Set-Cookie header that clears the
//     cookie and a plain-text "Invalid or expired admin link" body.
//
// When no autologin param is present the request is forwarded to the
// HTTPProxy, which handles 404 (not found), 503 (not running), and 502
// (upstream error) responses.
func (h *proxyHandler) proxy(w http.ResponseWriter, r *http.Request) {
	// Autologin handling occurs before forwarding.
	if query := r.URL.Query(); query.Has("autologin") {
		h.handleAutologin(w, r, r.PathValue("key"), query.Get("autologin"))
		return
	}
	if h.httpProxy == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Proxy not available", "success": false})
		return
	}
	h.httpProxy.ServeHTTP(w, r)
}

// handleAutologin validates the magic admin token and either redirects
// with a set-cookie (valid) or responds 401 with a clear-cookie
// (invalid/expired). The autologin query parameter is stripped from the
// redirect URL so the browser does not re-send it.
func (h *proxyHandler) handleAutologin(w http.ResponseWriter, r *http.Request, instanceKey, token string) {
	// Build the redirect URL without the autologin parameter.
	q := r.URL.Query()
	q.Del("autologin")
	redirectURL := r.URL.Path
	if encoded := q.Encode(); encoded != "" {
		redirectURL += "?" + encoded
	}
	if r.URL.Fragment != "" {
		redirectURL += "#" + r.URL.Fragment
	}
	if redirectURL == "" {
		redirectURL = "/" + proxy.ProxyPrefix + "/" + instanceKey + "/"
	}

	requestURL := requestScheme(r) + "://" + r.Host + r.URL.RequestURI()

	// No magic auth service wired → treat as invalid (no cookie to
	// set/clear).
	if h.magicAuth == nil || !h.magicAuth.ValidateToken(token, instanceKey, time.Now()) {
		if h.magicAuth != nil {
			w.Header().Set("Set-Cookie", h.magicAuth.ClearCookie(instanceKey, requestURL))
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Invalid or expired admin link"))
		return
	}

	w.Header().Set("Location", redirectURL)
	w.Header().Set("Set-Cookie", h.magicAuth.CreateCookie(instanceKey, token, requestURL, 0))
	w.WriteHeader(http.StatusFound)
}

// checkInstanceHealth reports whether the instance responds to a GET
// http://localhost:{port}/ within 5 seconds. A non-running or portless
// instance is considered unhealthy without making a request.
func (h *proxyHandler) checkInstanceHealth(instance instances.Instance) bool {
	if instance.Status != "running" || instance.Port == nil {
		return false
	}
	target := fmt.Sprintf("http://localhost:%d/", *instance.Port)
	resp, err := h.healthClient.Get(target)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// lookupInstance fetches the instance by key from the configured
// InstanceLookup. It returns instances.ErrNotFound (wrapped) when the
// instance does not exist so the caller can map it to a 404.
func (h *proxyHandler) lookupInstance(r *http.Request, key string) (instances.Instance, error) {
	if h.instanceLookup == nil {
		return instances.Instance{}, instances.ErrNotFound
	}
	return h.instanceLookup.FindByKey(r.Context(), key)
}

// writeProxyRouteError maps an instance lookup error to the appropriate
// HTTP JSON error response.
func writeProxyRouteError(w http.ResponseWriter, err error) {
	if errors.Is(err, instances.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Instance not found", "success": false})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to lookup instance", "success": false})
}

// requestScheme returns the scheme of the request, accounting for TLS
// and the X-Forwarded-Proto header (set by upstream reverse proxies).
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}
