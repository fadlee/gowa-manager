package httpapi

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func staticHandler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		requestPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if requestPath == "." || requestPath == "" {
			serveStaticFile(w, r, assets, "index.html", "no-cache")
			return
		}
		if strings.HasPrefix(requestPath, "assets/") {
			serveExistingAsset(w, r, assets, requestPath, "public, max-age=31536000, immutable")
			return
		}
		if requestPath == "favicon.ico" {
			serveExistingAsset(w, r, assets, requestPath, "public, max-age=31536000")
			return
		}
		serveStaticFile(w, r, assets, "index.html", "no-cache")
	})
}

func serveExistingAsset(w http.ResponseWriter, r *http.Request, assets fs.FS, name, cacheControl string) {
	if _, err := fs.Stat(assets, name); err != nil {
		http.NotFound(w, r)
		return
	}
	serveStaticFile(w, r, assets, name, cacheControl)
}

func serveStaticFile(w http.ResponseWriter, r *http.Request, assets fs.FS, name, cacheControl string) {
	data, err := fs.ReadFile(assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", cacheControl)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(data)
	}
}
