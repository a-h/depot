package python

import (
	"log/slog"
	"net/http"

	"github.com/a-h/depot/downloadcounter"
	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/python/db"
	"github.com/a-h/depot/python/handlers/simple"
	"github.com/a-h/depot/storage"
)

func New(log *slog.Logger, db *db.DB, storage storage.Storage, baseURL string, downloadCounter chan<- downloadcounter.DownloadEvent, metrics metrics.Metrics) http.Handler {
	return simple.New(log, db, storage, baseURL, downloadCounter, metrics)
}
