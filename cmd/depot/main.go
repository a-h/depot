package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/a-h/depot/auth"
	"github.com/a-h/depot/db"
	"github.com/a-h/depot/handlers"
	"github.com/a-h/depot/push"
	"github.com/alecthomas/kong"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

type Globals struct {
	Verbose bool `help:"Enable verbose logging" short:"v" default:"false"`
}

type CLI struct {
	Globals
	Serve ServeCmd `cmd:"" help:"Serve local Nix store as a publicly accessible"`
	Proxy ProxyCmd `cmd:"" help:"Proxy requests to a remote cache with authentication"`
	Push  PushCmd  `cmd:"" help:"Push store paths and flake references to a cache"`
}

type ServeCmd struct {
	ListenAddr string `help:"Address to listen on" default:":8080"`
	StorePath  string `help:"Path to Nix store" default:"./depot-nix-store"`
	CacheURL   string `help:"URL of the binary cache" default:"http://localhost:8080"`
	AuthFile   string `help:"Path to SSH public keys auth file (format: r/w ssh-key comment)" env:"DEPOT_AUTH_FILE"`
	PrivateKey string `help:"Path to private key file for signing narinfo files" env:"DEPOT_PRIVATE_KEY"`
}

type ProxyCmd struct {
	Target string `arg:"" help:"Target cache URL to proxy to"`
	Port   int    `help:"Port to listen on (0 for random port)" default:"43407"`
}

type PushCmd struct {
	Target     string   `arg:"" help:"Target cache URL to push to"`
	Stdin      bool     `help:"Read store paths and flake references from stdin" default:"false"`
	FlakeRefs  []string `help:"Flake references to push"`
	StorePaths []string `help:"Store paths to push"`
}

func (cmd *ServeCmd) Run(globals *Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	sqlDB, cacheDB, err := db.Init(cmd.StorePath, cmd.CacheURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

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
		Handler: handlers.New(log, cacheDB, cmd.StorePath, authConfig, privateKey),
	}
	log.Info("starting server", slog.String("addr", cmd.ListenAddr), slog.String("storePath", cmd.StorePath))
	return s.ListenAndServe()
}

func (cmd *ProxyCmd) Run(globals *Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	return push.RunProxy(log, cmd.Target, cmd.Port)
}

func (cmd *PushCmd) Run(globals *Globals) error {
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
