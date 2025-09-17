# github.com/a-h/depot

A Nix binary cache, written in Go, with upload support and SSH key authentication.

## Features

- **Read access**: Serve NAR files and narinfo metadata from your local Nix store
- **Upload support**: Accept uploads of compressed NAR files (.nar, .nar.xz, .nar.gz, .nar.bz2) and narinfo metadata via HTTP PUT
- **SSH Authentication**: JWT-based authentication using SSH public keys with read/write permissions
- **Proxy & Push**: Built-in proxy and push commands for authenticated access to remote caches
- **Compression support**: Automatic decompression of uploaded NAR files
- **NAR processing**: Uses go-nix library for proper NAR file parsing and extraction
- **Logging**: Structured JSON logging with configurable verbosity

## Quick Start

### 1. Generate a signing key (for narinfo signatures)

```bash
nix key generate-secret --key-name mycache-1 > signing.key
```

### 2. Start the server (no authentication)

```bash
depot serve --private-key signing.key --verbose
```

### 3. Start the server with SSH authentication

Create an auth file with SSH public keys:

```bash
# auth.keys - format: permission ssh-keytype base64key comment
w ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABA... user@laptop
r ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABA... user@desktop
```

Then start the server:

```bash
depot serve --auth-file auth.keys --private-key signing.key --verbose
```

### 4. Push to a remote cache

```bash
# Push a flake reference
depot push https://my-cache.example.com github:NixOS/nixpkgs#sl

# Push store paths
depot push https://my-cache.example.com /nix/store/abc123...

# Push from stdin
echo "/nix/store/abc123..." | depot push https://my-cache.example.com --stdin
```

## Authentication

The server supports SSH key-based authentication using JWT tokens. Authentication is configured via a text file containing SSH public keys with permission levels.

### Auth File Format

Each line in the auth file has the format:

```text
<permission> <ssh-keytype> <base64-key> <comment>
```

Where:

- `permission`: `r` for read-only, `w` for read-write
- `ssh-keytype`: SSH key type (e.g., `ssh-rsa`, `ssh-ed25519`)
- `base64-key`: The base64-encoded public key
- `comment`: Optional comment

Example auth file:

```text
w ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC... admin@laptop
r ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQD... readonly@desktop
w ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... deploy@ci
```

### Authentication Behavior

- If **any** key has read-only (`r`) permission, **all** access requires authentication
- If **only** write (`w`) keys are configured, only uploads require authentication
- If **no** auth file is provided, no authentication is required

### Using the Proxy

The `depot proxy` command creates an authenticated proxy to a remote cache:

```bash
# Start proxy on default port (43407)
depot proxy https://my-cache.example.com

# Start proxy on random port
depot proxy https://my-cache.example.com --port 0

# Then use nix commands through the proxy
nix copy --to http://localhost:43407 nixpkgs#sl
```

### Supported Upload Formats

The server supports uploading NAR files in multiple compression formats:

- `.nar` - Uncompressed NAR files
- `.nar.xz` - XZ-compressed NAR files (most common with Nix)
- `.nar.gz` - Gzip-compressed NAR files  
- `.nar.bz2` - Bzip2-compressed NAR files

The server automatically detects the compression format from the URL and decompresses the content before processing.

## Tasks

### build

```bash
go build -o depot ./cmd/depot
```

### run

```bash
go run ./cmd/depot
```

### serve

Interactive: true

```bash
go run ./cmd/depot serve --verbose
```

### nix-build

```bash
nix build
```

### nix-run

```bash
nix run
```

### nix-develop

```bash
nix develop
```

### docker-build

Interactive: true

```bash
nix build .#docker-image
```

### docker-load

Interactive: true

Once you've built the image, you can load it into a local Docker daemon with `docker load`.

```bash
docker rmi ghcr.io/a-h/depot:latest || true
docker load < result
```

### docker-run

Interactive: true

```bash
mkdir -p ${HOME}/depot-nix-store
docker run --rm -v ${HOME}/depot-nix-store:/depot-nix-store -p 8080:8080 ghcr.io/a-h/depot:latest
```

### push-store-path

Push a store path to the binary cache. If authentication is enabled, use the `proxy` command to proxy with authentication.

```bash
nix copy --to http://localhost:8080 `nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw` --refresh
```

### store-list

```bash
nix store ls --store http://localhost:8080 --recursive /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
```

### test

interactive: true

Run all tests including integration tests:

```bash
go test ./... -v -coverprofile=coverage.out -timeout 5m
```

### test-coverage-summary

interactive: true

Show coverage summary:

```bash
go tool cover -func=coverage.out | grep total
```

### sqlite-find

Warning: this may damage the running db.

Interactive: true

```bash
sqlite3 -header -column "file:$HOME/depot-nix-store/var/nix/db/db.sqlite?ro=1" "SELECT hashPart, namePart FROM NARs WHERE namePart LIKE '%source%';"
```

### push-without-tools

```bash
export FLAKE=github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526
export   PKG=github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl

# Copy flake input and source to the store.
nix flake archive --to http://localhost:8080 $FLAKE --refresh

# Copy the sl package of the flake, and it derivation.
nix copy --to 'http://localhost:8080' $PKG --refresh
nix copy --derivation --to 'http://localhost:8080' $PKG --refresh

# Copy realised paths of inputs to the derivation so that we can build it on the remote.
nix copy --to http://localhost:8080 $(
  nix-store --realise $(
    nix derivation show "$PKG" | jq -r '.[].inputDrvs | keys[]'
  )
)
```