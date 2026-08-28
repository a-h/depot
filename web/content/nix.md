# Nix Binary Cache

Depot implements the Nix binary cache protocol, so Nix can use it as a substituter to fetch pre-built store paths instead of building from source.

## Pushing packages

Use `depot nix push` to push store paths to your depot server:

```bash
# Push by flake reference.
depot nix push {{.BaseURL}} --flake-refs nixpkgs#hello nixpkgs#jq

# Push specific store paths.
depot nix push {{.BaseURL}} --store-paths /nix/store/abc123...

# Push from stdin.
echo "/nix/store/abc123..." | depot nix push {{.BaseURL}} --stdin
```

You can also push using `nix copy` directly:

```bash
nix copy --to {{.BaseURL}}/nix nixpkgs#hello
```

## Configuring Nix

### Fetch the public signing key

Before configuring Nix to trust your depot, retrieve the public signing key:

```bash
curl {{.BaseURL}}/nix/nix-cache-info
```

The response contains the `StoreDir` and `WantMassQuery` fields. The signing key name is set when starting the server with `--private-key`. Convert the private key to a public key if you do not already have it:

```bash
nix key convert-secret-to-public < signing.key
# Output: depot-1:AAA...
```

### nix.conf

```ini
# ~/.config/nix/nix.conf  or  /etc/nix/nix.conf
substituters = {{.BaseURL}}/nix https://cache.nixos.org
trusted-public-keys = <key-name>:<public-key-base64>= cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=
```

List your depot first so Nix checks it before the public cache.

### Per-command flags

```bash
nix build \
  --option substituters "{{.BaseURL}}/nix https://cache.nixos.org" \
  --option trusted-public-keys "<key-name>:<public-key-base64>= cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=" \
  nixpkgs#hello
```

## Endpoints

```
{{.BaseURL}}/nix                 substituter root
{{.BaseURL}}/nix/nix-cache-info  cache info and public key name
```
