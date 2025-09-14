package nixcacheinfo

import (
	"fmt"
	"log/slog"
	"net/http"
)

func New(log *slog.Logger, storePath string) Handler {
	return Handler{
		log:       log,
		storePath: storePath,
	}
}

type Handler struct {
	log       *slog.Logger
	storePath string
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "StoreDir: %s\nWantMassQuery: 1\nPriority: 30\n", h.storePath)
	return
}
