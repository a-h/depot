package handlers

import (
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/a-h/depot/db"
	loghandler "github.com/a-h/depot/handlers/log"
	narhandler "github.com/a-h/depot/handlers/nar"
	narinfohandler "github.com/a-h/depot/handlers/narinfo"
	nixcacheinfo "github.com/a-h/depot/handlers/nixcacheinfo"
)

func New(log *slog.Logger, db *db.DB) http.Handler {
	nci := nixcacheinfo.New(log, db)
	nih := narinfohandler.New(log, db)
	nh := narhandler.New(log, db)
	lh := loghandler.New(log)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nix-cache-info" {
			nci.ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".narinfo") {
			r.SetPathValue("hashpart", getHashPart(r))
			nih.ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".nar") {
			r.SetPathValue("hashpart", getHashPart(r))
			nh.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/log/") {
			storepath := strings.TrimPrefix(r.URL.Path, "/log/")
			storepath = filepath.Clean("/" + storepath)
			r.SetPathValue("storepath", storepath)
			lh.ServeHTTP(w, r)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})

	return NewLogger(log, h)
}

func getHashPart(r *http.Request) string {
	file := path.Base(r.URL.Path)
	ext := strings.ToLower(path.Ext(file))
	return strings.TrimSuffix(file, ext)
}
