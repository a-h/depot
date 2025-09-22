package metadata

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/depot/npm/db"
	"github.com/a-h/depot/npm/models"
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
	case http.MethodDelete:
		h.Delete(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	scope, pkgName, version, err := parsePath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if pkgName == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fullPkgName := pkgName
	if scope != "" {
		fullPkgName = scope + "/" + pkgName
	}

	if version == "" {
		metadata, ok, err := h.db.GetPackage(r.Context(), fullPkgName)
		if err != nil {
			h.log.Error("failed to get package metadata", slog.String("package", fullPkgName), slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "package not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(metadata); err != nil {
			h.log.Error("failed to encode metadata", slog.String("package", fullPkgName), slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		return
	}

	if version == "latest" {
		metadata, ok, err := h.db.GetPackage(r.Context(), fullPkgName)
		if err != nil {
			h.log.Error("failed to get package metadata", slog.String("package", fullPkgName), slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "package not found", http.StatusNotFound)
			return
		}
		latestVersion, exists := metadata.DistTags["latest"]
		if !exists {
			http.Error(w, "latest version not found", http.StatusNotFound)
			return
		}
		versionMetadata, ok := metadata.Versions[latestVersion]
		if !ok {
			http.Error(w, "latest version not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(versionMetadata); err != nil {
			h.log.Error("failed to encode version metadata", slog.String("package", fullPkgName), slog.String("version", latestVersion), slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		return
	}

	versionMetadata, ok, err := h.db.GetPackageVersion(r.Context(), fullPkgName, version)
	if err != nil {
		h.log.Error("failed to get version metadata", slog.String("package", fullPkgName), slog.String("version", version), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "version not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(versionMetadata); err != nil {
		h.log.Error("failed to encode version metadata", slog.String("package", fullPkgName), slog.String("version", version), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}

func (h Handler) Put(w http.ResponseWriter, r *http.Request) {
	scope, pkgName, version, err := parsePath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if pkgName == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fullPkgName := pkgName
	if scope != "" {
		fullPkgName = scope + "/" + pkgName
	}
	if version == "" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Decode the version metadata from the request body.
	defer r.Body.Close()
	var versionMetadata models.AbbreviatedVersion
	if err := json.NewDecoder(r.Body).Decode(&versionMetadata); err != nil {
		h.log.Error("failed to parse version metadata", slog.Any("error", err))
		http.Error(w, "invalid version metadata", http.StatusBadRequest)
		return
	}

	// Validate package name and version match.
	if versionMetadata.Name != fullPkgName {
		http.Error(w, "package name mismatch", http.StatusBadRequest)
		return
	}

	// Save the version to the database.
	if err := h.db.PutPackageVersion(r.Context(), fullPkgName, version, versionMetadata); err != nil {
		h.log.Error("failed to save package version", slog.String("package", fullPkgName), slog.String("version", version), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.log.Debug("saved package version", slog.String("package", fullPkgName), slog.String("version", version))
	w.WriteHeader(http.StatusCreated)
}

// parsePath extracts scope, package name, and version from the request path.
// Scope and version may be empty if not present in the path.
// Handles scoped packages like @scope/package/version and unscoped like package/version.
func parsePath(requestPath string) (scope, packageName, version string, err error) {
	// Trim leading and trailing slashes, then split by "/"
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" || len(parts) > 3 {
		return "", "", "", errors.New("invalid path")
	}

	// Scoped.
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 2 {
			return "", "", "", errors.New("invalid path")
		}
		scope = parts[0]
		packageName = parts[1]
		if len(parts) == 3 {
			version = parts[2]
		}
		return scope, packageName, version, nil
	}

	// Unscoped.
	packageName = parts[0]
	if len(parts) >= 2 {
		version = parts[1]
	}
	return "", packageName, version, nil
}

func (h Handler) Delete(w http.ResponseWriter, r *http.Request) {
	scope, pkgName, version, err := parsePath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if pkgName == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fullPkgName := pkgName
	if scope != "" {
		fullPkgName = scope + "/" + pkgName
	}
	if version == "" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.db.DeletePackageVersion(r.Context(), fullPkgName, version); err != nil {
		h.log.Error("failed to delete package version", slog.String("package", fullPkgName), slog.String("version", version), slog.Any("error", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
