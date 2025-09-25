package save

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"strings"

	"github.com/a-h/depot/npm/download"
	"github.com/a-h/depot/storage"
)

// Saver saves NPM packages from various sources.
type Saver struct {
	log        *slog.Logger
	downloader *download.Downloader
}

// New creates a new Saver instance.
func New(log *slog.Logger, storage storage.Storage) *Saver {
	return &Saver{
		log:        log,
		downloader: download.New(log, storage),
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
	alreadySeen := make(map[string]bool)
	specIter := NewSliceIterator(specs)
	for spec := range specIter.Iterate() {
		if alreadySeen[spec.String()] {
			continue
		}
		alreadySeen[spec.String()] = true
		deps, err := s.downloader.Download(ctx, spec, false, false)
		if err != nil {
			return fmt.Errorf("failed to download package %s: %w", spec.String(), err)
		}
		s.log.Info("downloaded package", slog.String("package", spec.String()), slog.Int("dependencies", len(deps)))
		for _, d := range deps {
			if alreadySeen[d.String()] {
				continue
			}
			if strings.HasPrefix(d.Version, "file:") {
				s.log.Error("skipping file: dependency", slog.String("package", spec.String()), slog.String("dependency", d.String()))
			}
			if strings.HasPrefix(d.Version, "npm:") {
				s.log.Info("skipping npm: alias dependency", slog.String("package", spec.String()), slog.String("dependency", d.String()))
				continue
			}
			specIter.Append(d)
		}
	}
	s.log.Info("all packages saved", slog.Int("total", len(alreadySeen)))
	return nil
}

func NewSliceIterator[T any](slice []T) *SliceIterator[T] {
	return &SliceIterator[T]{slice: slice}
}

type SliceIterator[T any] struct {
	slice []T
}

func (it *SliceIterator[T]) Append(v T) {
	it.slice = append(it.slice, v)
}

func (it *SliceIterator[T]) Iterate() iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := 0; ; i++ {
			if i >= len(it.slice) {
				break
			}
			if !yield(it.slice[i]) {
				return
			}
		}
	}
}

// SaveFromReader saves packages from a reader (e.g., stdin or file).
func (s *Saver) SaveFromReader(ctx context.Context, reader io.Reader) error {
	scanner := bufio.NewScanner(reader)

	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments.
		}
		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	if len(lines) == 0 {
		return fmt.Errorf("no packages found in input")
	}

	return s.Save(ctx, lines)
}
