package narinfo

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/nix-community/go-nix/pkg/narinfo"
	"github.com/nix-community/go-nix/pkg/nixhash"
	"github.com/nix-community/go-nix/pkg/sqlite/binary_cache_v6"
)

func New(log *slog.Logger, cacheDB *binary_cache_v6.Queries, cache int64) Handler {
	return Handler{
		log:     log,
		cacheDB: cacheDB,
		cache:   cache,
	}
}

type Handler struct {
	log     *slog.Logger
	cacheDB *binary_cache_v6.Queries
	cache   int64
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
	hashPart := r.PathValue("hashpart")

	// First, try to find the narinfo in the binary cache database (for uploaded entries).
	narRows, err := h.cacheDB.QueryNar(r.Context(), binary_cache_v6.QueryNarParams{
		Cache:    h.cache,
		Hashpart: hashPart,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			h.log.Error("failed to query cached narinfo", slog.String("hashPart", hashPart), slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		err = nil
	}
	if len(narRows) == 0 {
		http.Error(w, fmt.Sprintf("path not found for: %s\n", hashPart), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/x-nix-narinfo")

	// Reconstruct store path from hash part and name part.
	nar := narRows[0]
	storePath := fmt.Sprintf("/nix/store/%s", hashPart)
	if nar.Namepart.Valid && nar.Namepart.String != "" {
		storePath = fmt.Sprintf("/nix/store/%s-%s", hashPart, nar.Namepart.String)
	}

	info := &narinfo.NarInfo{
		StorePath:   storePath,
		URL:         nar.Url.String,
		Compression: nar.Compression.String,
		FileSize:    uint64(nar.Filesize.Int64),
		NarSize:     uint64(nar.Narsize.Int64),
	}
	if nar.Filehash.Valid && nar.Filehash.String != "" {
		if fileHash, err := nixhash.ParseAny(nar.Filehash.String, nil); err == nil {
			info.FileHash = fileHash
		} else {
			h.log.Warn("failed to parse file hash", slog.String("fileHash", nar.Filehash.String), slog.Any("error", err))
		}
	}
	if nar.Narhash.Valid && nar.Narhash.String != "" {
		if narHash, err := nixhash.ParseAny(nar.Narhash.String, nil); err == nil {
			info.NarHash = narHash
		} else {
			h.log.Warn("failed to parse nar hash", slog.String("narHash", nar.Narhash.String), slog.Any("error", err))
		}
	}
	if nar.Refs.Valid && nar.Refs.String != "" {
		info.References = strings.Fields(nar.Refs.String)
	}
	if nar.Deriver.Valid && nar.Deriver.String != "" {
		info.Deriver = nar.Deriver.String
	}

	h.log.Debug(r.URL.String(), slog.String("storePath", storePath), slog.String("source", "cache"))

	output := info.String()
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(output)))
	if _, err = w.Write([]byte(output)); err != nil {
		h.log.Error("failed to write response", slog.Any("error", err))
		return
	}
}

func (h Handler) Put(w http.ResponseWriter, r *http.Request) {
	hashPart := r.PathValue("hashpart")

	h.log.Info("uploading narinfo", slog.String("hashPart", hashPart))

	defer r.Body.Close()
	narinfoData, err := narinfo.Parse(r.Body)
	if err != nil {
		h.log.Error("failed to parse narinfo", slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Validate that the hash part matches.
	expectedHashPart := getHashPartFromStorePath(narinfoData.StorePath)
	if expectedHashPart != hashPart {
		h.log.Error("hash part mismatch", slog.String("expected", expectedHashPart), slog.String("actual", hashPart))
		http.Error(w, "hash part does not match store path", http.StatusBadRequest)
		return
	}

	// Extract name part from store path.
	namePart := ""
	if base := filepath.Base(narinfoData.StorePath); base != "" {
		parts := strings.SplitN(base, "-", 2)
		if len(parts) > 1 {
			namePart = parts[1]
		}
	}

	// Prepare references string.
	refsStr := strings.Join(narinfoData.References, " ")

	// Prepare hash strings.
	var fileHashStr, narHashStr string
	if narinfoData.FileHash != nil {
		fileHashStr = narinfoData.FileHash.String()
	}
	if narinfoData.NarHash != nil {
		narHashStr = narinfoData.NarHash.String()
	}

	// Prepare signatures.
	signatures := make([]string, len(narinfoData.Signatures))
	for i, sig := range narinfoData.Signatures {
		signatures[i] = sig.String()
	}

	// Store the NAR info.
	err = h.cacheDB.InsertNar(r.Context(), binary_cache_v6.InsertNarParams{
		Cache:       1,
		Hashpart:    hashPart,
		Namepart:    sql.NullString{String: namePart, Valid: namePart != ""},
		Url:         sql.NullString{String: narinfoData.URL, Valid: narinfoData.URL != ""},
		Compression: sql.NullString{String: narinfoData.Compression, Valid: narinfoData.Compression != ""},
		Filehash:    sql.NullString{String: fileHashStr, Valid: fileHashStr != ""},
		Filesize:    sql.NullInt64{Int64: int64(narinfoData.FileSize), Valid: narinfoData.FileSize > 0},
		Narhash:     sql.NullString{String: narHashStr, Valid: narHashStr != ""},
		Narsize:     sql.NullInt64{Int64: int64(narinfoData.NarSize), Valid: narinfoData.NarSize > 0},
		Refs:        sql.NullString{String: refsStr, Valid: len(narinfoData.References) > 0},
		Deriver:     sql.NullString{String: narinfoData.Deriver, Valid: narinfoData.Deriver != ""},
		Sigs:        sql.NullString{String: strings.Join(signatures, " "), Valid: len(signatures) > 0},
		Ca:          sql.NullString{String: narinfoData.CA, Valid: narinfoData.CA != ""},
		Timestamp:   time.Now().Unix(),
	})
	if err != nil {
		h.log.Error("failed to store narinfo in cache database", slog.String("hashPart", hashPart), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.log.Info("successfully uploaded narinfo", slog.String("hashPart", hashPart), slog.String("storePath", narinfoData.StorePath))

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
