package save

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/a-h/depot/npm/download"
)

// Saver saves NPM packages from various sources.
type Saver struct {
	log        *slog.Logger
	downloader *download.Downloader
}

// New creates a new Saver instance.
func New(log *slog.Logger, baseDir string) *Saver {
	return &Saver{
		log:        log,
		downloader: download.New(log, baseDir),
	}
}

// Save saves packages from command line arguments.
func (s *Saver) Save(ctx context.Context, packages []string) error {
	if len(packages) == 0 {
		return fmt.Errorf("no packages specified")
	}

	specs := make([]download.PackageSpec, len(packages))
	for i, pkg := range packages {
		specs[i] = download.ParsePackageSpec(strings.TrimSpace(pkg))
	}

	s.log.Info("saving packages", slog.Int("count", len(specs)))
	return s.downloader.DownloadPackages(ctx, specs)
}

// SaveFromReader saves packages from a reader (e.g., stdin or file).
func (s *Saver) SaveFromReader(ctx context.Context, reader io.Reader) error {
	var specs []download.PackageSpec
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments.
		}
		specs = append(specs, download.ParsePackageSpec(line))
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	if len(specs) == 0 {
		return fmt.Errorf("no packages found in input")
	}

	s.log.Info("saving packages from input", slog.Int("count", len(specs)))
	return s.downloader.DownloadPackages(ctx, specs)
}
