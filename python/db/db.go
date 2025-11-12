package db

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/a-h/depot/python/models"
	"github.com/a-h/kv"
)

func New(store kv.Store) (db *DB) {
	return &DB{store: store}
}

type DB struct {
	store kv.Store
}

// buildPackageKey builds a database key for package metadata.
func (d *DB) buildPackageKey(packageName, version string) string {
	normalizedName := normalizeName(packageName)
	encodedName := url.PathEscape(normalizedName)
	encodedVersion := url.PathEscape(version)
	return path.Join("/python", encodedName, encodedVersion)
}

// normalizeName normalizes a Python package name according to PEP 503.
// Package names are case-insensitive and hyphens/underscores are equivalent.
func normalizeName(name string) string {
	normalized := strings.ToLower(name)
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, ".", "-")
	return normalized
}

// GetPackageVersion retrieves specific version metadata.
func (d *DB) GetPackageVersion(ctx context.Context, packageName, version string) (metadata models.SimpleFileEntry, ok bool, err error) {
	key := d.buildPackageKey(packageName, version)
	_, ok, err = d.store.Get(ctx, key, &metadata)
	if err != nil {
		return models.SimpleFileEntry{}, false, err
	}
	if !ok {
		return models.SimpleFileEntry{}, false, nil
	}
	return metadata, true, nil
}

// GetPackage retrieves all versions of a package.
func (d *DB) GetPackage(ctx context.Context, packageName string, baseURL string) (index models.SimplePackageIndex, err error) {
	normalizedName := normalizeName(packageName)
	encodedName := url.PathEscape(normalizedName)
	prefix := path.Join("/python", encodedName) + "/"

	index = models.SimplePackageIndex{
		Meta: models.SimpleMeta{
			APIVersion: "1.0",
		},
		Name:     packageName,
		Files:    []models.SimpleFileEntry{},
		Versions: []string{},
	}

	records, err := d.store.GetPrefix(ctx, prefix, 0, -1)
	if err != nil {
		return index, err
	}

	if len(records) == 0 {
		return index, nil
	}

	index.Files, err = kv.ValuesOf[models.SimpleFileEntry](records)
	if err != nil {
		return index, err
	}

	seenVersions := make(map[string]bool)
	for i, file := range index.Files {
		v := file.Version()
		if !seenVersions[v] {
			seenVersions[v] = true
			index.Versions = append(index.Versions, v)
		}
		// Normalise the URL.
		index.Files[i].URL = strings.TrimSuffix(baseURL, "/") + "/" + path.Join(file.PackageName(), file.Filename)
	}

	return index, nil
}

// ListPackages lists all package names in the repository.
func (d *DB) ListPackages(ctx context.Context) (packages []string, err error) {
	prefix := "/python/"
	records, err := d.store.GetPrefix(ctx, prefix, 0, -1)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	for _, record := range records {
		// Extract package name from key like "/python/package-name/version"
		parts := strings.Split(strings.TrimPrefix(record.Key, prefix), "/")
		if len(parts) >= 1 && parts[0] != "" {
			if !seen[parts[0]] {
				seen[parts[0]] = true
				packages = append(packages, parts[0])
			}
		}
	}

	return packages, nil
}

// PutPackageVersion saves specific version metadata.
func (d *DB) PutPackageVersion(ctx context.Context, file models.SimpleFileEntry) error {
	key := d.buildPackageKey(file.PackageName(), file.Version())
	return d.store.Put(ctx, key, -1, file)
}

// DeletePackage deletes all versions of a package.
func (d *DB) DeletePackage(ctx context.Context, packageName string) error {
	normalizedName := normalizeName(packageName)
	encodedName := url.PathEscape(normalizedName)
	prefix := path.Join("/python", encodedName) + "/"
	if _, err := d.store.DeletePrefix(ctx, prefix, 0, -1); err != nil {
		return err
	}
	return nil
}

// DeletePackageVersion deletes a specific version of a package.
func (d *DB) DeletePackageVersion(ctx context.Context, packageName, version string) error {
	key := d.buildPackageKey(packageName, version)
	_, err := d.store.Delete(ctx, key)
	return err
}
