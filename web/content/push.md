# Pushing Packages

The `depot` CLI can save packages from public registries to local storage and push them to a depot server. This two-step workflow lets you prepare packages offline and upload them in bulk.

## Go modules

```bash
# Step 1: save modules locally (fetches from proxy.golang.org).
depot go save golang.org/x/text@v0.21.0 rsc.io/quote@v1.5.2

# Save all dependencies declared in a go.mod file.
depot go save ./go.mod

# Step 2: push to depot.
depot go push {{.BaseURL}}
```

## Python packages

```bash
# Step 1: save packages locally (fetches from PyPI).
depot python save "requests>=2.0.0" "flask==2.3.0"

# Save from a requirements.txt file.
depot python save --stdin < requirements.txt

# Step 2: push to depot.
depot python push {{.BaseURL}}
```

## NPM packages

```bash
# Step 1: save packages locally.
depot npm save express lodash

# Step 2: push to depot.
depot npm push {{.BaseURL}}
```

## Nix store paths

```bash
# Push by flake reference (builds and pushes the closure).
depot nix push {{.BaseURL}} --flake-refs nixpkgs#hello nixpkgs#jq

# Push specific store paths.
depot nix push {{.BaseURL}} --store-paths /nix/store/abc123...

# Push from stdin.
nix-store --realise /nix/store/abc123.drv | depot nix push {{.BaseURL}} --stdin
```

You can also push Nix store paths using `nix copy` without the `depot` CLI:

```bash
nix copy --to {{.BaseURL}}/nix nixpkgs#hello
```

## Pushing from a zip or archive

If you have a `.depot-storage` directory from a previous `depot save` run, compress it and push by extracting on the depot host or by using the web upload interface.

```bash
# Create an archive of the local storage.
tar -czf depot-storage.tar.gz .depot-storage/

# Extract on the depot host and push.
tar -xzf depot-storage.tar.gz
depot go push {{.BaseURL}}
depot python push {{.BaseURL}}
depot npm push {{.BaseURL}}
```

## Authenticated push

When the depot server requires authentication, start the proxy first and push through it:

```bash
depot proxy {{.BaseURL}} &
PROXY=http://localhost:43407

depot go push $PROXY
depot python push $PROXY
depot npm push $PROXY
depot nix push $PROXY --flake-refs nixpkgs#hello
```
