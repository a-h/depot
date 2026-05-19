package push

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Pusher uploads saved Go modules to a remote depot.
type Pusher struct {
	log    *slog.Logger
	client *http.Client
	target string
	token  string
}

// New creates a new Pusher.
func New(log *slog.Logger, target string) *Pusher {
	return &Pusher{
		log:    log,
		client: &http.Client{Timeout: 60 * time.Second},
		target: strings.TrimSuffix(target, "/"),
	}
}

// SetAuthToken sets the bearer token for authentication.
func (p *Pusher) SetAuthToken(token string) {
	p.token = token
}

// Push uploads all saved Go module files to the remote depot.
// Files are pushed in order: .info, .mod, then .zip.
func (p *Pusher) Push(ctx context.Context, baseDir string) error {
	var infoFiles, modFiles, zipFiles []string

	err := filepath.Walk(baseDir, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(baseDir, fpath)
		if err != nil {
			return err
		}
		switch {
		case strings.HasSuffix(rel, ".info"):
			infoFiles = append(infoFiles, rel)
		case strings.HasSuffix(rel, ".mod"):
			modFiles = append(modFiles, rel)
		case strings.HasSuffix(rel, ".zip"):
			zipFiles = append(zipFiles, rel)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	total := len(infoFiles) + len(modFiles) + len(zipFiles)
	if total == 0 {
		return fmt.Errorf("no module files found in directory %s", baseDir)
	}

	// Push in order: .info first (creates DB record), then .mod (updates it), then .zip.
	for _, groups := range [][]string{infoFiles, modFiles, zipFiles} {
		for _, rel := range groups {
			if err := p.pushFile(ctx, baseDir, rel); err != nil {
				return err
			}
		}
	}

	p.log.Info("all module files pushed", slog.Int("count", total))
	return nil
}

func (p *Pusher) pushFile(ctx context.Context, baseDir, rel string) error {
	fpath := filepath.Join(baseDir, rel)
	f, err := os.Open(fpath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", fpath, err)
	}
	defer f.Close()

	// The relative path components are already module-escaped on disk (written by the
	// downloader using module.EscapePath and module.EscapeVersion), so they are safe to
	// use directly in the URL. We only need to convert OS path separators to slashes.
	urlPath := filepath.ToSlash(rel)
	url := fmt.Sprintf("%s/go/%s", p.target, urlPath)

	p.log.Debug("pushing file", slog.String("path", urlPath))
	if err := p.putData(ctx, url, f); err != nil {
		return fmt.Errorf("failed to push %s: %w", urlPath, err)
	}

	p.log.Info("pushed file", slog.String("path", urlPath))
	return nil
}

func (p *Pusher) putData(ctx context.Context, url string, data io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, data)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
