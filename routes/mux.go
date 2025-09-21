package routes

import (
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/a-h/depot/auth"
	authmiddleware "github.com/a-h/depot/middleware/auth"
	"github.com/a-h/depot/middleware/logger"
	nixdb "github.com/a-h/depot/nix/db"
	nixhandler "github.com/a-h/depot/nix/handlers"
	npmdb "github.com/a-h/depot/npm/db"
	npmhandler "github.com/a-h/depot/npm/handlers"
	"github.com/a-h/depot/storage"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

func New(log *slog.Logger, nixdb *nixdb.DB, npmdb *npmdb.DB, storePath string, authConfig *auth.AuthConfig, privateKey *signature.SecretKey) http.Handler {
	mux := http.NewServeMux()

	nixStoragePath := filepath.Join(storePath, "nix")
	nih := nixhandler.New(log, nixdb, nixStoragePath, privateKey)
	mux.Handle("/nix/", http.StripPrefix("/nix", nih))

	npmStorage := storage.NewFileSystem(filepath.Join(storePath, "npm"))
	npmh := npmhandler.New(log, npmdb, npmStorage)
	mux.Handle("/npm/", http.StripPrefix("/npm", npmh))

	authHandler := authmiddleware.New(log, authConfig, mux)
	return logger.New(log, authHandler)
}
