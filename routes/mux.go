package routes

import (
	"log/slog"
	"net/http"

	"github.com/a-h/depot/auth"
	authmiddleware "github.com/a-h/depot/middleware/auth"
	"github.com/a-h/depot/middleware/logger"
	"github.com/a-h/depot/nix/db"
	nixhandler "github.com/a-h/depot/nix/handlers"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

func New(log *slog.Logger, db *db.DB, storePath string, privateKey *signature.SecretKey, authConfig *auth.AuthConfig) http.Handler {
	mux := http.NewServeMux()

	nih := nixhandler.New(log, db, storePath, privateKey)
	mux.Handle("/nix/", http.StripPrefix("/nix", nih))

	authHandler := authmiddleware.New(log, authConfig, mux)
	return logger.New(log, authHandler)
}
