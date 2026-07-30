package push

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/a-h/depot/nix/nixcmd"
	"github.com/a-h/depot/proxy"
)

// Push handles pushing store paths and flake references to a cache via proxy.
type Push struct {
	log       *slog.Logger
	target    string
	transport http.RoundTripper
}

// New creates a new Push instance.
func New(log *slog.Logger, target string, transport http.RoundTripper) *Push {
	return &Push{
		log:       log,
		target:    target,
		transport: transport,
	}
}

// PushStorePaths pushes individual store paths to the cache with comprehensive dependencies.
func (p *Push) PushStorePaths(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	addr, cleanup, err := proxy.StartProxy(p.log, p.target, p.transport)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	targetURL, err := url.Parse(p.target)
	if err != nil {
		return fmt.Errorf("invalid target URL: %w", err)
	}

	proxyURL := url.URL{
		Scheme: "http",
		Host:   addr,
		Path:   targetURL.Path,
	}
	p.log.Info("started proxy", slog.String("url", proxyURL.String()), slog.String("target", p.target))

	for _, path := range paths {
		if err := p.pushComprehensive(ctx, proxyURL.String(), path); err != nil {
			return fmt.Errorf("failed to comprehensively push %s: %w", path, err)
		}
	}

	return nil
}

// PushFlakeReference pushes a flake reference to the cache with comprehensive dependencies.
func (p *Push) PushFlakeReference(ctx context.Context, flakeRef string) error {
	addr, cleanup, err := proxy.StartProxy(p.log, p.target, p.transport)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	targetURL, err := url.Parse(p.target)
	if err != nil {
		return fmt.Errorf("invalid target URL: %w", err)
	}

	proxyURL := url.URL{
		Scheme: "http",
		Host:   addr,
		Path:   targetURL.Path,
	}
	p.log.Info("started proxy", slog.String("url", proxyURL.String()), slog.String("target", p.target))

	return p.pushFlakeComprehensive(ctx, proxyURL.String(), flakeRef)
}

// PushFromStdin reads store paths and flake references from stdin and pushes them.
func (p *Push) PushFromStdin(ctx context.Context) error {
	addr, cleanup, err := proxy.StartProxy(p.log, p.target, p.transport)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	targetURL, err := url.Parse(p.target)
	if err != nil {
		return fmt.Errorf("invalid target URL: %w", err)
	}

	proxyURL := url.URL{
		Scheme: "http",
		Host:   addr,
		Path:   targetURL.Path,
	}
	p.log.Info("started proxy", slog.String("url", proxyURL.String()), slog.String("target", p.target))

	scanner := bufio.NewScanner(os.Stdin)
	var storePaths []string
	var flakeRefs []string

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Determine if this is a store path or flake reference.
		if strings.HasPrefix(line, "/nix/store/") {
			storePaths = append(storePaths, line)
		} else if strings.Contains(line, "#") || strings.Contains(line, ":") {
			// Likely a flake reference.
			flakeRefs = append(flakeRefs, line)
		} else {
			// Assume it's a store path.
			storePaths = append(storePaths, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading from stdin: %w", err)
	}

	for _, path := range storePaths {
		p.log.Info("pushing store path comprehensively", slog.String("path", path))
		if err := p.pushComprehensive(ctx, proxyURL.String(), path); err != nil {
			return fmt.Errorf("failed to push store path %s: %w", path, err)
		}
	}

	for _, flakeRef := range flakeRefs {
		p.log.Info("pushing flake reference comprehensively", slog.String("ref", flakeRef))
		if err := p.pushFlakeComprehensive(ctx, proxyURL.String(), flakeRef); err != nil {
			return fmt.Errorf("failed to push flake reference %s: %w", flakeRef, err)
		}
	}

	return nil
}

func (p *Push) pushComprehensive(ctx context.Context, proxyURL, storePath string) error {
	p.log.Info("getting derivation info", slog.String("path", storePath))

	// Get input derivations and sources for the store path.
	inputDerivations, inputSrcs, err := nixcmd.DerivationShow(ctx, os.Stdout, os.Stderr, ".", storePath)
	if err != nil {
		return fmt.Errorf("failed to get derivation info for %s: %w", storePath, err)
	}

	allInputs := append(inputSrcs, inputDerivations...)
	allPaths := []string{storePath}

	if len(allInputs) > 0 {
		p.log.Info("realising input dependencies", slog.Int("count", len(allInputs)))

		realisedPaths, err := nixcmd.RealiseStorePaths(ctx, os.Stdout, os.Stderr, allInputs...)
		if err != nil {
			return fmt.Errorf("failed to realise input derivations: %w", err)
		}

		allPaths = append(allPaths, realisedPaths...)
	}

	p.log.Info("copying all paths", slog.Int("count", len(allPaths)))

	return nixcmd.CopyTo(ctx, os.Stdout, os.Stderr, ".", proxyURL, false, allPaths...)
}

func (p *Push) pushFlakeComprehensive(ctx context.Context, proxyURL, flakeRef string) error {
	p.log.Info("evaluating flake reference", slog.String("ref", flakeRef))

	// Split flake reference into base flake and attribute.
	// flake archive only accepts the base flake (before #), not attributes.
	baseFlake := flakeRef
	if before, _, ok := strings.Cut(flakeRef, "#"); ok {
		baseFlake = before
	}

	// Archive the flake source.
	if err := nixcmd.FlakeArchive(ctx, os.Stdout, os.Stderr, proxyURL, baseFlake); err != nil {
		return fmt.Errorf("failed to archive flake: %w", err)
	}

	storePath, err := nixcmd.Eval(ctx, os.Stdout, os.Stderr, flakeRef)
	if err != nil {
		return fmt.Errorf("failed to evaluate flake reference: %w", err)
	}

	p.log.Info("flake reference evaluated", slog.String("ref", flakeRef), slog.String("path", storePath))

	return p.pushComprehensive(ctx, proxyURL, storePath)
}

// RunProxy runs a proxy command with simple logging, blocking until ctx is cancelled.
func RunProxy(ctx context.Context, log *slog.Logger, target string, port int, transport http.RoundTripper) error {
	proxyAddr, cleanup, err := proxy.StartProxyOnPort(log, target, transport, port)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	log.Info("proxy running", slog.String("addr", proxyAddr), slog.String("target", target))
	log.Info("press Ctrl+C to stop")

	<-ctx.Done()
	return nil
}
