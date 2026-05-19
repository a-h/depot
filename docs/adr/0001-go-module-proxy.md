# ADR 0001: Go Module Proxy Support

## Status

Accepted

## Context

Depot is a multi-package repository server supporting Nix, NPM, and Python packages. Users in air-gapped environments need to mirror Go modules. The workflow is: save modules to disk on a connected machine, transfer the disk to the air-gapped network, push modules to a depot server, then configure the Go toolchain to use depot as its module proxy via `GOPROXY`.

The Go module proxy protocol is well-defined (see `go help goproxy`) and consists of five GET endpoints per module. The protocol is intentionally simple enough to serve from a static file system.

## Decision

Add Go module proxy support to depot, following the same save/push/serve pattern used by NPM and Python packages.

### Upstream source

Fetch modules from `proxy.golang.org` (the public Go module proxy) rather than from version control systems directly. This provides pre-built module archives in the standard format the Go toolchain expects.

### Module path encoding

Use `golang.org/x/mod/module` for module path escaping (`EscapePath`/`UnescapePath`) and version escaping (`EscapeVersion`/`UnescapeVersion`). This is the canonical implementation used by the Go toolchain itself, handling the `!lowercase` encoding for capital letters in module paths.

### Storage layout

Mirror the proxy protocol URL structure in storage keys:

```
{escaped-module}/@v/{escaped-version}.info
{escaped-module}/@v/{escaped-version}.mod
{escaped-module}/@v/{escaped-version}.zip
```

This means the GET handler path maps directly to storage keys after stripping the `/go/` prefix, simplifying the implementation.

### Database records

Store module metadata (version info and go.mod content) in the KV store under `/go/{escaped-module-path}/{escaped-version}`. The zip files are stored only in the storage backend (filesystem or S3), not in the database.

### Dependency resolution

When saving modules, recursively resolve transitive dependencies by parsing each downloaded module's `go.mod` file. Respect `replace` directives (substituting the replacement module path and version) and skip local path replacements with a warning.

## Alternatives considered

### Shell out to `go mod download`

Requires the Go toolchain to be installed on the saving machine. Harder to control the storage location and format. The Go toolchain's module cache layout differs from the proxy protocol layout.

### Fetch directly from VCS (git clone)

Requires VCS tools (git, hg, svn). Complex to implement correctly for all hosting providers. The `proxy.golang.org` service already solves this by providing pre-built module archives.

### Use Athens or another existing Go proxy

Athens is a full-featured Go module proxy server. However, it adds an external dependency, does not integrate with depot's authentication, storage backends, or metrics, and includes features (on-demand upstream fetching) not needed for the air-gapped use case.

### Use the `goproxy/goproxy` library

This library implements the full GOPROXY protocol as an `http.Handler` with configurable `Fetcher` and `Cacher` interfaces. However, it is designed as a caching proxy that fetches from upstream on demand. Adapting it to depot's save/push/serve model would require implementing custom interfaces to bridge to depot's `kv.Store` and `storage.Storage` backends, adding complexity without clear benefit. The GOPROXY protocol is simple enough (five GET endpoints) that a direct implementation provides tighter integration with depot's existing patterns.

## Consequences

- Users can mirror Go modules for air-gapped environments using the same depot workflow they use for NPM and Python.
- The Go toolchain is configured to use depot via `GOPROXY=http://depot-server:8080/go`.
- Module path encoding follows the canonical Go implementation, ensuring compatibility with all valid module paths.
