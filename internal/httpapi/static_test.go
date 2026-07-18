package httpapi

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<div id=\"root\"></div>")},
		"assets/app.js":  &fstest.MapFile{Data: []byte("console.log('app')")},
		"favicon.ico":    &fstest.MapFile{Data: []byte("ico")},
		"assets/app.css": &fstest.MapFile{Data: []byte("body{}")},
	}
}

func TestStaticServesSPAFallback(t *testing.T) {
	for _, path := range []string{"/", "/instances/1", "/instances/anything"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(Dependencies{StaticFS: testAssets()}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if rec.Body.String() != "<div id=\"root\"></div>" {
				t.Fatalf("body = %q", rec.Body.String())
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
				t.Fatalf("Cache-Control = %q", got)
			}
		})
	}
}

func TestStaticServesAssetsWithImmutableCache(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Dependencies{StaticFS: testAssets()}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "console.log('app')" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestStaticServesFaviconWithOneYearCache(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Dependencies{StaticFS: testAssets()}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestStaticMissingAssetReturns404(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Dependencies{StaticFS: testAssets()}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() == "<div id=\"root\"></div>" {
		t.Fatal("missing asset returned SPA HTML")
	}
}
