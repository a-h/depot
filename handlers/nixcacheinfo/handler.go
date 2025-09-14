package nixcacheinfo

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/a-h/depot/db"
)

func New(log *slog.Logger, db *db.DB) Handler {
	return Handler{
		log: log,
	}
}

type Handler struct {
	log *slog.Logger
	db  *db.DB
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "StoreDir: %s\nWantMassQuery: 1\nPriority: 30\n", h.db.StorePath)
	return
}
