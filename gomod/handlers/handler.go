package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/depot/gomod/db"
	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/storage"
	"golang.org/x/mod/module"
)

// New creates an HTTP handler implementing the Go module proxy protocol.
func New(log *slog.Logger, db *db.DB, storage storage.Storage, metrics metrics.Metrics) http.Handler {
	return &router{
		metadata: &metadataHandler{log: log, db: db, storage: storage, metrics: metrics},
		archive:  &archiveHandler{log: log, storage: storage, metrics: metrics},
	}
}

// pathInfo holds the parsed components of a request path.
type pathInfo struct {
	modulePath string
	// resource is one of: "list", "v1.0.0.info", "v1.0.0.mod", "v1.0.0.zip", or "@latest".
	resource string
}

// parsePath extracts the module path and resource from a request path.
// The path should already have the /go/ prefix stripped.
//
// Examples:
//
//	github.com/foo/bar/@v/list        -> module="github.com/foo/bar", resource="list"
//	github.com/foo/bar/@v/v1.0.0.info -> module="github.com/foo/bar", resource="v1.0.0.info"
//	github.com/foo/bar/@latest        -> module="github.com/foo/bar", resource="@latest"
func parsePath(requestPath string) (info pathInfo, err error) {
	p := strings.TrimPrefix(requestPath, "/")

	if strings.HasSuffix(p, "/@latest") {
		encoded := strings.TrimSuffix(p, "/@latest")
		decoded, err := module.UnescapePath(encoded)
		if err != nil {
			return pathInfo{}, fmt.Errorf("invalid module path: %w", err)
		}
		return pathInfo{modulePath: decoded, resource: "@latest"}, nil
	}

	encoded, resource, ok := strings.Cut(p, "/@v/")
	if !ok {
		return pathInfo{}, fmt.Errorf("invalid path: missing /@v/ or /@latest")
	}
	if resource == "" {
		return pathInfo{}, fmt.Errorf("invalid path: empty resource")
	}

	decoded, err := module.UnescapePath(encoded)
	if err != nil {
		return pathInfo{}, fmt.Errorf("invalid module path: %w", err)
	}

	return pathInfo{modulePath: decoded, resource: resource}, nil
}

// storageKey builds a storage key from a module path and resource name.
func storageKey(modulePath, resource string) (string, error) {
	encoded, err := module.EscapePath(modulePath)
	if err != nil {
		return "", err
	}
	return encoded + "/@v/" + resource, nil
}

// router dispatches requests to the appropriate resource handler.
type router struct {
	metadata *metadataHandler
	archive  *archiveHandler
}

func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	info, err := parsePath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch {
	case strings.HasSuffix(info.resource, ".zip"):
		rt.archive.serveHTTP(w, r, info)
	default:
		rt.metadata.serveHTTP(w, r, info)
	}
}

// metadataHandler handles .info, .mod, list, and @latest requests.
// These resources are backed by the database and optionally stored to
// the storage backend on PUT.
type metadataHandler struct {
	log     *slog.Logger
	db      *db.DB
	storage storage.Storage
	metrics metrics.Metrics
}

func (h *metadataHandler) serveHTTP(w http.ResponseWriter, r *http.Request, info pathInfo) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.get(w, r, info)
	case http.MethodPut:
		h.put(w, r, info)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *metadataHandler) get(w http.ResponseWriter, r *http.Request, info pathInfo) {
	switch {
	case info.resource == "@latest":
		h.getLatest(w, r, info.modulePath)
	case info.resource == "list":
		h.getList(w, r, info.modulePath)
	case strings.HasSuffix(info.resource, ".info"):
		version := strings.TrimSuffix(info.resource, ".info")
		h.getVersionInfo(w, r, info.modulePath, version)
	case strings.HasSuffix(info.resource, ".mod"):
		version := strings.TrimSuffix(info.resource, ".mod")
		h.getGoMod(w, r, info.modulePath, version)
	default:
		http.Error(w, "unknown resource type", http.StatusBadRequest)
	}
}

func (h *metadataHandler) getLatest(w http.ResponseWriter, r *http.Request, modulePath string) {
	mv, ok, err := h.db.GetLatestVersion(r.Context(), modulePath)
	if err != nil {
		h.log.Error("failed to get latest version", slog.String("module", modulePath), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mv.Info)
}

func (h *metadataHandler) getList(w http.ResponseWriter, r *http.Request, modulePath string) {
	versions, err := h.db.ListVersions(r.Context(), modulePath)
	if err != nil {
		h.log.Error("failed to list versions", slog.String("module", modulePath), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, v := range versions {
		fmt.Fprintln(w, v)
	}
}

func (h *metadataHandler) getVersionInfo(w http.ResponseWriter, r *http.Request, modulePath, version string) {
	unescaped, err := module.UnescapeVersion(version)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid version: %v", err), http.StatusBadRequest)
		return
	}

	mv, ok, err := h.db.GetModuleVersion(r.Context(), modulePath, unescaped)
	if err != nil {
		h.log.Error("failed to get module version", slog.String("module", modulePath), slog.String("version", unescaped), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mv.Info)
}

func (h *metadataHandler) getGoMod(w http.ResponseWriter, r *http.Request, modulePath, version string) {
	unescaped, err := module.UnescapeVersion(version)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid version: %v", err), http.StatusBadRequest)
		return
	}

	mv, ok, err := h.db.GetModuleVersion(r.Context(), modulePath, unescaped)
	if err != nil {
		h.log.Error("failed to get module version", slog.String("module", modulePath), slog.String("version", unescaped), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, mv.GoMod)
}

func (h *metadataHandler) put(w http.ResponseWriter, r *http.Request, info pathInfo) {
	switch {
	case strings.HasSuffix(info.resource, ".info"):
		version := strings.TrimSuffix(info.resource, ".info")
		h.putVersionInfo(w, r, info.modulePath, version)
	case strings.HasSuffix(info.resource, ".mod"):
		version := strings.TrimSuffix(info.resource, ".mod")
		h.putGoMod(w, r, info.modulePath, version)
	default:
		http.Error(w, "unknown resource type for PUT", http.StatusBadRequest)
	}
}

func (h *metadataHandler) putVersionInfo(w http.ResponseWriter, r *http.Request, modulePath, version string) {
	defer r.Body.Close()

	unescaped, err := module.UnescapeVersion(version)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid version: %v", err), http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var vi db.VersionInfo
	if err := json.Unmarshal(body, &vi); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	mv := db.ModuleVersion{Info: vi}
	if err := h.db.PutModuleVersion(r.Context(), modulePath, unescaped, mv); err != nil {
		h.log.Error("failed to put module version", slog.String("module", modulePath), slog.String("version", unescaped), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := writeToStorage(r.Context(), h.storage, modulePath, version+".info", body); err != nil {
		h.log.Error("failed to write .info to storage", slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.metrics.IncrementUploadMetrics(r.Context(), "go", int64(len(body)))
	w.WriteHeader(http.StatusOK)
}

func (h *metadataHandler) putGoMod(w http.ResponseWriter, r *http.Request, modulePath, version string) {
	defer r.Body.Close()

	unescaped, err := module.UnescapeVersion(version)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid version: %v", err), http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Get existing record to preserve Info, or create new.
	mv, _, err := h.db.GetModuleVersion(r.Context(), modulePath, unescaped)
	if err != nil {
		h.log.Error("failed to get existing module version", slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	mv.GoMod = string(body)

	if err := h.db.PutModuleVersion(r.Context(), modulePath, unescaped, mv); err != nil {
		h.log.Error("failed to put module version", slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := writeToStorage(r.Context(), h.storage, modulePath, version+".mod", body); err != nil {
		h.log.Error("failed to write .mod to storage", slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.metrics.IncrementUploadMetrics(r.Context(), "go", int64(len(body)))
	w.WriteHeader(http.StatusOK)
}

// archiveHandler handles .zip requests.
// These are stored directly in the storage backend without database records.
type archiveHandler struct {
	log     *slog.Logger
	storage storage.Storage
	metrics metrics.Metrics
}

func (h *archiveHandler) serveHTTP(w http.ResponseWriter, r *http.Request, info pathInfo) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.get(w, r, info)
	case http.MethodPut:
		h.put(w, r, info)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *archiveHandler) get(w http.ResponseWriter, r *http.Request, info pathInfo) {
	key, err := storageKey(info.modulePath, info.resource)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid module path: %v", err), http.StatusBadRequest)
		return
	}

	file, exists, err := h.storage.Get(r.Context(), key)
	if err != nil {
		h.log.Error("failed to get zip", slog.String("key", key), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/zip")
	bytesDownloaded, err := io.Copy(w, file)
	if err != nil {
		h.log.Error("failed to serve zip", slog.String("key", key), slog.Any("error", err))
		return
	}
	h.metrics.IncrementDownloadMetrics(r.Context(), "go", bytesDownloaded)
}

func (h *archiveHandler) put(w http.ResponseWriter, r *http.Request, info pathInfo) {
	defer r.Body.Close()

	key, err := storageKey(info.modulePath, info.resource)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid module path: %v", err), http.StatusBadRequest)
		return
	}

	f, err := h.storage.Put(r.Context(), key)
	if err != nil {
		h.log.Error("failed to create storage file", slog.String("key", key), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	bytesWritten, err := io.Copy(f, r.Body)
	if err != nil {
		h.log.Error("failed to write zip to storage", slog.String("key", key), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.metrics.IncrementUploadMetrics(r.Context(), "go", bytesWritten)
	w.WriteHeader(http.StatusOK)
}

func writeToStorage(ctx context.Context, s storage.Storage, modulePath, resource string, data []byte) error {
	key, err := storageKey(modulePath, resource)
	if err != nil {
		return err
	}

	f, err := s.Put(ctx, key)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(data)
	return err
}
