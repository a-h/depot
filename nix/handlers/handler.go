package handlers

import (
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/a-h/depot/nix/db"
	loghandler "github.com/a-h/depot/nix/handlers/log"
	narhandler "github.com/a-h/depot/nix/handlers/nar"
	narinfohandler "github.com/a-h/depot/nix/handlers/narinfo"
	nixcacheinfo "github.com/a-h/depot/nix/handlers/nixcacheinfo"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

func New(log *slog.Logger, db *db.DB, storePath string, privateKey *signature.SecretKey) http.Handler {
	nci := nixcacheinfo.New(log, privateKey)
	nih := narinfohandler.New(log, db, privateKey)
	nh := narhandler.New(log, storePath)
	lh := loghandler.New(log)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nix-cache-info" {
			nci.ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".narinfo") {
			r.SetPathValue("hashpart", getHashPart(r.URL.Path))
			nih.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/nar/") && (strings.HasSuffix(r.URL.Path, ".nar") || strings.HasSuffix(r.URL.Path, ".nar.xz") || strings.HasSuffix(r.URL.Path, ".nar.bz2") || strings.HasSuffix(r.URL.Path, ".nar.gz")) {
			r.SetPathValue("hashpart", getHashPart(r.URL.Path))
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
}

func getHashPart(urlPath string) string {
	file := path.Base(urlPath)
	if dotIndex := strings.Index(file, "."); dotIndex != -1 {
		return file[:dotIndex]
	}
	return file
}
