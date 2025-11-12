package cmd

import (
	"context"
	"log/slog"
	"os"

	"github.com/a-h/depot/cmd/globals"
	pythonpush "github.com/a-h/depot/python/push"
	"github.com/a-h/depot/python/save"
	"github.com/a-h/depot/storage"
)

type PythonCmd struct {
	Save Save `cmd:"" help:"Save Python packages to local store"`
	Push Push `cmd:"" help:"Push Python packages to remote depot"`
}

type Save struct {
	Dir      string   `help:"Directory to save packages to" default:".depot-storage/python" env:"DEPOT_PYTHON_DIR"`
	Packages []string `arg:"" help:"Package names to save (format: package==version)" optional:"true"`
	Stdin    bool     `help:"Read package list from stdin" default:"false"`
}

func (cmd *Save) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	ctx := context.Background()
	storage := storage.NewFileSystem(cmd.Dir)
	saver := save.New(log, storage)

	if cmd.Stdin {
		return saver.SaveFromReader(ctx, os.Stdin)
	}

	if len(cmd.Packages) == 0 {
		log.Info("no packages specified, reading from stdin")
		return saver.SaveFromReader(ctx, os.Stdin)
	}

	return saver.Save(ctx, cmd.Packages)
}

type Push struct {
	Target string `arg:"" help:"Target depot URL to push to"`
	Dir    string `help:"Directory containing Python packages to push" default:".depot-storage/python" env:"DEPOT_PYTHON_DIR"`
	Token  string `help:"JWT authentication token" env:"DEPOT_AUTH_TOKEN"`
}

func (cmd *Push) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	pusher := pythonpush.New(log, cmd.Target)
	if cmd.Token != "" {
		pusher.SetAuthToken(cmd.Token)
	}

	return pusher.Push(context.Background(), cmd.Dir)
}
