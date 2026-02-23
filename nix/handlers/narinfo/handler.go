package narinfo

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/nix/db"
	"github.com/nix-community/go-nix/pkg/narinfo"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

func New(log *slog.Logger, db *db.DB, privateKey *signature.SecretKey, metrics metrics.Metrics) Handler {
	return Handler{
		log:        log,
		db:         db,
		privateKey: privateKey,
		metrics:    metrics,
	}
}

type Handler struct {
	log        *slog.Logger
	db         *db.DB
	privateKey *signature.SecretKey
	metrics    metrics.Metrics
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
	// First, try to find the narinfo in the binary cache database (for uploaded entries).
	ni, ok, err := h.db.GetNarInfo(r.Context(), r.URL.Path)
	if err != nil {
		h.log.Error("failed to query cached narinfo", slog.String("path", r.URL.Path), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, fmt.Sprintf("%s not found", r.URL.Path), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", ni.ContentType())

	h.log.Debug(r.URL.String(), slog.String("storePath", ni.StorePath), slog.String("source", "cache"))

	output := ni.String()
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(output)))
	if _, err = w.Write([]byte(output)); err != nil {
		h.log.Error("failed to write response", slog.Any("error", err))
		return
	}

	h.metrics.IncrementDownloadMetrics(r.Context(), "nix", int64(len(output)))
}

func (h Handler) Put(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	ni, err := narinfo.Parse(r.Body)
	if err != nil {
		h.log.Error("failed to parse narinfo", slog.String("path", r.URL.Path), slog.Any("error", err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// We should have received a request like /optional-cache-name/16hvpw4b3r05girazh4rnwbw0jgjkb4l.narinfo
	// We need to extract the hash part (16hvpw4b3r05girazh4rnwbw0jgjkb4l) and compare it to the hash in the StorePath to check that the uploaded narinfo matches the URL.
	expectedHashPart := strings.TrimSuffix(filepath.Base(r.URL.Path), ".narinfo")

	// Validate that the hash of the URL matches the value in the narinfo.
	actualHashPart := getHashPartFromStorePath(ni.StorePath)
	if actualHashPart != expectedHashPart {
		h.log.Error("hash part mismatch", slog.String("expected", expectedHashPart), slog.String("actual", actualHashPart))
		http.Error(w, fmt.Sprintf("URL hash part %q does not match store path %q", expectedHashPart, ni.StorePath), http.StatusBadRequest)
		return
	}

	// If we have a private key, sign this narinfo.
	if h.privateKey != nil {
		sig, err := h.privateKey.Sign(nil, ni.Fingerprint())
		if err != nil {
			h.log.Error("failed to sign narinfo during upload", slog.String("path", r.URL.Path), slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		ni.Signatures = append(ni.Signatures, sig)
	}

	// Store the NAR info.
	err = h.db.PutNarInfo(r.Context(), r.URL.Path, ni)
	if err != nil {
		h.log.Error("failed to store narinfo in cache database", slog.String("path", r.URL.Path), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func getHashPartFromStorePath(storePath string) string {
	// Store paths are like /nix/store/abc123...-name
	// We need to extract the hash part (abc123...)
	base := filepath.Base(storePath)
	parts := strings.SplitN(base, "-", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
