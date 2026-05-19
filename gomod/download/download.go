package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/a-h/depot/storage"
	"golang.org/x/mod/module"
)

const defaultProxyURL = "https://proxy.golang.org"

// ModuleSpec represents a module path and optional version.
type ModuleSpec struct {
	Path    string
	Version string
}

func (m ModuleSpec) String() string {
	if m.Version == "" {
		return m.Path
	}
	return m.Path + "@" + m.Version
}

// ParseModuleSpec parses a "module@version" string.
func ParseModuleSpec(spec string) ModuleSpec {
	// Use LastIndex-based cut because module paths can contain "@" in scoped names.
	if path, version, ok := strings.Cut(spec, "@"); ok {
		return ModuleSpec{Path: path, Version: version}
	}
	return ModuleSpec{Path: spec}
}

// Downloader fetches Go modules from an upstream proxy.
type Downloader struct {
	log      *slog.Logger
	client   *http.Client
	storage  storage.Storage
	proxyURL string
}

// New creates a new Downloader.
func New(log *slog.Logger, storage storage.Storage) *Downloader {
	return &Downloader{
		log:      log,
		client:   &http.Client{Timeout: 5 * time.Minute},
		storage:  storage,
		proxyURL: defaultProxyURL,
	}
}

// SetProxyURL overrides the upstream proxy URL.
func (d *Downloader) SetProxyURL(url string) {
	d.proxyURL = strings.TrimSuffix(url, "/")
}

// ResolveLatest resolves the latest version for a module.
func (d *Downloader) ResolveLatest(ctx context.Context, modulePath string) (version string, err error) {
	encoded, err := module.EscapePath(modulePath)
	if err != nil {
		return "", fmt.Errorf("failed to encode module path: %w", err)
	}
	url := fmt.Sprintf("%s/%s/@latest", d.proxyURL, encoded)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	var info struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("failed to decode latest info: %w", err)
	}
	return info.Version, nil
}

// Download fetches .info, .mod, and .zip for a module version and stores them.
// It returns the raw go.mod content for dependency resolution.
func (d *Downloader) Download(ctx context.Context, modulePath, version string) (goModContent []byte, err error) {
	encoded, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, fmt.Errorf("failed to encode module path: %w", err)
	}
	escaped, err := module.EscapeVersion(version)
	if err != nil {
		return nil, fmt.Errorf("failed to escape version: %w", err)
	}

	base := path.Join(encoded, "@v", escaped)

	if _, err := d.downloadFile(ctx, encoded, escaped, base+".info"); err != nil {
		return nil, fmt.Errorf("failed to download .info: %w", err)
	}

	goModContent, err = d.downloadFile(ctx, encoded, escaped, base+".mod")
	if err != nil {
		return nil, fmt.Errorf("failed to download .mod: %w", err)
	}

	if _, err := d.downloadFile(ctx, encoded, escaped, base+".zip"); err != nil {
		return nil, fmt.Errorf("failed to download .zip: %w", err)
	}

	return goModContent, nil
}

// downloadFile downloads a single file from the upstream proxy into storage.
// If the file already exists in storage, it reads and returns the existing content
// without contacting the upstream proxy. Returns the file content.
func (d *Downloader) downloadFile(ctx context.Context, encodedPath, escapedVersion, storageKey string) (content []byte, err error) {
	r, exists, err := d.storage.Get(ctx, storageKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check %s: %w", storageKey, err)
	}
	if exists {
		defer r.Close()
		d.log.Debug("skipping existing file", slog.String("key", storageKey))
		return io.ReadAll(r)
	}

	ext := path.Ext(storageKey)
	url := fmt.Sprintf("%s/%s/@v/%s%s", d.proxyURL, encodedPath, escapedVersion, ext)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	w, err := d.storage.Put(ctx, storageKey)
	if err != nil {
		return nil, err
	}
	defer w.Close()

	content, err = io.ReadAll(io.TeeReader(resp.Body, w))
	if err != nil {
		return nil, err
	}
	return content, nil
}
