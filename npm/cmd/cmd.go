package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/a-h/depot/cmd/globals"
	"github.com/a-h/depot/npm/pkglock"
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
	Packages []string `arg:"" help:"Package names to save (format: package@version or ./path/to/package-lock.json)" default:"./package-lock.json"`
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

	if len(cmd.Packages) == 1 && strings.HasSuffix(cmd.Packages[0], "package-lock.json") {
		f, err := os.Open(cmd.Packages[0])
		if err != nil {
			return fmt.Errorf("failed to open package-lock.json: %w", err)
		}
		defer f.Close()
		pkgs, err := pkglock.Parse(ctx, f)
		if err != nil {
			return fmt.Errorf("failed to parse package-lock.json: %w", err)
		}
		cmd.Packages = pkgs
	}

	if len(cmd.Packages) == 0 {
		return fmt.Errorf("no packages specified and stdin not enabled")
	}

	return saver.Save(ctx, cmd.Packages)
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
