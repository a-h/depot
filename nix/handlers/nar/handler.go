package nar

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nix-community/go-nix/pkg/nixbase32"
)

func New(log *slog.Logger, storePath string) Handler {
	return Handler{
		log:       log,
		storePath: storePath,
	}
}

type Handler struct {
	log       *slog.Logger
	storePath string
}

// getFileExtensionAndContentType extracts the file extension from the URL path
// and returns both the extension and corresponding content type.
func getFileExtensionAndContentType(path string) (string, string) {
	if strings.HasSuffix(path, ".nar.xz") {
		return ".nar.xz", "application/x-xz"
	}
	if strings.HasSuffix(path, ".nar.gz") {
		return ".nar.gz", "application/gzip"
	}
	if strings.HasSuffix(path, ".nar.bz2") {
		return ".nar.bz2", "application/x-bzip2"
	}
	return ".nar", "application/octet-stream"
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.GetHead(w, r)
		return
	case http.MethodHead:
		h.GetHead(w, r)
		return
	case http.MethodPut:
		h.Put(w, r)
		return
	}
	http.Error(w, fmt.Sprintf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
}

func (h *Handler) GetHead(w http.ResponseWriter, r *http.Request) {
	// Get the hash part - this is the file hash, not the store path hash.
	hashPart := r.PathValue("hashpart")

	// Remove any NAR hash suffix if present (e.g., "filehash-narhash" -> "filehash").
	if split := strings.SplitN(hashPart, "-", 2); len(split) == 2 {
		hashPart = split[0]
	}

	// Validate hash part to prevent directory traversal.
	if !isValidHashPart(hashPart) {
		h.log.Debug("invalid hash part", slog.String("hashPart", hashPart))
		http.Error(w, "invalid hash part", http.StatusBadRequest)
		return
	}

	fileExt, contentType := getFileExtensionAndContentType(r.URL.Path)

	narPath := filepath.Join(h.storePath, "nar", hashPart+fileExt)
	file, err := os.Open(narPath)
	if err != nil {
		if !os.IsNotExist(err) {
			h.log.Error("failed to open NAR file", slog.String("narPath", narPath), slog.String("hashPart", hashPart), slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		h.log.Debug("NAR file not found", slog.String("narPath", narPath), slog.String("hashPart", hashPart))
		http.Error(w, "NAR file not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		h.log.Error("failed to stat NAR file", slog.String("narPath", narPath), slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, "failed to get NAR file info", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	if r.Method == http.MethodHead {
		// Skip writing body for HEAD requests.
		w.WriteHeader(http.StatusOK)
		return
	}

	_, err = io.Copy(w, file)
	if err != nil {
		h.log.Error("failed to serve NAR file", slog.String("narPath", narPath), slog.String("hashPart", hashPart), slog.Any("error", err))
		return
	}
}

func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	hashPart := r.PathValue("hashpart")
	if split := strings.SplitN(hashPart, "-", 2); len(split) == 2 {
		hashPart = split[0]
	}

	// Validate hash part to prevent directory traversal.
	if !isValidHashPart(hashPart) {
		h.log.Debug("invalid hash part", slog.String("hashPart", hashPart))
		http.Error(w, "invalid hash part", http.StatusBadRequest)
		return
	}

	fileExt, _ := getFileExtensionAndContentType(r.URL.Path)

	narDir := filepath.Join(h.storePath, "nar")
	if err := os.MkdirAll(narDir, 0755); err != nil {
		h.log.Error("failed to create NAR storage directory", slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	narPath := filepath.Join(narDir, hashPart+fileExt)
	file, err := os.Create(narPath)
	if err != nil {
		h.log.Error("failed to create NAR file", slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	_, err = io.Copy(file, r.Body)
	if err != nil {
		h.log.Error("failed to write NAR file", slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Ensure all data is written to disk before returning.
	if err := file.Sync(); err != nil {
		h.log.Error("failed to sync NAR file", slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// isValidHashPart validates that a hash part is a valid nixbase32 string.
func isValidHashPart(hashPart string) bool {
	if len(hashPart) == 0 {
		return false
	}
	// Use go-nix's nixbase32 validation to ensure proper format.
	return nixbase32.ValidateString(hashPart) == nil
}
