package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/a-h/depot/cmd/globals"
	npmpush "github.com/a-h/depot/npm/push"
	"github.com/a-h/depot/npm/save"
	"github.com/a-h/depot/storage"
)

type NPMCmd struct {
	Save Save `cmd:"" help:"Save NPM packages to local store"`
	Push Push `cmd:"" help:"Push NPM packages to remote depot"`
}

type Save struct {
	Dir      string   `help:"Directory to save packages to" default:".depot-storage/npm" env:"DEPOT_NPM_DIR"`
	Packages []string `arg:"" help:"Package names to save (format: package@version)"`
	Stdin    bool     `help:"Read package list from stdin" default:"false"`
}

func (cmd *Save) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	storage := storage.NewFileSystem(cmd.Dir)
	saver := save.New(log, storage)

	if cmd.Stdin {
		return saver.SaveFromReader(context.Background(), os.Stdin)
	}

	if len(cmd.Packages) > 0 {
		return saver.Save(context.Background(), cmd.Packages)
	}

	return fmt.Errorf("no packages specified and stdin not enabled")
}

type Push struct {
	Target string `arg:"" help:"Target depot URL to push to"`
	Dir    string `help:"Directory containing NPM packages to push" default:".depot-storage/npm" env:"DEPOT_NPM_DIR"`
	Token  string `help:"JWT authentication token" env:"DEPOT_AUTH_TOKEN"`
}

func (cmd *Push) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	pusher := npmpush.New(log, cmd.Target)
	if cmd.Token != "" {
		pusher.SetAuthToken(cmd.Token)
	}

	return pusher.Push(context.Background(), cmd.Dir)
}
