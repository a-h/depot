package models

import (
	"encoding/json"
	"time"
)

// AbbreviatedPackage represents the abbreviated package metadata.
type AbbreviatedPackage struct {
	Name     string                        `json:"name"`
	Modified time.Time                     `json:"modified"`
	DistTags map[string]string             `json:"dist-tags"`
	Versions map[string]AbbreviatedVersion `json:"versions"`
}

// AbbreviatedVersion represents the abbreviated version metadata.
type AbbreviatedVersion struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Deprecated           json.RawMessage   `json:"deprecated,omitempty"`
	Dist                 *Dist             `json:"dist"`
	Dependencies         map[string]string `json:"dependencies,omitempty"`
	OptionalDependencies map[string]string `json:"optionalDependencies,omitempty"`
	DevDependencies      map[string]string `json:"devDependencies,omitempty"`
	BundledDependencies  []string          `json:"bundledDependencies,omitempty"`
	PeerDependencies     map[string]string `json:"peerDependencies,omitempty"`
	Bin                  json.RawMessage   `json:"bin,omitempty"`
	Directories          json.RawMessage   `json:"directories,omitempty"`
	Engines              json.RawMessage   `json:"engines,omitempty"`
	ID                   json.RawMessage   `json:"_id"`
	NodeVersion          json.RawMessage   `json:"_nodeVersion,omitempty"`
	NpmVersion           json.RawMessage   `json:"_npmVersion,omitempty"`
	NpmUser              *Person           `json:"_npmUser,omitempty"`
	HasShrinkwrap        bool              `json:"_hasShrinkwrap,omitempty"`
}

// Dist represents distribution information.
type Dist struct {
	Integrity    string          `json:"integrity,omitempty"`
	Shasum       string          `json:"shasum"`
	Tarball      string          `json:"tarball"`
	FileCount    int             `json:"fileCount,omitempty"`
	UnpackedSize int64           `json:"unpackedSize,omitempty"`
	Signatures   []DistSignature `json:"signatures,omitempty"`
}

// DistSignature represents a distribution signature.
type DistSignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

// Person represents a person (author, contributor, maintainer, etc.).
type Person struct {
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
}
