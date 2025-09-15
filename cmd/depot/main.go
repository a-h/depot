package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/a-h/depot/db"
	"github.com/a-h/depot/handlers"
	"github.com/alecthomas/kong"
)

type Globals struct {
	Verbose bool `help:"Enable verbose logging" short:"v" default:"false"`
}

type CLI struct {
	Globals
	Serve ServeCmd `cmd:"" help:"Serve local Nix store as a publicly accessible"`
}

type ServeCmd struct {
	ListenAddr  string `help:"Address to listen on" default:":8080"`
	StorePath   string `help:"Path to Nix store" default:"./depot-nix-store"`
	CacheURL    string `help:"URL of the binary cache" default:"http://localhost:8080"`
	UploadToken string `help:"Token required for uploads (if empty, uploads are not protected)" env:"DEPOT_UPLOAD_TOKEN"`
}

func (cmd *ServeCmd) Run(globals *Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	sqlDB, nixDB, cacheDB, err := db.Init(cmd.StorePath, cmd.CacheURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	// Create HTTP server.
	s := http.Server{
		Addr:    cmd.ListenAddr,
		Handler: handlers.New(log, nixDB, cacheDB, cmd.StorePath, cmd.UploadToken),
	}
	log.Info("starting server", slog.String("addr", cmd.ListenAddr), slog.String("storePath", cmd.StorePath))
	return s.ListenAndServe()
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
