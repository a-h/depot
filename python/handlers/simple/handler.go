package simple

import (
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/python/db"
	"github.com/a-h/depot/python/models"
	"github.com/a-h/depot/storage"
)

func New(log *slog.Logger, db *db.DB, storage storage.Storage, baseURL string, metrics metrics.Metrics) Handler {
	return Handler{
		log:     log,
		db:      db,
		storage: storage,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		metrics: metrics,
	}
}

type Handler struct {
	log     *slog.Logger
	db      *db.DB
	storage storage.Storage
	baseURL string
	metrics metrics.Metrics
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Trim prefix of /simple, if present.
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/simple")

	switch r.Method {
	case http.MethodGet:
		h.Get(w, r)
		return
	case http.MethodPut:
		h.Put(w, r)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	pkg := strings.TrimPrefix(r.URL.Path, "/")
	pkg = strings.TrimSuffix(pkg, "/")

	if pkg == "" {
		h.listPackages(w, r)
		return
	}

	pathParts := strings.Split(pkg, "/")
	if len(pathParts) > 1 {
		h.getPackageFile(w, r, pathParts[0], pathParts[1])
		return
	}

	h.getPackage(w, r, pkg)
}

func (h Handler) listPackages(w http.ResponseWriter, r *http.Request) {
	h.log.Debug("Listing packages")
	packages, err := h.db.ListPackages(r.Context())
	if err != nil {
		h.log.Error("failed to list packages", slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.log.Debug("Found packages", slog.Int("count", len(packages)))

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/vnd.pypi.simple.v1+json") {
		h.listPackagesJSON(w, packages)
		return
	}

	h.listPackagesHTML(w, packages)
}

func (h Handler) listPackagesJSON(w http.ResponseWriter, packages []string) {
	response := struct {
		Meta struct {
			APIVersion string `json:"api-version"`
		} `json:"meta"`
		Projects []struct {
			Name string `json:"name"`
		} `json:"projects"`
	}{
		Meta: struct {
			APIVersion string `json:"api-version"`
		}{
			APIVersion: "1.0",
		},
	}

	for _, pkg := range packages {
		response.Projects = append(response.Projects, struct {
			Name string `json:"name"`
		}{Name: pkg})
	}

	w.Header().Set("Content-Type", "application/vnd.pypi.simple.v1+json")
	json.NewEncoder(w).Encode(response)
}

func (h Handler) listPackagesHTML(w http.ResponseWriter, packages []string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<!DOCTYPE html>\n<html>\n<head><title>Simple index</title></head>\n<body>\n"))

	for _, pkg := range packages {
		w.Write([]byte("<a href=\"" + html.EscapeString(pkg) + "/\">" + html.EscapeString(pkg) + "</a><br/>\n"))
	}

	w.Write([]byte("</body>\n</html>\n"))
}

func (h Handler) getPackage(w http.ResponseWriter, r *http.Request, packageName string) {
	h.log.Debug("Getting package", slog.String("package", packageName))
	index, err := h.db.GetPackage(r.Context(), packageName, h.baseURL)
	if err != nil {
		h.log.Error("failed to get package", slog.String("package", packageName), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if len(index.Files) == 0 {
		http.Error(w, "package not found", http.StatusNotFound)
		return
	}

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/vnd.pypi.simple.v1+json") {
		w.Header().Set("Content-Type", "application/vnd.pypi.simple.v1+json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(index)
		return
	}

	h.getPackageHTML(w, index)
}

func (h Handler) getPackageHTML(w http.ResponseWriter, index models.SimplePackageIndex) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<!DOCTYPE html>\n<html>\n<head><title>Links for " + html.EscapeString(index.Name) + "</title></head>\n<body>\n<h1>Links for " + html.EscapeString(index.Name) + "</h1>\n"))

	for _, file := range index.Files {
		w.Write([]byte("<a href=\"" + html.EscapeString(file.URL) + "\""))

		if sha256, ok := file.Hashes["sha256"]; ok {
			w.Write([]byte(" data-dist-info-metadata=\"sha256=" + html.EscapeString(sha256) + "\""))
		}

		if file.RequiresPython != "" {
			w.Write([]byte(" data-requires-python=\"" + html.EscapeString(file.RequiresPython) + "\""))
		}

		w.Write([]byte(">" + html.EscapeString(file.Filename) + "</a><br/>\n"))
	}

	w.Write([]byte("</body>\n</html>\n"))
}

func (h Handler) getPackageFile(w http.ResponseWriter, r *http.Request, pkg string, fileName string) {
	path := path.Join(pkg, fileName)
	h.log.Debug("Getting package file", slog.String("path", path), slog.String("pkg", pkg), slog.String("filename", fileName))
	reader, exists, err := h.storage.Get(r.Context(), path)
	if err != nil {
		h.log.Error("failed to get file", slog.String("path", path), slog.Any("error", err))
		http.Error(w, "failed to get file", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	bytesDownloaded, err := io.Copy(w, reader)
	if err != nil {
		h.log.Error("failed to write file to response", slog.String("path", path), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.metrics.IncrementDownloadMetrics(r.Context(), "python", bytesDownloaded)
}

func (h Handler) Put(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if strings.HasSuffix(path, ".json") {
		var file models.SimpleFileEntry
		if err := json.NewDecoder(r.Body).Decode(&file); err != nil {
			h.log.Warn("failed to decode metadata", slog.String("path", path), slog.Any("error", err))
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if file.PackageName() == "" || file.Version() == "" {
			h.log.Warn("invalid metadata, missing package name or version", slog.String("path", path))
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := h.db.PutPackageVersion(r.Context(), file); err != nil {
			h.log.Error("failed to store package version", slog.String("path", path), slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		return
	}

	writer, err := h.storage.Put(r.Context(), path)
	if err != nil {
		h.log.Error("failed to create file", slog.String("path", path), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer writer.Close()

	bytesWritten, err := io.Copy(writer, r.Body)
	if err != nil {
		h.log.Error("failed to write file", slog.String("path", path), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.metrics.IncrementUploadMetrics(r.Context(), "python", bytesWritten)

	h.log.Debug("stored file", slog.String("path", path))
	w.WriteHeader(http.StatusCreated)
}
