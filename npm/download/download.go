package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"log/slog"

	"github.com/Masterminds/semver/v3"
	"github.com/a-h/depot/npm/models"
	"github.com/a-h/depot/npm/sri"
	"github.com/a-h/depot/storage"
)

const npmRegistryURL = "https://registry.npmjs.org"

// PackageSpec represents a package specification (name@version).
type PackageSpec struct {
	Name    string
	Version string
}

func (pkg PackageSpec) String() string {
	if pkg.Version == "" {
		return pkg.Name
	}
	return fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
}

// ParsePackageSpec parses a package specification string.
func ParsePackageSpec(spec string) PackageSpec {
	parts := strings.Split(spec, "@")
	if len(parts) == 1 {
		return PackageSpec{Name: parts[0], Version: "latest"}
	}
	// Handle scoped packages like @types/node@1.0.0.
	if strings.HasPrefix(spec, "@") && len(parts) > 2 {
		return PackageSpec{
			Name:    strings.Join(parts[:2], "@"),
			Version: parts[2],
		}
	}
	return PackageSpec{Name: parts[0], Version: parts[1]}
}

// Downloader handles concurrent package downloads.
type Downloader struct {
	log     *slog.Logger
	client  *http.Client
	storage storage.Storage
}

// New creates a new downloader.
func New(log *slog.Logger, storage storage.Storage) *Downloader {
	return &Downloader{
		log: log,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		storage: storage,
	}
}

func (d *Downloader) findVersion(versionConstraint string, versions map[string]models.AbbreviatedVersion, distTags map[string]string) (models.AbbreviatedVersion, bool) {
	if versionConstraint == "" {
		versionConstraint = "latest"
	}

	// Check if the version constraint is a dist-tag.
	for tag, version := range distTags {
		if tag == versionConstraint {
			if v, ok := versions[version]; ok {
				return v, true
			}
			// Fall back to latest if the dist-tag version is not found.
			version = "latest"
		}
	}

	c, err := semver.NewConstraint(versionConstraint)
	if err != nil {
		return models.AbbreviatedVersion{}, false
	}
	var vers semver.Collection
	for _, v := range versions {
		sv, err := semver.NewVersion(v.Version)
		if err != nil {
			continue
		}
		vers = append(vers, sv)
	}
	sort.Sort(vers)

	// Find the highest version that satisfies the constraint.
	for i := len(vers) - 1; i >= 0; i-- {
		if c.Check(vers[i]) {
			v := vers[i]
			if version, ok := versions[v.Original()]; ok {
				return version, true
			}
		}
	}

	return models.AbbreviatedVersion{}, false
}

func (d *Downloader) Download(ctx context.Context, pkg PackageSpec, updateMetadata, overwriteTar bool) (deps []PackageSpec, err error) {
	d.log.Debug("fetching metadata", slog.String("pkg", pkg.String()))
	metadata, err := d.fetchMetadata(ctx, pkg.Name, updateMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata for package %s: %w", pkg.Name, err)
	}

	d.log.Debug("resolving version", slog.String("name", pkg.Name), slog.String("version", pkg.Version))
	version, ok := d.findVersion(pkg.Version, metadata.Versions, metadata.DistTags)
	if !ok {
		return nil, fmt.Errorf("version %s not found for package %s", pkg.Version, pkg.Name)
	}

	d.log.Debug("downloading tarball", slog.String("name", pkg.Name), slog.String("version", version.Version))
	if err := d.downloadTarball(ctx, version, overwriteTar); err != nil {
		return nil, err
	}

	d.log.Debug("collating dependencies", slog.String("name", pkg.Name), slog.String("version", version.Version))
	for depName, depVersion := range version.Dependencies {
		if !isValidPackage(depName, depVersion) {
			continue
		}
		deps = append(deps, PackageSpec{Name: depName, Version: depVersion})
	}

	return deps, nil
}

func isValidPackage(name, version string) bool {
	if name == "" || version == "" {
		return false
	}
	if strings.HasPrefix(version, "file:") {
		return false
	}
	if strings.HasPrefix(version, ".") || strings.HasPrefix(version, "/") || strings.HasPrefix(version, "~/") {
		return false
	}
	return true
}

func (d *Downloader) fetchMetadata(ctx context.Context, packageName string, updateMetadata bool) (m models.AbbreviatedPackage, err error) {
	fileName := filepath.Join(packageName, "metadata.json")

	if !updateMetadata {
		r, exists, err := d.storage.Get(fileName)
		if err != nil {
			return m, fmt.Errorf("failed to open existing metadata file: %w", err)
		}
		if exists {
			defer r.Close()
			if err := json.NewDecoder(r).Decode(&m); err != nil {
				return m, fmt.Errorf("failed to decode existing metadata file: %w", err)
			}
			return m, nil
		}
	}

	url := fmt.Sprintf("%s/%s", npmRegistryURL, packageName)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return m, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return m, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	f, err := d.storage.Put(fileName)
	if err != nil {
		return m, fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer f.Close()

	// Copy response to the file and JSON decoder.
	rr := io.TeeReader(resp.Body, f)
	if err := json.NewDecoder(rr).Decode(&m); err != nil {
		return m, err
	}

	return m, nil
}

func (d *Downloader) downloadTarball(ctx context.Context, version models.AbbreviatedVersion, overwrite bool) error {
	if version.Dist == nil {
		return fmt.Errorf("no dist information for version %s@%s", version.Name, version.Version)
	}

	filePath := filepath.Join(version.Name, fmt.Sprintf("%s-%s.tgz", version.Name, version.Version))
	if !overwrite {
		_, exists, err := d.storage.Stat(filePath)
		if err != nil {
			return fmt.Errorf("failed to check if file exists: %w", err)
		}
		if exists {
			d.log.Debug("skipping existing tarball", slog.String("file", filePath))
			return nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", version.Dist.Tarball, nil)
	if err != nil {
		return err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Create file.
	file, err := d.storage.Put(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Download with streaming hash verification and progress reporting.
	hasher, err := sri.Parse(version.Dist.Integrity)
	if err != nil {
		return err
	}
	mw := io.MultiWriter(file, hasher)
	if _, err = io.Copy(mw, resp.Body); err != nil {
		return err
	}

	// Verify hash.
	if hasher.String() != version.Dist.Integrity {
		return fmt.Errorf("hash mismatch: expected %s, got %s", version.Dist.Integrity, hasher.String())
	}

	return nil
}
