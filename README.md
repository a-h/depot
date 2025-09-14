# github.com/a-h/depot

A Nix binary cache, written in Go.

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

```bash
nix copy --to http://localhost:8080 `nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw` --refresh
```

### store-list

```bash
nix store ls --store http://localhost:8080 --recursive /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
```
