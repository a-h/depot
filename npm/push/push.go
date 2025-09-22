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

// Pusher handles uploading NPM packages to a remote depot.
type Pusher struct {
	log    *slog.Logger
	client *http.Client
	target string
	token  string
}

// New creates a new Push instance.
func New(log *slog.Logger, target string) *Pusher {
	return &Pusher{
		log:    log,
		client: &http.Client{Timeout: 60 * time.Second},
		target: strings.TrimSuffix(target, "/"),
	}
}

// SetAuthToken sets the JWT authentication token.
func (p *Pusher) SetAuthToken(token string) {
	p.token = token
}

// Push pushes all packages from a directory to the remote depot.
func (p *Pusher) Push(ctx context.Context, baseDir string) error {
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
			p.log.Debug("pushing package", slog.String("name", metadata.Name), slog.Int("count", packageCount))

			if err := p.pushPackage(ctx, metadata, filepath.Dir(path)); err != nil {
				return fmt.Errorf("failed to push %s: %w", metadata.Name, err)
			}

			p.log.Info("package pushed", slog.String("name", metadata.Name), slog.Int("count", packageCount))
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
func (p *Pusher) pushPackage(ctx context.Context, metadata models.AbbreviatedPackage, packageDir string) error {
	p.log.Debug("pushing package", slog.String("name", metadata.Name))

	// Push each version that we bothered to download.
	for version, versionInfo := range metadata.Versions {
		if _, err := os.Stat(filepath.Join(packageDir, fmt.Sprintf("%s-%s.tgz", versionInfo.Name, versionInfo.Version))); err != nil {
			p.log.Debug("skipping version, tarball not found", slog.String("package", versionInfo.Name), slog.String("version", versionInfo.Version))
			continue
		}
		// DistTags are a map of tag name to version.
		var tagsForVersion []string
		for tag, taggedVersion := range metadata.DistTags {
			if taggedVersion == version {
				tagsForVersion = append(tagsForVersion, tag)
			}
		}

		if err := p.pushVersion(ctx, versionInfo, tagsForVersion, packageDir); err != nil {
			return fmt.Errorf("failed to push version %s: %w", version, err)
		}
	}

	p.log.Debug("package pushed successfully", slog.String("name", metadata.Name))
	return nil
}

// pushVersion pushes a single version of a package.
func (p *Pusher) pushVersion(ctx context.Context, versionInfo models.AbbreviatedVersion, tagsForVersion []string, packageDir string) error {
	p.log.Debug("pushing version", slog.String("package", versionInfo.Name), slog.String("version", versionInfo.Version))

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

	tagsForVersion = append(tagsForVersion, versionInfo.Version)
	for _, tag := range tagsForVersion {
		versionURL := fmt.Sprintf("%s/npm/%s/%s", p.target, versionInfo.Name, tag)
		if err := p.putData(ctx, versionURL, bytes.NewReader(versionData), "application/json"); err != nil {
			return fmt.Errorf("failed to push version metadata: %w", err)
		}
	}

	p.log.Info("pushed version", slog.String("package", versionInfo.Name), slog.String("version", versionInfo.Version))
	return nil
}

// putData performs a PUT request with the given data.
func (p *Pusher) putData(ctx context.Context, url string, data io.Reader, contentType string) error {
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
