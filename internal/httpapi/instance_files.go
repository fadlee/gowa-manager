package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fadlee/gowa-manager/internal/instances"
)

const instanceFilePreviewLimit int64 = 256 * 1024

type instanceFilesResponse struct {
	Path    string              `json:"path"`
	Entries []instanceFileEntry `json:"entries"`
}

type instanceFileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	ModifiedAt  string `json:"modifiedAt"`
	Previewable bool   `json:"previewable"`
}

type instanceFilePreviewResponse struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	ModifiedAt  string `json:"modifiedAt"`
	ContentType string `json:"contentType"`
	Encoding    string `json:"encoding"`
	Content     string `json:"content"`
}

type safeInstancePath struct {
	rootReal string
	rel      string
	full     string
}

func (h *instanceHandler) files(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	target, ok := h.safeInstancePath(w, r, id)
	if !ok {
		return
	}
	info, err := os.Stat(target.full)
	if err != nil {
		h.writeFileError(w, err, "Path not found")
		return
	}
	if !info.IsDir() {
		writeValidation(w, "Path is not a directory")
		return
	}
	entries, err := os.ReadDir(target.full)
	if err != nil {
		h.writeFileError(w, err, "Failed to list files")
		return
	}
	out := make([]instanceFileEntry, 0, len(entries))
	for _, entry := range entries {
		entryPath := filepath.Join(target.full, entry.Name())
		entryReal, err := filepath.EvalSymlinks(entryPath)
		if err != nil || !pathWithinRoot(target.rootReal, entryReal) {
			continue
		}
		entryInfo, err := os.Stat(entryReal)
		if err != nil || (!entryInfo.Mode().IsRegular() && !entryInfo.IsDir()) {
			continue
		}
		entryType := "file"
		if entryInfo.IsDir() {
			entryType = "directory"
		}
		relPath := path.Join(target.rel, entry.Name())
		out = append(out, instanceFileEntry{
			Name:        entry.Name(),
			Path:        relPath,
			Type:        entryType,
			Size:        entryInfo.Size(),
			ModifiedAt:  entryInfo.ModTime().UTC().Format(time.RFC3339Nano),
			Previewable: entryInfo.Mode().IsRegular() && isPreviewableFile(entryReal, entryInfo.Size()),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == "directory"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, http.StatusOK, instanceFilesResponse{Path: target.rel, Entries: out})
}

func (h *instanceHandler) filePreview(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	target, ok := h.safeInstancePath(w, r, id)
	if !ok {
		return
	}
	info, err := os.Stat(target.full)
	if err != nil {
		h.writeFileError(w, err, "Path not found")
		return
	}
	if !info.Mode().IsRegular() {
		writeValidation(w, "Path is not a file")
		return
	}
	if info.Size() > instanceFilePreviewLimit {
		writeValidation(w, "File is too large to preview")
		return
	}
	contentType, encoding, ok := previewEncoding(target.full)
	if !ok {
		writeValidation(w, "File is not previewable")
		return
	}
	data, err := os.ReadFile(target.full)
	if err != nil {
		h.writeFileError(w, err, "Failed to read file")
		return
	}
	var content string
	if encoding == "utf-8" {
		if !utf8.Valid(data) {
			writeValidation(w, "File is not valid UTF-8")
			return
		}
		content = string(data)
	} else {
		content = base64.StdEncoding.EncodeToString(data)
	}
	writeJSON(w, http.StatusOK, instanceFilePreviewResponse{
		Path:        target.rel,
		Name:        instanceFileName(target),
		Type:        "file",
		Size:        info.Size(),
		ModifiedAt:  info.ModTime().UTC().Format(time.RFC3339Nano),
		ContentType: contentType,
		Encoding:    encoding,
		Content:     content,
	})
}

func (h *instanceHandler) fileDownload(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	target, ok := h.safeInstancePath(w, r, id)
	if !ok {
		return
	}
	info, err := os.Stat(target.full)
	if err != nil {
		h.writeFileError(w, err, "Path not found")
		return
	}
	if !info.Mode().IsRegular() {
		writeValidation(w, "Path is not a file")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": instanceFileName(target)}))
	http.ServeFile(w, r, target.full)
}

func (h *instanceHandler) safeInstancePath(w http.ResponseWriter, r *http.Request, id int64) (safeInstancePath, bool) {
	if _, err := h.service.Get(r.Context(), id); err != nil {
		h.writeError(w, err, "Failed to get instance", false)
		return safeInstancePath{}, false
	}
	if h.dirResolver == nil {
		h.writeError(w, instances.ErrRuntimeNotReady, "Instance filesystem not ready", false)
		return safeInstancePath{}, false
	}
	root, err := h.dirResolver.InstanceDir(id)
	if err != nil {
		h.writeFileError(w, err, "Failed to resolve instance directory")
		return safeInstancePath{}, false
	}
	rel, err := cleanRelativeFilePath(r.URL.Query().Get("path"))
	if err != nil {
		writeValidation(w, err.Error())
		return safeInstancePath{}, false
	}
	target, err := resolveInstanceFilePath(root, rel)
	if err != nil {
		h.writeFileError(w, err, "Invalid path")
		return safeInstancePath{}, false
	}
	return target, true
}

func instanceFileName(target safeInstancePath) string {
	if target.rel != "" {
		return path.Base(target.rel)
	}
	return filepath.Base(target.full)
}

func cleanRelativeFilePath(raw string) (string, error) {
	if strings.ContainsRune(raw, 0) {
		return "", errors.New("Path contains NUL byte")
	}
	if filepath.IsAbs(raw) || path.IsAbs(raw) {
		return "", errors.New("Path must be relative")
	}
	slashPath := strings.ReplaceAll(raw, "\\", "/")
	if path.IsAbs(slashPath) {
		return "", errors.New("Path must be relative")
	}
	for _, part := range strings.Split(slashPath, "/") {
		if part == ".." {
			return "", errors.New("Path traversal is not allowed")
		}
	}
	cleaned := path.Clean(slashPath)
	if cleaned == "." {
		return "", nil
	}
	return cleaned, nil
}

func resolveInstanceFilePath(root, rel string) (safeInstancePath, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return safeInstancePath{}, err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return safeInstancePath{}, err
	}
	full := rootReal
	if rel != "" {
		full = filepath.Join(rootReal, filepath.FromSlash(rel))
	}
	fullReal, err := filepath.EvalSymlinks(full)
	if err != nil {
		return safeInstancePath{}, err
	}
	if !pathWithinRoot(rootReal, fullReal) {
		return safeInstancePath{}, errors.New("path escapes instance directory")
	}
	return safeInstancePath{rootReal: rootReal, rel: rel, full: fullReal}, nil
}

func pathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func isPreviewableFile(filename string, size int64) bool {
	if size > instanceFilePreviewLimit {
		return false
	}
	_, _, ok := previewEncoding(filename)
	return ok
}

func previewEncoding(filename string) (contentType string, encoding string, ok bool) {
	contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	baseType := strings.ToLower(strings.Split(contentType, ";")[0])
	if isCommonImageContentType(baseType) {
		return contentType, "base64", true
	}
	if isTextContentType(baseType) || isTextFileExtension(filename) {
		if contentType == "application/octet-stream" {
			contentType = "text/plain"
		}
		if !strings.Contains(strings.ToLower(contentType), "charset=") {
			contentType += "; charset=utf-8"
		}
		return contentType, "utf-8", true
	}
	return contentType, "", false
}

func isCommonImageContentType(contentType string) bool {
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml", "image/bmp", "image/x-icon":
		return true
	default:
		return false
	}
}

func isTextContentType(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") || contentType == "application/json" || contentType == "application/xml" || contentType == "application/javascript" || contentType == "application/x-javascript"
}

func isTextFileExtension(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".txt", ".md", ".json", ".yaml", ".yml", ".xml", ".csv", ".log", ".env", ".ini", ".conf", ".toml", ".js", ".ts", ".tsx", ".jsx", ".css", ".html", ".htm", ".sh", ".sql":
		return true
	default:
		return false
	}
}

func (h *instanceHandler) writeFileError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Path not found", "success": false})
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error(), "success": false})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": errorMessage(err, fallback), "success": false})
}
