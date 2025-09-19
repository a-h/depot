package npm

import (
	"log/slog"
	"net/http"

	"github.com/a-h/depot/npm/db"
)

func New(log *slog.Logger, db *db.DB, storePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
}
