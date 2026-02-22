package tarball

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/depot/downloadcounter"
	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/storage"
)

func New(log *slog.Logger, storage storage.Storage, downloadCounter chan<- downloadcounter.DownloadEvent, metrics metrics.Metrics) Handler {
	return Handler{
		log:             log,
		storage:         storage,
		downloadCounter: downloadCounter,
		metrics:         metrics,
	}
}

// Handler serves NPM package tarballs.
type Handler struct {
	log             *slog.Logger
	storage         storage.Storage
	downloadCounter chan<- downloadcounter.DownloadEvent
	metrics         metrics.Metrics
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead, http.MethodGet:
		h.Get(w, r)
		return
	case http.MethodPut:
		h.Put(w, r)
		return
	}
	http.Error(w, fmt.Sprintf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	requestPath := strings.TrimPrefix(r.URL.Path, "/")

	// Extract package name from path.
	parts := strings.Split(requestPath, "/")
	if len(parts) < 2 {
		http.Error(w, "invalid tarball path", http.StatusBadRequest)
		return
	}

	// Check if file exists and open for reading using Storage interface.
	file, exists, err := h.storage.Get(requestPath)
	if err != nil {
		h.log.Error("failed to read tarball", slog.String("path", requestPath), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "tarball not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	h.log.Debug("serving tarball", slog.String("path", requestPath))
	w.Header().Set("Content-Type", "application/octet-stream")

	// Copy file content to response.
	bytesDownloaded, err := io.Copy(w, file)
	if err != nil {
		h.log.Error("failed to serve tarball", slog.String("path", requestPath), slog.Any("error", err))
		return
	}

	h.downloadCounter <- downloadcounter.DownloadEvent{Group: "npm", Name: requestPath}
	h.metrics.IncrementDownloadMetrics(r.Context(), "npm", bytesDownloaded)
}

func (h Handler) Put(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	path := strings.TrimPrefix(r.URL.Path, "/")

	// Extract package name from path.
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "invalid tarball path", http.StatusBadRequest)
		return
	}

	// Use Storage interface for writing.
	f, err := h.storage.Put(path)
	if err != nil {
		h.log.Error("failed to create tarball", slog.String("path", path), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Copy request body to storage.
	bytesWritten, err := io.Copy(f, r.Body)
	if err != nil {
		h.log.Error("failed to save tarball", slog.String("path", path), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.metrics.IncrementUploadMetrics(r.Context(), "npm", bytesWritten)

	h.log.Debug("tarball uploaded successfully", slog.String("path", path))
	w.WriteHeader(http.StatusOK)
}
