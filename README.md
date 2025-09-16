# github.com/a-h/depot

A Nix binary cache, written in Go, with upload support.

## Features

- **Read access**: Serve NAR files and narinfo metadata from your local Nix store
- **Upload support**: Accept uploads of compressed NAR files (.nar, .nar.xz, .nar.gz, .nar.bz2) and narinfo metadata via HTTP PUT
- **Authentication**: Optional token-based authentication for uploads
- **Compression support**: Automatic decompression of uploaded NAR files
- **NAR processing**: Uses go-nix library for proper NAR file parsing and extraction
- **Logging**: Structured JSON logging with configurable verbosity

## Configuration

### Upload Authentication

To secure uploads, set the `DEPOT_UPLOAD_TOKEN` environment variable or use the `--upload-token` flag:

```bash
export DEPOT_UPLOAD_TOKEN="your-secret-token"
go run ./cmd/depot serve --verbose
```

When a token is configured, all upload operations (PUT requests) require an `Authorization` header:

```bash
# Using Bearer token format
curl -H "Authorization: Bearer your-secret-token" -X PUT --data-binary @file.nar http://localhost:8080/abc123.nar

# Or simple token format
curl -H "Authorization: your-secret-token" -X PUT --data-binary @file.nar http://localhost:8080/abc123.nar
```

If no token is configured, uploads are allowed without authentication (not recommended for production).

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

```bash
nix build .#docker-image
```

### docker-load

Once you've built the image, you can load it into a local Docker daemon with `docker load`.

```bash
docker load < result
```

### docker-run

```bash
docker run -p 8080:8080 app:latest
```

### push-store-path

Push a store path to the binary cache. If authentication is enabled, you'll need to configure the token:

```bash
# Without authentication
nix copy --to http://localhost:8080 `nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw` --refresh

# With authentication (set token in environment)
export DEPOT_UPLOAD_TOKEN="your-secret-token"
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
