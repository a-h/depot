# github.com/a-h/depot

Storage for Nix, NPM, and Python packages.

## Features

- **Read access**: Serve NAR files and narinfo metadata from your local Nix store
- **Upload support**: Accept uploads of compressed NAR files (.nar, .nar.xz, .nar.gz, .nar.bz2) and narinfo metadata via HTTP PUT
- **SSH Authentication**: JWT-based authentication using SSH public keys with read/write permissions
- **Proxy & Push**: Built-in proxy and push commands for authenticated access to remote caches
- **Compression support**: Automatic decompression of uploaded NAR files
- **NAR processing**: Uses go-nix library for proper NAR file parsing and extraction
- **Logging**: Structured JSON logging with configurable verbosity
- **Storage backends**: Filesystem or S3-compatible storage (AWS S3, MinIO, GCP Cloud Storage)

## Nix usage

### 1. Generate a signing key (for narinfo signatures)

```bash
nix key generate-secret --key-name local-depot-1 > signing.key
```

The `--key-name` can be any identifier you choose (e.g., `mycompany-cache`, `home-cache-1`). The `-1` suffix is a convention for key versioning. Use the same key name consistently when configuring clients to trust your cache.

### 2. Start the server (no authentication)

```bash
depot serve --private-key signing.key --verbose
```

### 3. Configure Nix to trust the cache

Extract the public key from your signing key, or fetch the public key from the Nix cache info handler (`http://localhost:8080/nix/nix-cache-info`).

```bash
# Convert the private key to public key
nix key convert-secret-to-public < signing.key
# Output: local-depot-1:public-key-base64...
```

Add the public key to your Nix configuration to trust signatures from your cache. Replace `local-depot-1:public-key-base64...` with the actual output from the command above:

```bash
# Option 1: Add to ~/.config/nix/nix.conf
echo "trusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY= local-depot-1:public-key-base64..." >> ~/.config/nix/nix.conf

# Option 2: Add to /etc/nix/nix.conf (system-wide, requires sudo)
sudo sh -c 'echo "trusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY= local-depot-1:public-key-base64..." >> /etc/nix/nix.conf'

# Option 3: Use flags when running nix commands
nix build --option trusted-public-keys "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY= local-depot-1:public-key-base64..." --option substituters "http://localhost:8080 https://cache.nixos.org" nixpkgs#sl
```

Add your depot as a substituter:

```bash
# Add to nix.conf (depot first, then official cache as fallback)
echo "substituters = http://localhost:8080 https://cache.nixos.org" >> ~/.config/nix/nix.conf

# Or use the --option flag
nix build --option substituters "http://localhost:8080 https://cache.nixos.org" nixpkgs#sl
```

Note: The order matters - Nix checks substituters sequentially. List your depot first for faster lookups, with the official cache as a fallback.

### 4. Start the server with SSH authentication

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

### 5. Push to a remote cache

```bash
# Push a flake reference
depot push https://my-cache.example.com --flake-refs github:NixOS/nixpkgs#sl

# Push store paths
depot push https://my-cache.example.com --store-paths /nix/store/abc123...

# Push from stdin
echo "/nix/store/abc123..." | depot push https://my-cache.example.com --stdin
```

Or push using `nix copy` - see `push-without-tools` for complete push examples.

```bash
nix copy --to https://my-cache.example.com nixpkgs#sl
```

## NPM usage

### 1. Download packages from NPM

```bash
depot npm save express
```

This will create a `.depot-storage` in the current directory containing the NPM package tarballs and metadata.

### 2. Push the NPM packages to depot

```bash
depot npm push http://localhost:8080
```

### 3. Use the packages

```bash
npm install --registry http://localhost:8080 express
```

## Python usage

### 1. Download packages from PyPI

```bash
# Download specific packages.
depot python save "requests>=2.0.0" "flask==2.3.0"

# Download from a requirements.txt file.
depot python save --stdin < requirements.txt

# Or pipe a list of packages.
echo "requests>=2.0.0" | depot python save --stdin
```

This will create a `.depot-storage/python` directory containing the Python package files and metadata.

The save command supports standard Python package specifiers:

- `package==1.0.0` - Exact version
- `package>=1.0.0` - Minimum version
- `package>=1.0.0,<2.0.0` - Version range
- `package~=1.4.2` - Compatible release

### 2. Push the Python packages to depot

```bash
depot python push http://localhost:8080
```

### 3. Use the packages with pip

```bash
# Install from your depot.
pip install --index-url http://localhost:8080/python/simple/ requests

# Use as an additional package index.
pip install --extra-index-url http://localhost:8080/python/simple/ requests

# In requirements.txt, use:
# --index-url http://localhost:8080/python/simple/
# requests>=2.0.0
```

Or configure pip permanently in `pip.conf` or `~/.pip/pip.conf`:

```ini
[global]
index-url = http://localhost:8080/python/simple/
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

## S3 Storage Configuration

Start server with S3 storage backend:

```bash
# Using AWS S3 with IAM role (EC2/ECS/Fargate).
depot serve \
  --storage-type=s3 \
  --s3-bucket=my-depot-bucket \
  --s3-region=us-east-1 \
  --database-url=postgres://user:pass@host/db

# Using MinIO with explicit credentials.
depot serve \
  --storage-type=s3 \
  --s3-bucket=depot \
  --s3-region=us-east-1 \
  --s3-endpoint=http://localhost:9000 \
  --s3-access-key-id=minioadmin \
  --s3-secret-access-key=minioadmin \
  --s3-force-path-style=true \
  --database-url=sqlite:///tmp/depot.db

# Environment variables can also be used.
export DEPOT_STORAGE_TYPE=s3
export DEPOT_S3_BUCKET=my-depot-bucket
export DEPOT_S3_REGION=us-east-1
depot serve
```

S3 configuration options:
- `--storage-type`: Set to `s3` for S3 storage
- `--s3-bucket`: S3 bucket name (required)
- `--s3-region`: AWS region (default: us-east-1)
- `--s3-endpoint`: Custom endpoint for MinIO/LocalStack
- `--s3-access-key-id`: Access key (uses IAM if not set)
- `--s3-secret-access-key`: Secret key (uses IAM if not set)
- `--s3-force-path-style`: Use path-style URLs (required for MinIO)

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

```bash
go run ./cmd/depot serve --store-path=$HOME/depot-store --verbose
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

```bash
nix build .#docker-image
```

### docker-load

Once you've built the image, you can load it into a local Docker daemon with `docker load`.

```bash
docker rmi ghcr.io/a-h/depot:latest || true
docker load < result
```

### microk8s-load

```bash
gunzip -c result | microk8s ctr image import -
```

### docker-run

```bash
mkdir -p ${HOME}/depot-store
docker run --rm -v ${HOME}/depot-store:/depot-store -p 8080:8080 ghcr.io/a-h/depot:latest
```

### push-store-path

Push a store path to the binary cache. If authentication is enabled, use the `proxy` command to proxy with authentication.

```bash
nix copy --to http://localhost:8080 `nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw` --refresh
```

### push-sample-packages

Push a couple of NPM packages, Python packages, and Nix packages.

```bash
export DEPOT_URL=http://localhost:8080

# Save and push NPM packages.
go run ./cmd/depot npm save express lodash
go run ./cmd/depot npm push $DEPOT_URL

# Save and push Python packages.
go run ./cmd/depot python save "requests>=2.0.0" "flask==2.3.0"
go run ./cmd/depot python push $DEPOT_URL

# Push Nix packages.
go run ./cmd/depot nix push $DEPOT_URL --flake-refs nixpkgs#hello
go run ./cmd/depot nix push $DEPOT_URL --flake-refs nixpkgs#jq
```

### download-sample-packages

Download a couple of NPM packages, Python packages, and Nix packages from depot.

```bash
export DEPOT_URL=http://localhost:8080
export DEPOT_NPM_URL=$DEPOT_URL/npm
export DEPOT_PYTHON_URL=$DEPOT_URL/python/simple/
export DEPOT_NIX_URL=$DEPOT_URL/nix
export DOWNLOAD_DIR=$(mktemp -d)

echo "Downloading artifacts to $DOWNLOAD_DIR"

# Download NPM packages from depot.
npm pack --registry $DEPOT_NPM_URL --pack-destination $DOWNLOAD_DIR express lodash

# Download Python packages from depot.
python -m pip download --dest $DOWNLOAD_DIR --index-url $DEPOT_PYTHON_URL requests flask

# Download Nix packages from depot.
nix build \
  --no-link \
  --option substituters "$DEPOT_NIX_URL https://cache.nixos.org" \
  nixpkgs#hello nixpkgs#jq
```

### store-list

```bash
nix store ls --store http://localhost:8080 --recursive /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
```

### test

Run all tests including integration tests:

```bash
go test ./... -v -coverprofile=coverage.out -timeout 5m
```

### test-s3-integration

```bash
go test ./storage -v -run TestS3Storage
```

### test-coverage-summary

Show coverage summary:

```bash
go tool cover -func=coverage.out | grep total
```

### sqlite-find

Warning: this may damage the running db.

```bash
sqlite3 -header -column "file:$HOME/depot-store/depot.db?ro=1" "SELECT key from kv;"
```

### push-without-tools

```bash
export FLAKE=github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526
export   PKG=github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl

# Copy flake input and source to the store.
nix flake archive --to http://localhost:8080/nix $FLAKE --refresh

# Copy the sl package of the flake, and it derivation.
nix copy --to 'http://localhost:8080/nix' $PKG --refresh
nix copy --derivation --to 'http://localhost:8080/nix' $PKG --refresh

# Copy realised paths of inputs to the derivation so that we can build it on the remote.
nix copy --to http://localhost:8080/nix $(
  nix-store --realise $(
    nix derivation show "$PKG" | jq -r '.[].inputDrvs | keys[]'
  )
)
```

## Notes

If you're hosting Depot on Docker or Kubernetes, you may encounter SQLite disk I/O errors due to filesystem restrictions. If you see an error like this:

```text
sqlite: step: disk I/O error
```

This is because SSHFS (Rancher Desktop's default), 9fs and other network filesystems do not support file locking, which SQLite requires. To resolve this, you can use a volume that supports file locking, such as a local disk or a network filesystem that supports it. Disabling WAL with the `&_journal_mode=DELETE` SQLite connection string can also resolve the issue.
