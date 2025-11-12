package push

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func New(log *slog.Logger, target string) *Pusher {
	return &Pusher{
		log: log,
		client: &http.Client{
			Timeout: time.Hour,
		},
		target: strings.TrimSuffix(target, "/"),
	}
}

type Pusher struct {
	log       *slog.Logger
	target    string
	authToken string
	client    *http.Client
}

func (p *Pusher) SetAuthToken(token string) {
	p.authToken = token
}

func (p *Pusher) Push(ctx context.Context, dir string) error {
	p.log.Info("pushing Python packages", slog.String("target", p.target), slog.String("dir", dir))

	binaryExtensions := []string{".gz", ".tar.gz", ".whl", ".zip"}
	var metadataFiles []string
	var binaryFiles []string
	err := filepath.Walk(dir, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(name)
		if strings.HasSuffix(name, ".json") {
			metadataFiles = append(metadataFiles, name)
			return nil
		}
		if slices.Contains(binaryExtensions, ext) {
			binaryFiles = append(binaryFiles, name)
			return nil
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	p.log.Info("pushing binary files", slog.Int("count", len(binaryFiles)))
	err = p.pushFiles(ctx, dir, binaryFiles)
	if err != nil {
		return fmt.Errorf("failed to push binary files: %w", err)
	}

	p.log.Info("pushing metadata files", slog.Int("count", len(metadataFiles)))
	err = p.pushFiles(ctx, dir, metadataFiles)
	if err != nil {
		return fmt.Errorf("failed to push metadata files: %w", err)
	}

	p.log.Info("push complete")

	return nil
}

func (p *Pusher) pushFiles(ctx context.Context, dir string, files []string) (err error) {
	for _, file := range files {
		relPath, err := filepath.Rel(dir, file)
		if err != nil {
			return err
		}
		to := p.target + "/python/" + filepath.ToSlash(relPath)

		p.log.Debug("pushing file", slog.String("file", relPath), slog.String("to", to))
		if err := p.pushFile(ctx, file, to); err != nil {
			return fmt.Errorf("failed to push file %s: %w", relPath, err)
		}
	}

	return nil
}

func (p *Pusher) pushFile(ctx context.Context, filePath string, to string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, to, file)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if p.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.authToken)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
