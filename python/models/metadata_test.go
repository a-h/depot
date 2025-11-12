package models

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"testing"
)

//go:embed agate.json
var agateMetadataJSON []byte

func TestSimpleAgateMetadata(t *testing.T) {
	var spi SimplePackageIndex
	err := json.Unmarshal(agateMetadataJSON, &spi)
	if err != nil {
		t.Fatalf("failed to unmarshal agate metadata: %v", err)
	}
	if spi.Name != "agate" {
		t.Errorf("expected package name 'agate', got '%s'", spi.Name)
	}
	if len(spi.Files) == 0 {
		t.Errorf("expected at least one file entry, got 0")
	}
	if len(spi.Versions) == 0 {
		t.Errorf("expected at least one version, got 0")
	}
	for _, file := range spi.Files {
		if file.Filename == "" {
			t.Errorf("file entry has empty filename")
		}
		if file.URL == "" {
			t.Errorf("file entry has empty URL")
		}
		if file.Size <= 0 {
			t.Errorf("file entry has non-positive size")
		}
		if len(file.Hashes) == 0 {
			t.Errorf("file entry has no hashes")
		}
		if file.PackageName() != "agate" {
			t.Errorf("expected package name 'agate' from filename, got '%s'", file.PackageName())
		}
		v := file.Version()
		if v == "" {
			t.Errorf("file entry has empty version")
		}
		if !versionRegexp.MatchString(v) {
			t.Errorf("version '%s' does not match expected format", v)
		}
	}
}

var versionRegexp = regexp.MustCompile(`\d+\.\d+.\d+`)

func TestVersion(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"agate-0.5.2.tar.gz", "0.5.2"},
		{"package-1.0.0.zip", "1.0.0"},
		{"lib-v2.3.4.whl", "v2.3.4"},
		{"tool-10.20.30.tar.bz2", "10.20.30"},
		{"invalid_package.tar.gz", ""},
		{"3d_maps-1.0.0.tar.gz", "1.0.0"},
		{"requests-0.2.0.tar.gz", "0.2.0"},
	}

	for _, test := range tests {
		fileEntry := SimpleFileEntry{Filename: test.filename}
		version := fileEntry.Version()
		if version != test.expected {
			t.Errorf("for filename '%s', expected version '%s', got '%s'", test.filename, test.expected, version)
		}
	}
}
