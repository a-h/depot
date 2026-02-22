package routes

import (
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/a-h/depot/auth"
	"github.com/a-h/depot/downloadcounter"
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

func New(log *slog.Logger, nixdb *nixdb.DB, npmdb *npmdb.DB, pythondb *pythondb.DB, storePath string, authConfig *auth.AuthConfig, privateKey *signature.SecretKey, downloadCounter chan<- downloadcounter.DownloadEvent, metrics metrics.Metrics) http.Handler {
	mux := http.NewServeMux()

	nixStorage := storage.NewFileSystem(filepath.Join(storePath, "nix"))
	nih := nixhandler.New(log, nixdb, nixStorage, privateKey, downloadCounter, metrics)
	mux.Handle("/nix/", http.StripPrefix("/nix", nih))

	npmStorage := storage.NewFileSystem(filepath.Join(storePath, "npm"))
	npmh := npmhandler.New(log, npmdb, npmStorage, downloadCounter, metrics)
	mux.Handle("/npm/", http.StripPrefix("/npm", npmh))

	pythonStorage := storage.NewFileSystem(filepath.Join(storePath, "python"))
	pythonh := pythonhandler.New(log, pythondb, pythonStorage, "http://localhost:8080/python", downloadCounter, metrics)
	mux.Handle("/python/", http.StripPrefix("/python", pythonh))

	authHandler := authmiddleware.New(log, authConfig, mux)
	return logger.New(log, authHandler)
}
