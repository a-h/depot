package routes

import (
	"log/slog"
	"net/http"

	"github.com/a-h/depot/auth"
	"github.com/a-h/depot/metrics"
	authmiddleware "github.com/a-h/depot/middleware/auth"
	"github.com/a-h/depot/middleware/logger"
	nixdb "github.com/a-h/depot/nix/db"
	nixhandler "github.com/a-h/depot/nix/handlers"
	npmdb "github.com/a-h/depot/npm/db"
	npmhandler "github.com/a-h/depot/npm/handlers"
	pythondb "github.com/a-h/depot/python/db"
	pythonhandler "github.com/a-h/depot/python/handlers"
	"github.com/a-h/depot/storage"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

func New(log *slog.Logger, nixdb *nixdb.DB, nixStorage storage.Storage, npmdb *npmdb.DB, npmStorage storage.Storage, pythondb *pythondb.DB, pythonStorage storage.Storage, authConfig *auth.AuthConfig, privateKey *signature.SecretKey, metrics metrics.Metrics) http.Handler {
	mux := http.NewServeMux()

	nih := nixhandler.New(log, nixdb, nixStorage, privateKey, metrics)
	mux.Handle("/nix/", http.StripPrefix("/nix", nih))

	npmh := npmhandler.New(log, npmdb, npmStorage, metrics)
	mux.Handle("/npm/", http.StripPrefix("/npm", npmh))

	pythonh := pythonhandler.New(log, pythondb, pythonStorage, "http://localhost:8080/python", metrics)
	mux.Handle("/python/", http.StripPrefix("/python", pythonh))

	authHandler := authmiddleware.New(log, authConfig, mux)
	return logger.New(log, authHandler)
}
