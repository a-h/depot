package save

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"strings"

	"github.com/a-h/depot/gomod/download"
	"github.com/a-h/depot/storage"
	"golang.org/x/mod/modfile"
)

// Saver saves Go modules from various sources.
type Saver struct {
	log        *slog.Logger
	downloader *download.Downloader
}

// New creates a new Saver.
func New(log *slog.Logger, storage storage.Storage) *Saver {
	return &Saver{
		log:        log,
		downloader: download.New(log, storage),
	}
}

// SetProxyURL overrides the upstream proxy URL for testing.
func (s *Saver) SetProxyURL(url string) {
	s.downloader.SetProxyURL(url)
}

// Save saves modules specified as "module@version" strings or a path to go.mod.
func (s *Saver) Save(ctx context.Context, specs []string) error {
	if len(specs) == 0 {
		return fmt.Errorf("no modules specified")
	}

	// If a single argument is a go.mod file, parse it.
	if len(specs) == 1 && strings.HasSuffix(specs[0], "go.mod") {
		return s.saveFromGoMod(ctx, specs[0])
	}

	moduleSpecs := make([]download.ModuleSpec, len(specs))
	for i, spec := range specs {
		moduleSpecs[i] = download.ParseModuleSpec(strings.TrimSpace(spec))
	}
	return s.saveModules(ctx, moduleSpecs)
}

func (s *Saver) saveFromGoMod(ctx context.Context, goModPath string) error {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("failed to read go.mod: %w", err)
	}

	f, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return fmt.Errorf("failed to parse go.mod: %w", err)
	}

	var specs []download.ModuleSpec
	for _, req := range f.Require {
		specs = append(specs, download.ModuleSpec{
			Path:    req.Mod.Path,
			Version: req.Mod.Version,
		})
	}

	if len(specs) == 0 {
		return fmt.Errorf("no dependencies found in %s", goModPath)
	}

	s.log.Info("parsed go.mod", slog.String("file", goModPath), slog.Int("dependencies", len(specs)))
	return s.saveModules(ctx, specs)
}

func (s *Saver) saveModules(ctx context.Context, specs []download.ModuleSpec) error {
	alreadySeen := make(map[string]bool)
	specIter := newSliceIterator(specs)

	for spec := range specIter.iterate() {
		key := spec.String()
		if alreadySeen[key] {
			continue
		}
		alreadySeen[key] = true

		// Resolve latest version if not specified.
		if spec.Version == "" {
			version, err := s.downloader.ResolveLatest(ctx, spec.Path)
			if err != nil {
				return fmt.Errorf("failed to resolve latest version for %s: %w", spec.Path, err)
			}
			spec.Version = version
			s.log.Info("resolved latest version", slog.String("module", spec.Path), slog.String("version", version))
		}

		// Download the module files and get the go.mod content. Since Go 1.17,
		// go.mod includes all transitive dependencies (both direct and indirect),
		// so we recursively resolve by reading each downloaded module's go.mod.
		// go.sum is only for integrity verification and is not needed for resolution.
		goModContent, err := s.downloader.Download(ctx, spec.Path, spec.Version)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", spec.String(), err)
		}
		s.log.Info("downloaded module", slog.String("module", spec.String()))

		// Parse the downloaded go.mod for transitive dependencies.
		deps := parseTransitiveDeps(s.log, goModContent)
		for _, dep := range deps {
			if alreadySeen[dep.String()] {
				continue
			}
			specIter.append(dep)
		}
	}

	s.log.Info("all modules saved", slog.Int("total", len(alreadySeen)))
	return nil
}

// parseTransitiveDeps extracts dependencies from a go.mod file, respecting replace directives.
func parseTransitiveDeps(log *slog.Logger, goModContent []byte) []download.ModuleSpec {
	if len(goModContent) == 0 {
		return nil
	}
	f, err := modfile.Parse("go.mod", goModContent, nil)
	if err != nil {
		log.Warn("failed to parse downloaded go.mod for transitive deps", slog.String("error", err.Error()))
		return nil
	}

	// Build replacement map.
	replacements := make(map[string]modfile.Replace)
	for _, rep := range f.Replace {
		replacements[rep.Old.Path] = *rep
	}

	var deps []download.ModuleSpec
	for _, req := range f.Require {
		modPath := req.Mod.Path
		modVersion := req.Mod.Version

		if rep, ok := replacements[modPath]; ok {
			// Skip local path replacements.
			if isLocalPath(rep.New.Path) {
				log.Warn("skipping local path replacement, module will be missing from mirror", slog.String("module", modPath), slog.String("replacement", rep.New.Path))
				continue
			}
			modPath = rep.New.Path
			if rep.New.Version != "" {
				modVersion = rep.New.Version
			}
		}

		deps = append(deps, download.ModuleSpec{
			Path:    modPath,
			Version: modVersion,
		})
	}
	return deps
}

func isLocalPath(p string) bool {
	return strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~/")
}

func newSliceIterator[T any](slice []T) *sliceIterator[T] {
	return &sliceIterator[T]{slice: slice}
}

type sliceIterator[T any] struct {
	slice []T
}

func (it *sliceIterator[T]) append(v T) {
	it.slice = append(it.slice, v)
}

func (it *sliceIterator[T]) iterate() iter.Seq[T] {
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
