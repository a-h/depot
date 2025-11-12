package models

import (
	"encoding/json"
	"strings"
)

// SimplePackageIndex represents the Simple API package index.
type SimplePackageIndex struct {
	Meta     SimpleMeta        `json:"meta"`
	Name     string            `json:"name"`
	Files    []SimpleFileEntry `json:"files"`
	Versions []string          `json:"versions,omitempty"`
}

// SimpleMeta contains metadata for the Simple API.
type SimpleMeta struct {
	APIVersion string `json:"api-version"`
}

// SimpleFileEntry represents a file in the Simple API.
type SimpleFileEntry struct {
	Filename             string            `json:"filename"`
	URL                  string            `json:"url"`
	Hashes               map[string]string `json:"hashes"`
	RequiresPython       string            `json:"requires-python,omitempty"`
	Size                 int64             `json:"size,omitempty"`
	CoreMetadata         json.RawMessage   `json:"core-metadata,omitempty"`
	DataDistInfoMetadata json.RawMessage   `json:"data-dist-info-metadata,omitempty"`
	Yanked               json.RawMessage   `json:"yanked,omitempty"`
}

func (sf SimpleFileEntry) PackageName() string {
	parts := strings.SplitN(sf.Filename, "-", 2)
	if len(parts) < 2 {
		return sf.Filename
	}
	return parts[0]
}

var binaryExtensions = []string{".bz2", ".gz", ".tar", ".whl", ".zip"}

func (sf SimpleFileEntry) Version() string {
	fn := sf.Filename
	for _, ext := range binaryExtensions {
		fn = strings.TrimSuffix(fn, ext)
	}
	parts := strings.SplitN(fn, "-", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
