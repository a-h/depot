package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/a-h/depot/auth"
	nixdb "github.com/a-h/depot/nix/db"
	"github.com/a-h/depot/nix/push"
	npmdb "github.com/a-h/depot/npm/db"
	"github.com/a-h/depot/routes"
	"github.com/a-h/depot/store"
	"github.com/alecthomas/kong"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

type Globals struct {
	Verbose bool `help:"Enable verbose logging" short:"v" default:"false"`
}

type CLI struct {
	Globals
	Version VersionCmd `cmd:"" help:"Show version information"`
	Serve   ServeCmd   `cmd:"" help:"Start the depot server"`
	Proxy   ProxyCmd   `cmd:"" help:"Proxy requests to a remote depot with authentication"`
	Push    PushCmd    `cmd:"" help:"Push packages to a remote depot"`
}

var Version = "dev"

type VersionCmd struct{}

func (cmd *VersionCmd) Run(globals *Globals) error {
	fmt.Printf("%s", Version)
	return nil
}

type ServeCmd struct {
	DatabaseType string `help:"Choice of database (sqlite, rqlite or postgres)" default:"sqlite" enum:"sqlite,rqlite,postgres" env:"DEPOT_DATABASE_TYPE"`
	DatabaseURL  string `help:"Database connection URL" default:"file:depot-nix-store/depot.db?mode=rwc" env:"DEPOT_DATABASE_URL"`
	ListenAddr   string `help:"Address to listen on" default:":8080" env:"DEPOT_LISTEN_ADDR"`
	StorePath    string `help:"Path to file store" default:"./depot-store" env:"DEPOT_STORE_PATH"`
	AuthFile     string `help:"Path to SSH public keys auth file (format: r/w ssh-key comment)" env:"DEPOT_AUTH_FILE"`
	PrivateKey   string `help:"Path to private key file for signing narinfo files" env:"DEPOT_PRIVATE_KEY"`
}

func (cmd *ServeCmd) Run(globals *Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	// Create a new store.
	store, closer, err := store.New(context.Background(), cmd.DatabaseType, cmd.DatabaseURL)
	if err != nil {
		log.Error("failed to connect to database", slog.String("error", err.Error()))
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

func (cmd *ProxyCmd) Run(globals *Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	return push.RunProxy(log, cmd.Target, cmd.Port)
}

type PushCmd struct {
	Nix NixPushCmd `cmd:"" help:"Push Nix store paths and flake references to a remote depot"`
}

type NixPushCmd struct {
	Target     string   `arg:"" help:"Target cache URL to push to"`
	Stdin      bool     `help:"Read store paths and flake references from stdin" default:"false"`
	FlakeRefs  []string `help:"Flake references to push"`
	StorePaths []string `help:"Store paths to push"`
}

func (cmd *NixPushCmd) Run(globals *Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	pusher := push.New(log, cmd.Target)

	if cmd.Stdin {
		return pusher.PushFromStdin()
	}

	// Push flake references.
	for _, flakeRef := range cmd.FlakeRefs {
		if err := pusher.PushFlakeReference(flakeRef); err != nil {
			return fmt.Errorf("failed to push flake reference %s: %w", flakeRef, err)
		}
	}

	// Push store paths.
	if len(cmd.StorePaths) > 0 {
		return pusher.PushStorePaths(cmd.StorePaths)
	}

	if len(cmd.FlakeRefs) == 0 && len(cmd.StorePaths) == 0 && !cmd.Stdin {
		return fmt.Errorf("no store paths or flake references specified")
	}

	return nil
}

func main() {
	cli := CLI{
		Globals: Globals{},
	}

	ctx := kong.Parse(&cli,
		kong.Name("depot"),
		kong.Description("Serve your local Nix store over HTTP"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)
	err := ctx.Run(&cli.Globals)
	ctx.FatalIfErrorf(err)
}
