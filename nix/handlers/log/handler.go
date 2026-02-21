package log

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
)

func New(log *slog.Logger) Handler {
	return Handler{
		log: log,
	}
}

type Handler struct {
	log *slog.Logger
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	stderr := bytes.NewBuffer(nil)
	if err := nixLog(r.Context(), w, stderr, r.PathValue("storepath")); err != nil {
		h.log.Error("failed to get nix log", slog.Any("error", err), slog.String("stderr", stderr.String()))
		http.Error(w, "failed to get nix log", http.StatusInternalServerError)
		return
	}
}

func nixLog(ctx context.Context, stdout, stderr io.Writer, storePath string) (err error) {
	nixPath, err := exec.LookPath("nix")
	if err != nil {
		return fmt.Errorf("failed to find nix on path: %w", err)
	}
	cmd := exec.CommandContext(ctx, nixPath, "log", storePath)
	cmd.Stderr = stderr
	cmd.Stdout = stdout
	return cmd.Run()
}
