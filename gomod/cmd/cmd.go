package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/a-h/depot/cmd/globals"
	gopush "github.com/a-h/depot/gomod/push"
	"github.com/a-h/depot/gomod/save"
	"github.com/a-h/depot/storage"
)

// GoCmd groups Go module management commands.
type GoCmd struct {
	Save Save `cmd:"" help:"Save Go modules to local store. Fetches modules and their transitive dependencies from proxy.golang.org. Accepts module@version arguments or a path to a go.mod file."`
	Push Push `cmd:"" help:"Push saved Go modules to a remote depot server."`
}

// Save downloads Go modules from the upstream proxy.
type Save struct {
	Dir     string   `help:"Directory to save modules to." default:".depot-storage/go" env:"DEPOT_GO_DIR"`
	Modules []string `arg:"" help:"Module specs (module@version) or path to go.mod file." default:"./go.mod"`
}

// Run executes the save command.
func (cmd *Save) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	ctx := context.Background()
	s := storage.NewFileSystem(cmd.Dir)
	saver := save.New(log, s)
	return saver.Save(ctx, cmd.Modules)
}

// Push uploads saved Go modules to a remote depot.
type Push struct {
	Target string `arg:"" help:"Target depot URL to push to."`
	Dir    string `help:"Directory containing saved Go modules." default:".depot-storage/go" env:"DEPOT_GO_DIR"`
	Token  string `help:"JWT authentication token." env:"DEPOT_AUTH_TOKEN"`
}

// Run executes the push command.
func (cmd *Push) Run(globals *globals.Globals) error {
	opts := &slog.HandlerOptions{}
	if globals.Verbose {
		opts.Level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, opts))

	if cmd.Target == "" {
		return fmt.Errorf("target URL is required")
	}

	pusher := gopush.New(log, cmd.Target)
	if cmd.Token != "" {
		pusher.SetAuthToken(cmd.Token)
	}
	return pusher.Push(context.Background(), cmd.Dir)
}
