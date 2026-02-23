package push

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/a-h/depot/nix/nixcmd"
	"github.com/a-h/depot/proxy"
)

// Push handles pushing store paths and flake references to a cache via proxy.
type Push struct {
	log    *slog.Logger
	target string
}

// New creates a new Push instance.
func New(log *slog.Logger, target string) *Push {
	return &Push{
		log:    log,
		target: target,
	}
}

// PushStorePaths pushes individual store paths to the cache with comprehensive dependencies.
func (p *Push) PushStorePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	// Start proxy server.
	addr, cleanup, err := proxy.StartProxy(p.log, p.target)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	proxyURL := "http://" + addr
	p.log.Info("started proxy", slog.String("addr", addr), slog.String("target", p.target))

	// For each store path, do a comprehensive push.
	for _, path := range paths {
		if err := p.pushComprehensive(proxyURL, path); err != nil {
			return fmt.Errorf("failed to comprehensively push %s: %w", path, err)
		}
	}

	return nil
}

// PushFlakeReference pushes a flake reference to the cache with comprehensive dependencies.
func (p *Push) PushFlakeReference(flakeRef string) error {
	// Start proxy server.
	addr, cleanup, err := proxy.StartProxy(p.log, p.target)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	proxyURL := "http://" + addr
	p.log.Info("started proxy", slog.String("addr", addr), slog.String("target", p.target))

	// Do comprehensive push for the flake reference.
	return p.pushFlakeComprehensive(proxyURL, flakeRef)
}

// PushFromStdin reads store paths and flake references from stdin and pushes them.
func (p *Push) PushFromStdin() error {
	// Start proxy server.
	addr, cleanup, err := proxy.StartProxy(p.log, p.target)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	proxyURL := "http://" + addr
	p.log.Info("started proxy", slog.String("addr", addr), slog.String("target", p.target))

	// Read lines from stdin.
	scanner := bufio.NewScanner(os.Stdin)
	var storePaths []string
	var flakeRefs []string

	for scanner.Scan() {
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

	// Push store paths comprehensively.
	for _, path := range storePaths {
		p.log.Info("pushing store path comprehensively", slog.String("path", path))
		if err := p.pushComprehensive(proxyURL, path); err != nil {
			return fmt.Errorf("failed to push store path %s: %w", path, err)
		}
	}

	// Push flake references comprehensively.
	for _, flakeRef := range flakeRefs {
		p.log.Info("pushing flake reference comprehensively", slog.String("ref", flakeRef))
		if err := p.pushFlakeComprehensive(proxyURL, flakeRef); err != nil {
			return fmt.Errorf("failed to push flake reference %s: %w", flakeRef, err)
		}
	}

	return nil
}

// pushComprehensive does a comprehensive push of a store path including all dependencies.
func (p *Push) pushComprehensive(proxyURL, storePath string) error {
	p.log.Info("getting derivation info", slog.String("path", storePath))

	// Get input derivations and sources for the store path.
	inputDerivations, inputSrcs, err := nixcmd.DerivationShow(os.Stdout, os.Stderr, ".", storePath)
	if err != nil {
		return fmt.Errorf("failed to get derivation info for %s: %w", storePath, err)
	}

	// Combine all inputs.
	allInputs := append(inputSrcs, inputDerivations...)

	// Start with the main store path.
	allPaths := []string{storePath}

	if len(allInputs) > 0 {
		p.log.Info("realising input dependencies", slog.Int("count", len(allInputs)))

		// Realise all input derivations.
		realisedPaths, err := nixcmd.RealiseStorePaths(os.Stdout, os.Stderr, allInputs...)
		if err != nil {
			return fmt.Errorf("failed to realise input derivations: %w", err)
		}

		// Add realised dependencies to the paths to copy.
		allPaths = append(allPaths, realisedPaths...)
	}

	p.log.Info("copying all paths", slog.Int("count", len(allPaths)))

	// Copy all paths in one operation.
	return nixcmd.CopyTo(os.Stdout, os.Stderr, ".", proxyURL, false, allPaths...)
}

// pushFlakeComprehensive does a comprehensive push of a flake reference including the package and all dependencies.
func (p *Push) pushFlakeComprehensive(proxyURL, flakeRef string) error {
	p.log.Info("evaluating flake reference", slog.String("ref", flakeRef))

	// Split flake reference into base flake and attribute.
	// flake archive only accepts the base flake (before #), not attributes.
	baseFlake := flakeRef
	if before, _, ok := strings.Cut(flakeRef, "#"); ok {
		baseFlake = before
	}

	// First, archive the flake source.
	if err := nixcmd.FlakeArchive(os.Stdout, os.Stderr, proxyURL, baseFlake); err != nil {
		return fmt.Errorf("failed to archive flake: %w", err)
	}

	// Evaluate the flake reference to get the store path.
	storePath, err := nixcmd.Eval(os.Stdout, os.Stderr, flakeRef)
	if err != nil {
		return fmt.Errorf("failed to evaluate flake reference: %w", err)
	}

	p.log.Info("flake reference evaluated", slog.String("ref", flakeRef), slog.String("path", storePath))

	// Now do comprehensive push of the evaluated store path.
	return p.pushComprehensive(proxyURL, storePath)
}

// RunProxy runs a proxy command with simple logging.
func RunProxy(log *slog.Logger, target string, port int) error {
	// Start proxy on the specified port.
	proxyAddr, cleanup, err := proxy.StartProxyOnPort(log, target, port)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	log.Info("proxy running", slog.String("addr", proxyAddr), slog.String("target", target))
	log.Info("press Ctrl+C to stop")

	// Keep the proxy running until interrupted.
	select {}
}
