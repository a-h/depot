package db

import (
	"context"
	"path"
	"time"

	"github.com/a-h/kv"
	"golang.org/x/mod/module"
)

// New creates a new DB instance.
func New(store kv.Store) *DB {
	return &DB{store: store}
}

// DB wraps a KV store for Go module version metadata.
type DB struct {
	store kv.Store
}

// ModuleVersion stores metadata for a single module version.
type ModuleVersion struct {
	Info  VersionInfo `json:"info"`
	GoMod string      `json:"goMod"`
}

// VersionInfo matches the JSON format served by the Go module proxy protocol.
type VersionInfo struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

// buildKey builds a database key for a module version.
func buildKey(modulePath, version string) (key string, err error) {
	encoded, err := module.EscapePath(modulePath)
	if err != nil {
		return "", err
	}
	escaped, err := module.EscapeVersion(version)
	if err != nil {
		return "", err
	}
	return path.Join("/go", encoded, escaped), nil
}

// buildPrefix builds a database key prefix for a module path.
func buildPrefix(modulePath string) (prefix string, err error) {
	encoded, err := module.EscapePath(modulePath)
	if err != nil {
		return "", err
	}
	return path.Join("/go", encoded) + "/", nil
}

// PutModuleVersion stores metadata for a module version.
func (d *DB) PutModuleVersion(ctx context.Context, modulePath, version string, mv ModuleVersion) error {
	key, err := buildKey(modulePath, version)
	if err != nil {
		return err
	}
	return d.store.Put(ctx, key, -1, mv)
}

// GetModuleVersion retrieves metadata for a module version.
func (d *DB) GetModuleVersion(ctx context.Context, modulePath, version string) (mv ModuleVersion, ok bool, err error) {
	key, err := buildKey(modulePath, version)
	if err != nil {
		return ModuleVersion{}, false, err
	}
	_, ok, err = d.store.Get(ctx, key, &mv)
	if err != nil {
		return ModuleVersion{}, false, err
	}
	if !ok {
		return ModuleVersion{}, false, nil
	}
	return mv, true, nil
}

// ListVersions returns all stored version strings for a module.
func (d *DB) ListVersions(ctx context.Context, modulePath string) (versions []string, err error) {
	prefix, err := buildPrefix(modulePath)
	if err != nil {
		return nil, err
	}
	records, err := d.store.GetPrefix(ctx, prefix, 0, -1)
	if err != nil {
		return nil, err
	}
	mvs, err := kv.ValuesOf[ModuleVersion](records)
	if err != nil {
		return nil, err
	}
	versions = make([]string, len(mvs))
	for i, mv := range mvs {
		versions[i] = mv.Info.Version
	}
	return versions, nil
}

// GetLatestVersion returns the module version with the most recent Info.Time.
func (d *DB) GetLatestVersion(ctx context.Context, modulePath string) (mv ModuleVersion, ok bool, err error) {
	prefix, err := buildPrefix(modulePath)
	if err != nil {
		return ModuleVersion{}, false, err
	}
	records, err := d.store.GetPrefix(ctx, prefix, 0, -1)
	if err != nil {
		return ModuleVersion{}, false, err
	}
	if len(records) == 0 {
		return ModuleVersion{}, false, nil
	}
	mvs, err := kv.ValuesOf[ModuleVersion](records)
	if err != nil {
		return ModuleVersion{}, false, err
	}

	var latest ModuleVersion
	var latestTime time.Time
	for _, mv := range mvs {
		if mv.Info.Time.After(latestTime) {
			latest = mv
			latestTime = mv.Info.Time
		}
	}
	return latest, true, nil
}
