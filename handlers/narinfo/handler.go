package narinfo

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
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

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	hashPart := r.PathValue("hashpart")
	storePath, ok, err := h.db.QueryPathFromHashPart(r.Context(), hashPart)
	if err != nil {
		h.log.Error("failed to query path from hash part", slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, fmt.Sprintf("failed to query path: %v\n", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, fmt.Sprintf("path not found for: %s\n", hashPart), http.StatusNotFound)
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
	narHashParts := strings.SplitN(pathInfo.Hash, ":", 2)
	if len(narHashParts) != 2 {
		h.log.Error("invalid hash", slog.String("hash", pathInfo.Hash))
		http.Error(w, fmt.Sprintf("invalid hash: %s\n", pathInfo.Hash), http.StatusInternalServerError)
		return
	}
	narHash := narHashParts[1]

	w.Header().Set("Content-Type", "text/x-nix-narinfo")

	// Create the output.
	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, "StorePath: %s\n", storePath)
	fmt.Fprintf(buf, "URL: nar/%s-%s.nar\n", hashPart, narHash)
	fmt.Fprintf(buf, "Compression: none\n")
	fmt.Fprintf(buf, "NarHash: %s\n", pathInfo.Hash)
	fmt.Fprintf(buf, "NarSize: %d\n", pathInfo.NarSize)
	if len(pathInfo.Refs) > 0 {
		fmt.Fprintf(buf, "References: %s\n", strings.Join(pathInfo.Refs, " "))
	}
	if pathInfo.Deriver != "" {
		fmt.Fprintf(buf, "Deriver: %s\n", pathInfo.Deriver)
	}

	// Send the output.
	w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))
	_, err = w.Write(buf.Bytes())
	if err != nil {
		h.log.Error("failed to write response", slog.Any("error", err))
		return
	}
}

func (h Handler) Put(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "PUT not implemented", http.StatusNotImplemented)
}
