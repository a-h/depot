package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/a-h/depot/accesslog"
	"github.com/a-h/depot/auth"
	"github.com/a-h/depot/cmd/globals"
	"github.com/a-h/depot/loggedstorage"
	"github.com/a-h/depot/metrics"
	depotmetrics "github.com/a-h/depot/metrics"
	nixcmd "github.com/a-h/depot/nix/cmd"
	nixdb "github.com/a-h/depot/nix/db"
	"github.com/a-h/depot/nix/push"
	npmcmd "github.com/a-h/depot/npm/cmd"
	npmdb "github.com/a-h/depot/npm/db"
	pythoncmd "github.com/a-h/depot/python/cmd"
	pythondb "github.com/a-h/depot/python/db"
	"github.com/a-h/depot/storage"

	"github.com/a-h/depot/routes"
	"github.com/a-h/depot/store"
	"github.com/alecthomas/kong"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

type CLI struct {
	globals.Globals
	Version VersionCmd          `cmd:"" help:"Show version information"`
	Serve   ServeCmd            `cmd:"" help:"Start the depot server"`
	Proxy   ProxyCmd            `cmd:"" help:"Proxy requests to a remote depot with authentication"`
	Nix     nixcmd.NixCmd       `cmd:"" help:"Nix package management commands"`
	NPM     npmcmd.NPMCmd       `cmd:"" help:"NPM package management commands"`
	Python  pythoncmd.PythonCmd `cmd:"" help:"Python package management commands"`
}

var Version = "dev"

type VersionCmd struct{}

func (cmd *VersionCmd) Run(globals *globals.Globals) error {
	fmt.Printf("%s", Version)
	return nil
}

type S3Flags struct {
	Bucket          string `help:"S3 bucket name (required when storage-type=s3)" env:"DEPOT_S3_BUCKET"`
	Region          string `help:"S3 region" default:"us-east-1" env:"DEPOT_S3_REGION"`
	Endpoint        string `help:"S3 endpoint URL (for MinIO/custom endpoints)" env:"DEPOT_S3_ENDPOINT"`
	AccessKeyID     string `help:"S3 access key ID (uses IAM role if not set)" env:"DEPOT_S3_ACCESS_KEY_ID"`
	SecretAccessKey string `help:"S3 secret access key (uses IAM role if not set)" env:"DEPOT_S3_SECRET_ACCESS_KEY"`
	ForcePathStyle  bool   `help:"Use path-style S3 URLs (required for MinIO)" env:"DEPOT_S3_FORCE_PATH_STYLE"`
}

type ServeCmd struct {
	DatabaseType      string  `help:"Choice of database (sqlite, rqlite or postgres)" default:"sqlite" enum:"sqlite,rqlite,postgres" env:"DEPOT_DATABASE_TYPE"`
	DatabaseURL       string  `help:"Database connection URL" default:"" env:"DEPOT_DATABASE_URL"`
	ListenAddr        string  `help:"Address to listen on" default:":8080" env:"DEPOT_LISTEN_ADDR"`
	MetricsListenAddr string  `help:"Address for metrics endpoint" default:":9090" env:"DEPOT_METRICS_LISTEN_ADDR"`
	StorePath         string  `help:"Path to file store" default:"" env:"DEPOT_STORE_PATH"`
	AuthFile          string  `help:"Path to SSH public keys auth file (format: r/w ssh-key comment)" env:"DEPOT_AUTH_FILE"`
	PrivateKey        string  `help:"Path to private key file for signing narinfo files" env:"DEPOT_PRIVATE_KEY"`
	StorageType       string  `help:"Storage backend type (fs or s3)" default:"fs" enum:"fs,s3" env:"DEPOT_STORAGE_TYPE"`
	S3                S3Flags `embed:"" prefix:"s3-"`
}

func (cmd *ServeCmd) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	switch cmd.StorageType {
	case "s3":
		if cmd.S3.Bucket == "" {
			return fmt.Errorf("--s3-bucket must also be set when --storage-type=s3")
		}
	case "fs":
		if cmd.StorePath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get user home directory: %w", err)
			}
			cmd.StorePath = fmt.Sprintf("%s/depot-store", home)
		}
		if err := os.MkdirAll(cmd.StorePath, 0755); err != nil {
			return fmt.Errorf("failed to create store directory: %w", err)
		}
	default:
		return fmt.Errorf("unknown storage type: %q - expected 'fs' or 's3'", cmd.StorageType)
	}

	if cmd.DatabaseURL == "" {
		cmd.DatabaseURL = fmt.Sprintf("file:%s?cache=shared&mode=rwc&_busy_timeout=5000&_txlock=immediate&_journal_mode=DELETE", filepath.Join(cmd.StorePath, "depot.db"))
	}

	// Create a new store.
	store, closer, err := store.New(context.Background(), cmd.DatabaseType, cmd.DatabaseURL)
	if err != nil {
		log.Error("failed to connect to database", slog.String("error", err.Error()))
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer closer()

	// Load authentication configuration if provided.
	var authConfig *auth.AuthConfig
	if cmd.AuthFile != "" {
		authConfig, err = auth.LoadAuthConfig(cmd.AuthFile)
		if err != nil {
			return fmt.Errorf("failed to load auth config: %w", err)
		}
		log.Info("loaded authentication configuration", slog.String("authFile", cmd.AuthFile), slog.Int("keys", len(authConfig.Keys)), slog.Bool("requireAuthForRead", authConfig.RequireAuthForRead))
	}

	// Load private key for signing if provided.
	var privateKey *signature.SecretKey
	if cmd.PrivateKey != "" {
		keyData, err := os.ReadFile(cmd.PrivateKey)
		if err != nil {
			return err
		}
		key, err := signature.LoadSecretKey(string(keyData))
		if err != nil {
			return err
		}
		privateKey = &key
		log.Info("loaded private key for signing", slog.String("key", key.ToPublicKey().String()))
	}

	// Create HTTP server.
	metrics, err := depotmetrics.New()
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	go func() {
		if err := depotmetrics.ListenAndServe(cmd.MetricsListenAddr); err != nil {
			log.Error("metrics server exited", slog.String("addr", cmd.MetricsListenAddr), slog.String("error", err.Error()))
		}
	}()

	// Create logged storage to track usage metrics.
	al := accesslog.New(store)
	sctx := context.Background()
	nixStorage, nixStorageShutdown, nixStorageErr := cmd.createStorage(sctx, log, "nix", al, metrics)
	npmStorage, npmStorageShutdown, npmStorageErr := cmd.createStorage(sctx, log, "npm", al, metrics)
	pythonStorage, pythonStorageShutdown, pythonStorageErr := cmd.createStorage(sctx, log, "python", al, metrics)
	if err = errors.Join(nixStorageErr, npmStorageErr, pythonStorageErr); err != nil {
		return err
	}

	s := http.Server{
		Addr:    cmd.ListenAddr,
		Handler: routes.New(log, nixdb.New(store), nixStorage, npmdb.New(store), npmStorage, pythondb.New(store), pythonStorage, authConfig, privateKey, metrics),
	}
	log.Info("starting server", slog.String("addr", cmd.ListenAddr), slog.String("metricsAddr", cmd.MetricsListenAddr), slog.String("storePath", cmd.StorePath))
	err = s.ListenAndServe()
	log.Debug("server exited", slog.String("error", err.Error()))
	log.Debug("waiting 30s for nix storage to finish processing events")
	nixStorageShutdown(30 * time.Second)
	log.Debug("waiting 30s for npm storage to finish processing events")
	npmStorageShutdown(30 * time.Second)
	log.Debug("waiting 30s for python storage to finish processing events")
	pythonStorageShutdown(30 * time.Second)
	log.Info("server shutdown complete")
	return err
}

func (cmd *ServeCmd) createStorage(ctx context.Context, log *slog.Logger, prefix string, al *accesslog.AccessLog, m metrics.Metrics) (s storage.Storage, shutdown func(timeout time.Duration) error, err error) {
	var baseStorage storage.Storage
	switch cmd.StorageType {
	case "s3":
		baseStorage, err = storage.NewS3(ctx, storage.S3Config{
			Bucket:          cmd.S3.Bucket,
			Prefix:          prefix + "/",
			Region:          cmd.S3.Region,
			Endpoint:        cmd.S3.Endpoint,
			AccessKeyID:     cmd.S3.AccessKeyID,
			SecretAccessKey: cmd.S3.SecretAccessKey,
			ForcePathStyle:  cmd.S3.ForcePathStyle,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create s3 storage: %w", err)
		}
	case "fs":
		baseStorage = storage.NewFileSystem(filepath.Join(cmd.StorePath, prefix))
	default:
		return nil, nil, fmt.Errorf("unknown storage type %q", cmd.StorageType)
	}

	// Wrap the filesystem in access control.
	s, shutdown = loggedstorage.New(ctx, log, baseStorage, al, m)
	return s, shutdown, nil
}

type ProxyCmd struct {
	Target string `arg:"" help:"Target cache URL to proxy to"`
	Port   int    `help:"Port to listen on (0 for random port)" default:"43407"`
}

func (cmd *ProxyCmd) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	return push.RunProxy(log, cmd.Target, cmd.Port)
}

func main() {
	cli := CLI{
		Globals: globals.Globals{},
	}

	ctx := kong.Parse(&cli,
		kong.Name("depot"),
		kong.Description("Serve Nix, NPM, and Python packages"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)
	err := ctx.Run(&cli.Globals)
	ctx.FatalIfErrorf(err)
}
