package pkglock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
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

func Parse(ctx context.Context, r io.Reader) (pkgs []string, err error) {
	var lockFile NPMLock
	err = json.NewDecoder(r).Decode(&lockFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}
	var uniquePkgs = make(map[string]struct{})
	for name, pkg := range lockFile.Packages {
		pkg.Name = stripNodeModulesPath(name)
		if pkg.Name == "" || pkg.Version == "" {
			continue
		}
		uniquePkgs[fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)] = struct{}{}
	}
	pkgs = slices.Collect(maps.Keys(uniquePkgs))
	slices.Sort(pkgs)
	return pkgs, nil
}

func stripNodeModulesPath(p string) string {
	idx := strings.LastIndex(p, "node_modules/")
	if idx == -1 {
		return p
	}
	return p[idx+len("node_modules/"):]
}
