package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Dependencies struct {
	Logger            *slog.Logger
	AllowedOrigins    []string
	TestPanicRoute    bool
	StaticFS          fs.FS
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
}

func New(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", healthHandler)
	if deps.Instances != nil {
		registerInstanceRoutes(mux, deps)
	}
	if deps.System != nil && deps.PortAllocator != nil {
		registerSystemRoutes(mux, deps)
	}
	if deps.Versions != nil {
		registerVersionRoutes(mux, deps)
	}
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

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
