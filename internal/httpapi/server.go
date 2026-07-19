package httpapi

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/proxy"
)

type Dependencies struct {
	Logger            *slog.Logger
	AllowedOrigins    []string
	TestPanicRoute    bool
	StaticFS          fs.FS
	AdminUsername     string
	AdminPassword     string
	Instances         InstanceService
	InstanceLifecycle InstanceLifecycle
	DeviceClient      InstanceDeviceClient
	ConnectionTester  InstanceConnectionTester
	AdminLinkIssuer   AdminLinkIssuer
	System            SystemService
	PortAllocator     PortAllocator
	PortChecker       PortChecker
	AutoUpdate        AutoUpdateService
	Versions          VersionService
	VersionInstaller  VersionInstaller
	Readiness         ReadinessProbe
	// InstanceDirResolver resolves an instance ID to its on-disk directory.
	// Optional: used by the app layer to wire the cleanup scheduler; not
	// consulted by any HTTP route. Defined locally to avoid an import cycle
	// with the scheduler package.
	InstanceDirResolver InstanceDirResolver

	// Proxy-related dependencies. The /app/{key}/* routes are registered
	// outside the manager's Basic Auth middleware; they handle their own
	// authentication via magic admin cookies and instance-level Basic
	// Auth. When any of these are non-nil the proxy routes are exposed.
	HTTPProxy      *proxy.HTTPProxy
	WSBridge       *proxy.WSBridge
	MagicAuth      *auth.MagicAuthService
	InstanceLookup InstanceLookup
}

// InstanceDirResolver resolves an instance ID to its data directory path.
// Implementations include *instances.Filesystem.
type InstanceDirResolver interface {
	InstanceDir(id int64) (string, error)
}

func New(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", healthHandler)
	if deps.Readiness != nil {
		mux.HandleFunc("/api/ready", readyHandler(deps.Readiness))
	}
	registerAuthRoutes(mux, deps)

	// Protected routes (instances, system, versions) are registered on a
	// sub-mux and wrapped with Basic Auth middleware. This mirrors the
	// Bun/Elysia .guard() that protects the instances and system modules
	// while leaving health, auth/logout, and the /api/ 404 catch-all
	// unprotected.
	//
	// Auth is applied only when credentials are configured. In production
	// the config always supplies defaults ("admin"/"password"), so the
	// guard is always active. Tests that omit credentials get the raw
	// handler, preserving existing behaviour.
	protectedMux := http.NewServeMux()
	if deps.Instances != nil {
		registerInstanceRoutes(protectedMux, deps)
	}
	if deps.System != nil && deps.PortAllocator != nil {
		registerSystemRoutes(protectedMux, deps)
	}
	if deps.Versions != nil {
		registerVersionRoutes(protectedMux, deps)
	}
	protectedHandler := http.Handler(protectedMux)
	if deps.AdminUsername != "" && deps.AdminPassword != "" {
		protectedHandler = basicAuthMiddleware(protectedMux, deps.AdminUsername, deps.AdminPassword)
	}
	mux.Handle("/api/instances", protectedHandler)
	mux.Handle("/api/instances/", protectedHandler)
	mux.Handle("/api/system/", protectedHandler)

	// Proxy routes (/app/{key}/*) are outside Basic Auth. They handle
	// their own authentication via magic admin cookies and instance-level
	// Basic Auth injected by the proxy layer.
	registerProxyRoutes(mux, deps)

	if deps.TestPanicRoute {
		mux.HandleFunc("/api/__panic", func(http.ResponseWriter, *http.Request) { panic("test panic") })
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "Not found"})
	})
	if deps.StaticFS != nil {
		mux.Handle("/", staticHandler(deps.StaticFS))
	}
	return recoverMiddleware(requestIDMiddleware(corsMiddleware(mux, deps.AllowedOrigins), deps.Logger), deps.Logger)
}

func requestIDMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if logger != nil {
			logger.Info("http request", "method", r.Method, "route", r.URL.Path, "status", rw.status, "duration", time.Since(start))
		}
	})
}

func recoverMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if logger != nil {
					logger.Error("http panic recovered", "method", r.Method, "route", r.URL.Path)
				}
				writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": "Internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler, allowedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && originAllowed(origin, allowedOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originAllowed(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == "*" || strings.EqualFold(candidate, origin) {
			return true
		}
	}
	return false
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Hijack delegates to the underlying ResponseWriter's Hijack method so
// that WebSocket upgrades and other connection-hijacking handlers work
// through the request-ID middleware. If the underlying writer does not
// implement http.Hijacker, an error is returned.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrHijacked
	}
	return h.Hijack()
}

// Flush delegates to the underlying ResponseWriter's Flush method so
// that streaming responses and WebSocket upgrades work through the
// middleware. If the underlying writer does not implement http.Flusher,
// Flush is a no-op.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
