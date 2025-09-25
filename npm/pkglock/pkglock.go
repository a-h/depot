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

// Parse reads an npm package-lock.json (v2/v3) and returns a sorted
// list of unique "name@version" strings for registry packages.
func Parse(ctx context.Context, r io.Reader) (pkgs []string, err error) {
	var lockFile NPMLock
	if err = json.NewDecoder(r).Decode(&lockFile); err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}

	unique := make(map[string]struct{})

	for installPath, pkg := range lockFile.Packages {
		if installPath == "" {
			continue
		}

		// Skip packages that don't come from the npm registry (local, git, etc.).
		if pkg.Resolved == "" ||
			strings.HasPrefix(pkg.Resolved, "file:") ||
			strings.HasPrefix(pkg.Resolved, "git+") {
			continue
		}

		// Use the true published name if present.
		name := pkg.Name
		if name == "" {
			name = stripNodeModulesPath(installPath)
		}

		if name == "" || pkg.Version == "" {
			continue
		}

		unique[fmt.Sprintf("%s@%s", name, pkg.Version)] = struct{}{}
	}

	pkgs = slices.Collect(maps.Keys(unique))
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
