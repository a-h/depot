package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/a-h/depot/npm/models"
)

// Push handles uploading NPM packages to a remote depot.
type Push struct {
	log    *slog.Logger
	client *http.Client
	target string
	token  string
}

// New creates a new Push instance.
func New(log *slog.Logger, target string) *Push {
	return &Push{
		log:    log,
		client: &http.Client{Timeout: 60 * time.Second},
		target: strings.TrimSuffix(target, "/"),
	}
}

// SetAuthToken sets the JWT authentication token.
func (p *Push) SetAuthToken(token string) {
	p.token = token
}

// PushPackages pushes all packages from a directory to the remote depot.
func (p *Push) PushPackages(ctx context.Context, baseDir string) error {
	var packageCount int

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), "metadata.json") {
			// Read and unmarshal metadata to get package info.
			metadataData, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read metadata %s: %w", path, err)
			}

			var metadata models.AbbreviatedPackage
			if err := json.Unmarshal(metadataData, &metadata); err != nil {
				return fmt.Errorf("failed to unmarshal metadata %s: %w", path, err)
			}

			packageCount++
			p.log.Info("processing package", slog.String("name", metadata.Name), slog.Int("count", packageCount))

			if err := p.pushPackage(ctx, metadata, filepath.Dir(path)); err != nil {
				return fmt.Errorf("failed to push %s: %w", metadata.Name, err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	if packageCount == 0 {
		return fmt.Errorf("no packages found in directory %s", baseDir)
	}

	p.log.Info("all packages pushed successfully", slog.Int("count", packageCount))
	return nil
}

// pushPackage pushes a single package to the remote depot.
func (p *Push) pushPackage(ctx context.Context, metadata models.AbbreviatedPackage, packageDir string) error {
	p.log.Info("pushing package", slog.String("name", metadata.Name))

	// Push each version.
	for version, versionInfo := range metadata.Versions {
		if _, err := os.Stat(filepath.Join(packageDir, fmt.Sprintf("%s-%s.tgz", versionInfo.Name, versionInfo.Version))); err != nil {
			p.log.Warn("skipping version, tarball not found", slog.String("package", versionInfo.Name), slog.String("version", versionInfo.Version))
			continue
		}
		if err := p.pushVersion(ctx, versionInfo, packageDir); err != nil {
			return fmt.Errorf("failed to push version %s: %w", version, err)
		}
	}

	p.log.Info("package pushed successfully", slog.String("name", metadata.Name))
	return nil
}

// pushVersion pushes a single version of a package.
func (p *Push) pushVersion(ctx context.Context, versionInfo models.AbbreviatedVersion, packageDir string) error {
	p.log.Info("pushing version", slog.String("package", versionInfo.Name), slog.String("version", versionInfo.Version))

	// Find the tarball file.
	tarballName := fmt.Sprintf("%s-%s.tgz", versionInfo.Name, versionInfo.Version)
	tarballPath := filepath.Join(packageDir, tarballName)

	// Open tarball for reading.
	tarballFile, err := os.Open(tarballPath)
	if err != nil {
		return fmt.Errorf("failed to open tarball %s: %w", tarballPath, err)
	}
	defer tarballFile.Close()

	// Push tarball.
	tarballURL := fmt.Sprintf("%s/npm/%s/-/%s", p.target, versionInfo.Name, tarballName)
	if err := p.putData(ctx, tarballURL, tarballFile, "application/octet-stream"); err != nil {
		return fmt.Errorf("failed to push tarball: %w", err)
	}

	// Update version info with new tarball URL.
	versionInfo.Dist.Tarball = tarballURL

	// Push version metadata.
	versionData, err := json.Marshal(versionInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal version metadata: %w", err)
	}

	versionURL := fmt.Sprintf("%s/npm/%s/%s", p.target, versionInfo.Name, versionInfo.Version)
	if err := p.putData(ctx, versionURL, bytes.NewReader(versionData), "application/json"); err != nil {
		return fmt.Errorf("failed to push version metadata: %w", err)
	}

	p.log.Info("version pushed successfully", slog.String("package", versionInfo.Name), slog.String("version", versionInfo.Version))
	return nil
}

// putData performs a PUT request with the given data.
func (p *Push) putData(ctx context.Context, url string, data io.Reader, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, "PUT", url, data)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", contentType)
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
