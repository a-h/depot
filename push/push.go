package push

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/a-h/depot/nixcmd"
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

// PushStorePaths pushes individual store paths to the cache.
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

	// Copy paths to the proxy (which forwards to the target).
	return nixcmd.CopyTo(os.Stdout, os.Stderr, ".", proxyURL, false, paths...)
}

// PushFlakeReference pushes a flake reference to the cache.
func (p *Push) PushFlakeReference(flakeRef string) error {
	// Start proxy server.
	addr, cleanup, err := proxy.StartProxy(p.log, p.target)
	if err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	defer cleanup()

	proxyURL := "http://" + addr
	p.log.Info("started proxy", slog.String("addr", addr), slog.String("target", p.target))

	// Archive the flake to the proxy (which forwards to the target).
	return nixcmd.FlakeArchive(os.Stdout, os.Stderr, proxyURL, flakeRef)
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

	// Push store paths.
	if len(storePaths) > 0 {
		p.log.Info("pushing store paths", slog.Int("count", len(storePaths)))
		if err := nixcmd.CopyTo(os.Stdout, os.Stderr, ".", proxyURL, false, storePaths...); err != nil {
			return fmt.Errorf("failed to push store paths: %w", err)
		}
	}

	// Push flake references.
	for _, flakeRef := range flakeRefs {
		p.log.Info("pushing flake reference", slog.String("ref", flakeRef))
		if err := nixcmd.FlakeArchive(os.Stdout, os.Stderr, proxyURL, flakeRef); err != nil {
			return fmt.Errorf("failed to push flake reference %s: %w", flakeRef, err)
		}
	}

	return nil
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
