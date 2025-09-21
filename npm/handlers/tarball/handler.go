package tarball

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/depot/storage"
)

func New(log *slog.Logger, storage storage.Storage) Handler {
	return Handler{
		log:     log,
		storage: storage,
	}
}

// Handler serves NPM package tarballs.
type Handler struct {
	log     *slog.Logger
	storage storage.Storage
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
	file, exists, err := h.storage.Read(requestPath)
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

	h.log.Info("serving tarball", slog.String("path", requestPath))
	w.Header().Set("Content-Type", "application/octet-stream")

	// Copy file content to response.
	if _, err := io.Copy(w, file); err != nil {
		h.log.Error("failed to serve tarball", slog.String("path", requestPath), slog.Any("error", err))
		return
	}
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
	if err := h.storage.Write(path, r.Body); err != nil {
		h.log.Error("failed to save tarball", slog.String("path", path), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.log.Info("tarball uploaded successfully", slog.String("path", path))
	w.WriteHeader(http.StatusOK)
}
