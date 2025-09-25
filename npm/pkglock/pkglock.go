package pkglock

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type NPMLock struct {
	Name     string             `json:"name"`
	Version  string             `json:"version"`
	Packages map[string]Package `json:"packages"`
}

type Package struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Resolved     string            `json:"resolved"`
	Integrity    string            `json:"integrity"`
	Dependencies map[string]string `json:"dependencies"`
}

func Parse(ctx context.Context, lockFilePath string) (pkgs []string, err error) {
	f, err := os.Open(lockFilePath)
	if err != nil {
		return pkgs, fmt.Errorf("failed to open lock file %s: %w", lockFilePath, err)
	}
	var lockFile NPMLock
	err = json.NewDecoder(f).Decode(&lockFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}
	for _, pkg := range lockFile.Packages {
		pkgs = append(pkgs, fmt.Sprintf("%s@%s", pkg.Name, pkg.Version))
	}
	return pkgs, nil
}
