package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/a-h/depot/cmd/globals"
	"github.com/a-h/depot/nix/push"
)

type NixCmd struct {
	Push NixPushCmd `cmd:"" help:"Push Nix store paths and flake references to a remote depot"`
}

type NixPushCmd struct {
	Target     string   `arg:"" help:"Target cache URL to push to"`
	Stdin      bool     `help:"Read store paths and flake references from stdin" default:"false"`
	FlakeRefs  []string `help:"Flake references to push"`
	StorePaths []string `help:"Store paths to push"`
}

func (cmd *NixPushCmd) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	ctx, stop := globals.NewContext()
	defer stop()

	pusher := push.New(log, cmd.Target, globals.NewRoundTripper())

	if cmd.Stdin {
		return pusher.PushFromStdin(ctx)
	}

	for _, flakeRef := range cmd.FlakeRefs {
		if err := pusher.PushFlakeReference(ctx, flakeRef); err != nil {
			return fmt.Errorf("failed to push flake reference %s: %w", flakeRef, err)
		}
	}

	if len(cmd.StorePaths) > 0 {
		return pusher.PushStorePaths(ctx, cmd.StorePaths)
	}

	if len(cmd.FlakeRefs) == 0 && len(cmd.StorePaths) == 0 && !cmd.Stdin {
		return fmt.Errorf("no store paths or flake references specified")
	}

	return nil
}
