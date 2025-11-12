package python

import (
	"log/slog"
	"net/http"

	"github.com/a-h/depot/python/db"
	"github.com/a-h/depot/python/handlers/simple"
	"github.com/a-h/depot/storage"
)

func New(log *slog.Logger, db *db.DB, storage storage.Storage, baseURL string) http.Handler {
	return simple.New(log, db, storage, baseURL)
}
