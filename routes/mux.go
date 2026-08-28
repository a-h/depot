package routes

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/a-h/depot/auth"
	gomoddb "github.com/a-h/depot/gomod/db"
	gomodhandler "github.com/a-h/depot/gomod/handlers"
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
	"github.com/a-h/depot/web"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

// HandlerConfig holds the dependencies for each package type handler.
type HandlerConfig struct {
	GoMod  PackageHandlerConfig[*gomoddb.DB]
	Nix    NixHandlerConfig
	NPM    NPMHandlerConfig
	Python PythonHandlerConfig
}

// PackageHandlerConfig holds the DB and storage for a package type.
type PackageHandlerConfig[T any] struct {
	DB      T
	Storage storage.Storage
}

// NixHandlerConfig extends PackageHandlerConfig with Nix-specific options.
type NixHandlerConfig struct {
	PackageHandlerConfig[*nixdb.DB]
	PrivateKey *signature.SecretKey
}

// NPMHandlerConfig extends PackageHandlerConfig with NPM-specific options.
type NPMHandlerConfig struct {
	PackageHandlerConfig[*npmdb.DB]
	BaseURL string
}

// PythonHandlerConfig extends PackageHandlerConfig with Python-specific options.
type PythonHandlerConfig struct {
	PackageHandlerConfig[*pythondb.DB]
	BaseURL string
}

func New(log *slog.Logger, cfg HandlerConfig, authConfig *auth.AuthConfig, metrics metrics.Metrics) (http.Handler, error) {
	mux := http.NewServeMux()

	webh, err := web.New(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create web handler: %w", err)
	}
	mux.Handle("/", webh)

	goh := gomodhandler.New(log, cfg.GoMod.DB, cfg.GoMod.Storage, metrics)
	mux.Handle("/go/", http.StripPrefix("/go", goh))

	nih := nixhandler.New(log, cfg.Nix.DB, cfg.Nix.Storage, cfg.Nix.PrivateKey, metrics)
	mux.Handle("/nix/", http.StripPrefix("/nix", nih))

	npmh := npmhandler.New(log, cfg.NPM.DB, cfg.NPM.Storage, cfg.NPM.BaseURL, metrics)
	mux.Handle("/npm/", http.StripPrefix("/npm", npmh))

	pythonh := pythonhandler.New(log, cfg.Python.DB, cfg.Python.Storage, cfg.Python.BaseURL, metrics)
	mux.Handle("/python/", http.StripPrefix("/python", pythonh))

	authHandler := authmiddleware.New(log, authConfig, mux)
	return logger.New(log, authHandler), nil
}
