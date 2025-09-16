package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/a-h/depot/db"
	"github.com/a-h/depot/handlers"
	"github.com/alecthomas/kong"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

type Globals struct {
	Verbose bool `help:"Enable verbose logging" short:"v" default:"false"`
}

type CLI struct {
	Globals
	Serve   ServeCmd   `cmd:"" help:"Serve local Nix store as a publicly accessible"`
	Keypair KeypairCmd `cmd:"" help:"Manage signing keypairs"`
}

type ServeCmd struct {
	ListenAddr  string `help:"Address to listen on" default:":8080"`
	StorePath   string `help:"Path to Nix store" default:"./depot-nix-store"`
	CacheURL    string `help:"URL of the binary cache" default:"http://localhost:8080"`
	UploadToken string `help:"Token required for uploads (if empty, uploads are not protected)" env:"DEPOT_UPLOAD_TOKEN"`
	PrivateKey  string `help:"Path to private key file for signing narinfo files" env:"DEPOT_PRIVATE_KEY"`
}

type KeypairCmd struct {
	Generate KeypairGenerateCmd `cmd:"" help:"Generate a new signing keypair"`
}

type KeypairGenerateCmd struct {
	Name string `arg:"" help:"Name of the keypair to generate"`
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
		Handler: handlers.New(log, cacheDB, cmd.StorePath, cmd.UploadToken, privateKey),
	}
	log.Info("starting server", slog.String("addr", cmd.ListenAddr), slog.String("storePath", cmd.StorePath))
	return s.ListenAndServe()
}

func (cmd *KeypairGenerateCmd) Run(globals *Globals) error {
	secretKey, publicKey, err := signature.GenerateKeypair(cmd.Name, nil)
	if err != nil {
		return fmt.Errorf("failed to generate keypair: %w", err)
	}

	privateKeyFile := cmd.Name + ".private"
	publicKeyFile := cmd.Name + ".public"

	// Write private key.
	if err := os.WriteFile(privateKeyFile, []byte(secretKey.String()), 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Write public key.
	if err := os.WriteFile(publicKeyFile, []byte(publicKey.String()), 0644); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	fmt.Printf("Generated keypair '%s':\n", cmd.Name)
	fmt.Printf("Private key: %s\n", privateKeyFile)
	fmt.Printf("Public key:  %s\n", publicKeyFile)
	fmt.Printf("\nPublic key value: %s\n", publicKey.String())
	fmt.Printf("\nTo use with depot, add to your configuration:\n")
	fmt.Printf("  depot serve --private-key %s\n", privateKeyFile)
	fmt.Printf("\nTo trust this cache in Nix, add this line to your nix.conf:\n")
	fmt.Printf("  trusted-public-keys = %s\n", publicKey.String())
	fmt.Printf("\nNix configuration files are located at:\n")
	fmt.Printf("  System-wide: /etc/nix/nix.conf\n")
	fmt.Printf("  User-specific: ~/.config/nix/nix.conf\n")

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
