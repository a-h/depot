package routes

import (
	"log/slog"
	"net/http"

	"github.com/a-h/depot/auth"
	authmiddleware "github.com/a-h/depot/middleware/auth"
	"github.com/a-h/depot/middleware/logger"
	nixdb "github.com/a-h/depot/nix/db"
	nixhandler "github.com/a-h/depot/nix/handlers"
	npmdb "github.com/a-h/depot/npm/db"
	npmhandler "github.com/a-h/depot/npm/handlers"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

func New(log *slog.Logger, nixdb *nixdb.DB, npmdb *npmdb.DB, storePath string, authConfig *auth.AuthConfig, privateKey *signature.SecretKey) http.Handler {
	mux := http.NewServeMux()

	nih := nixhandler.New(log, nixdb, storePath, privateKey)
	mux.Handle("/nix/", http.StripPrefix("/nix", nih))

	npmh := npmhandler.New(log, npmdb, storePath)
	mux.Handle("/npm/", http.StripPrefix("/npm", npmh))

	authHandler := authmiddleware.New(log, authConfig, mux)
	return logger.New(log, authHandler)
}
