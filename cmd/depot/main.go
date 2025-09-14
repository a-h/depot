package main

import (
	"fmt"
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
	ListenAddr string `help:"Address to listen on" default:":8080"`
	StorePath  string `help:"Path to Nix store" default:"/nix"`
}

func (cmd *ServeCmd) Run(globals *Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	db, closer, err := db.New(cmd.StorePath)
	if err != nil {
		log.Error("failed to open Nix database", slog.Any("error", err))
		return fmt.Errorf("failed to open Nix database: %w", err)
	}
	defer closer()

	// Create HTTP server.
	s := http.Server{
		Addr:    cmd.ListenAddr,
		Handler: handlers.New(log, db),
	}
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
