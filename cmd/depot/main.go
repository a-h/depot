package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/a-h/depot/auth"
	"github.com/a-h/depot/cmd/globals"
	nixcmd "github.com/a-h/depot/nix/cmd"
	nixdb "github.com/a-h/depot/nix/db"
	"github.com/a-h/depot/nix/push"
	npmcmd "github.com/a-h/depot/npm/cmd"
	npmdb "github.com/a-h/depot/npm/db"

	"github.com/a-h/depot/routes"
	"github.com/a-h/depot/store"
	"github.com/alecthomas/kong"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

type CLI struct {
	globals.Globals
	Version VersionCmd    `cmd:"" help:"Show version information"`
	Serve   ServeCmd      `cmd:"" help:"Start the depot server"`
	Proxy   ProxyCmd      `cmd:"" help:"Proxy requests to a remote depot with authentication"`
	Nix     nixcmd.NixCmd `cmd:"" help:"Nix package management commands"`
	NPM     npmcmd.NPMCmd `cmd:"" help:"NPM package management commands"`
}

var Version = "dev"

type VersionCmd struct{}

func (cmd *VersionCmd) Run(globals *globals.Globals) error {
	fmt.Printf("%s", Version)
	return nil
}

type ServeCmd struct {
	DatabaseType string `help:"Choice of database (sqlite, rqlite or postgres)" default:"sqlite" enum:"sqlite,rqlite,postgres" env:"DEPOT_DATABASE_TYPE"`
	DatabaseURL  string `help:"Database connection URL" default:"" env:"DEPOT_DATABASE_URL"`
	ListenAddr   string `help:"Address to listen on" default:":8080" env:"DEPOT_LISTEN_ADDR"`
	StorePath    string `help:"Path to file store" default:"" env:"DEPOT_STORE_PATH"`
	AuthFile     string `help:"Path to SSH public keys auth file (format: r/w ssh-key comment)" env:"DEPOT_AUTH_FILE"`
	PrivateKey   string `help:"Path to private key file for signing narinfo files" env:"DEPOT_PRIVATE_KEY"`
}

func (cmd *ServeCmd) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))
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
	s := http.Server{
		Addr:    cmd.ListenAddr,
		Handler: routes.New(log, nixdb.New(store), npmdb.New(store), cmd.StorePath, authConfig, privateKey),
	}
	log.Info("starting server", slog.String("addr", cmd.ListenAddr), slog.String("storePath", cmd.StorePath))
	return s.ListenAndServe()
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
		kong.Description("Serve Nix and NPM packages"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)
	err := ctx.Run(&cli.Globals)
	ctx.FatalIfErrorf(err)
}
