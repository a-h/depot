package nar

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"

	"github.com/a-h/depot/db"
)

func New(log *slog.Logger, db *db.DB) Handler {
	return Handler{
		log: log,
		db:  db,
	}
}

type Handler struct {
	log *slog.Logger
	db  *db.DB
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.Get(w, r)
		return
	case http.MethodPut:
		h.Put(w, r)
		return
	}
	http.Error(w, fmt.Sprintf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	// Get the hash part.
	hashPart := r.PathValue("hashpart")

	// Get the expected Nar hash if the filename has one.
	var expectedNarHash string
	if split := strings.SplitN(hashPart, "-", 2); len(split) == 2 {
		expectedNarHash = "sha256:" + split[1]
		hashPart = split[0]
	}

	storePath, ok, err := h.db.QueryPathFromHashPart(r.Context(), hashPart)
	if err != nil {
		h.log.Error("failed to query path from hash part", slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, fmt.Sprintf("failed to query path: %v\n", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, fmt.Sprintf("path not found for %s\n", hashPart), http.StatusNotFound)
		return
	}
	pathInfo, ok, err := h.db.QueryPathInfo(r.Context(), storePath)
	if err != nil {
		h.log.Error("failed to query path info", slog.String("storePath", storePath), slog.Any("error", err))
		http.Error(w, fmt.Sprintf("failed to query path info: %v\n", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, fmt.Sprintf("path info not found for %s\n", storePath), http.StatusNotFound)
		return
	}
	if expectedNarHash != "" && expectedNarHash != pathInfo.Hash {
		h.log.Warn("incorrect NAR hash", slog.String("expected", expectedNarHash), slog.String("actual", pathInfo.Hash))
		http.Error(w, "Incorrect NAR hash. Maybe the path has been recreated.", http.StatusNotFound)
		return
	}

	// The Perl implementation sets the Content-Type to text/plain,
	// but it should be application/octet-stream.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", pathInfo.NarSize))

	stderr := bytes.NewBuffer(nil)
	if err = dumpPath(r.Context(), w, stderr, storePath); err != nil {
		h.log.Error("failed to dump path", slog.String("storePath", storePath), slog.String("stderr", stderr.String()), slog.Any("error", err))
		return
	}
}

func dumpPath(ctx context.Context, stdout, stderr io.Writer, ref string) (err error) {
	nixPath, err := exec.LookPath("nix")
	if err != nil {
		return fmt.Errorf("failed to find nix on path: %w", err)
	}
	cmdArgs := []string{"store", "dump-path", ref}
	cmd := exec.CommandContext(ctx, nixPath, cmdArgs...)
	cmd.Stderr = stderr
	cmd.Stdout = stdout
	return cmd.Run()
}

func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "PUT not implemented", http.StatusNotImplemented)
}
