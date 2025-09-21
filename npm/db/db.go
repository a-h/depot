package db

import (
	"context"
	"net/url"
	"path"

	"github.com/a-h/depot/npm/models"
	"github.com/a-h/kv"
)

func New(store kv.Store) (db *DB) {
	return &DB{store: store}
}

type DB struct {
	store kv.Store
}

// buildVersionKey builds a database key for version metadata.
func (d *DB) buildVersionKey(packageName, version string) string {
	encodedName := url.PathEscape(packageName)
	encodedVersion := url.PathEscape(version)
	return path.Join("/npm", encodedName, encodedVersion)
}

// GetPackageVersion retrieves specific version metadata.
func (d *DB) GetPackageVersion(ctx context.Context, packageName, version string) (metadata models.AbbreviatedVersion, ok bool, err error) {
	key := d.buildVersionKey(packageName, version)
	_, ok, err = d.store.Get(ctx, key, &metadata)
	if err != nil {
		return models.AbbreviatedVersion{}, false, err
	}
	if !ok {
		return models.AbbreviatedVersion{}, false, nil
	}
	return metadata, true, nil
}

// GetPackage retrieves complete package metadata with all versions.
// This builds the AbbreviatedMetadata format that NPM clients expect.
func (d *DB) GetPackage(ctx context.Context, packageName string) (metadata models.AbbreviatedPackage, ok bool, err error) {
	encodedName := url.PathEscape(packageName)
	prefix := path.Join("/npm", encodedName) + "/"

	records, err := d.store.GetPrefix(ctx, prefix, 0, -1)
	if err != nil {
		return models.AbbreviatedPackage{}, false, err
	}

	if len(records) == 0 {
		return models.AbbreviatedPackage{}, false, nil
	}

	versions := make(map[string]models.AbbreviatedVersion)
	var latestVersion string

	for _, record := range records {
		// Extract version from key safely using path.Base.
		versionPart := path.Base(record.Key)

		// Decode the version.
		if decodedVersion, err := url.PathUnescape(versionPart); err == nil {
			// Get the version metadata.
			var versionMetadata models.AbbreviatedVersion
			if _, ok, err := d.store.Get(ctx, record.Key, &versionMetadata); err == nil && ok {
				versions[decodedVersion] = versionMetadata
				latestVersion = decodedVersion // Simple latest selection - should use semver in production.
			}
		}
	}

	if len(versions) == 0 {
		return models.AbbreviatedPackage{}, false, nil
	}

	// Build complete package metadata.
	packageMetadata := models.AbbreviatedPackage{
		Name:     packageName,
		Versions: versions,
		DistTags: map[string]string{
			"latest": latestVersion,
		},
	}

	return packageMetadata, true, nil
}

// PutPackageVersion saves specific version metadata.
func (d *DB) PutPackageVersion(ctx context.Context, packageName, version string, metadata models.AbbreviatedVersion) error {
	key := d.buildVersionKey(packageName, version)
	return d.store.Put(ctx, key, -1, metadata)
}

// DeletePackage deletes all versions of a package.
func (d *DB) DeletePackage(ctx context.Context, packageName string) error {
	// Delete all versions.
	encodedName := url.PathEscape(packageName)
	prefix := path.Join("/npm", encodedName) + "/"

	records, err := d.store.GetPrefix(ctx, prefix, 0, 1000)
	if err != nil {
		return err
	}

	for _, record := range records {
		if _, err := d.store.Delete(ctx, record.Key); err != nil {
			return err
		}
	}

	return nil
}

// DeletePackageVersion deletes a specific version of a package.
func (d *DB) DeletePackageVersion(ctx context.Context, packageName, version string) error {
	key := d.buildVersionKey(packageName, version)
	_, err := d.store.Delete(ctx, key)
	return err
}
