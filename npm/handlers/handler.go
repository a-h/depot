package npm

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/npm/db"
	"github.com/a-h/depot/npm/handlers/metadata"
	"github.com/a-h/depot/npm/handlers/tarball"
	"github.com/a-h/depot/storage"
)

func New(log *slog.Logger, db *db.DB, storage storage.Storage, metrics metrics.Metrics) http.Handler {
	mh := metadata.New(log, db)
	th := tarball.New(log, storage, metrics)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Handle tarball uploads/downloads (paths ending with .tgz).
		if strings.HasSuffix(path, ".tgz") {
			th.ServeHTTP(w, r)
			return
		}

		// Delegate all non-tarball requests to the metadata handler.
		mh.ServeHTTP(w, r)
	})
}
