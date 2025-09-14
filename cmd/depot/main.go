package main

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/a-h/depot/handlers"
	"github.com/alecthomas/kong"
	"github.com/nix-community/go-nix/pkg/sqlite"
)

type Globals struct {
	Verbose bool `help:"Enable verbose logging" short:"v" default:"false"`
}

type CLI struct {
	Globals
	Serve ServeCmd `cmd:"" help:"Serve local Nix store as a publicly accessible"`
}

type ServeCmd struct {
	ListenAddr string `help:"Address to listen on" default:":8080"`
	StorePath  string `help:"Path to Nix store" default:"/nix"`
}

func (cmd *ServeCmd) Run(globals *Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	uri := filepath.Join(cmd.StorePath, "var", "nix", "db", "db.sqlite")
	sqlDB, db, err := sqlite.NixV10(uri)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	// Create HTTP server.
	s := http.Server{
		Addr:    cmd.ListenAddr,
		Handler: handlers.New(log, db, cmd.StorePath),
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
