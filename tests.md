# Testing overview

We must be able to prove the following on the store.

## Tests

### It is possible to upload a package from the public cache

This command copies the `sl` command to the store. The `nix eval` turns the flake reference into a store path.

```bash
nix copy --to http://localhost:8080 `nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw`
```

After uploading, we should be able to access relevant narinfo files and nar files from the HTTP endpoint.

### It is possible to copy the derivation of a public package

```bash
nix copy --derivation --to http://localhost:8080 `nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw`
```

I don't know what happens with .drv files, but I expect to be able to collect them from the cache or store.

It might be that we need to test pulling from the Nix store using the CLI, and see what HTTP requests it makes, e.g. `nix copy --derivation --to ~/temp-nix-store/ $(nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw)` so that we can make good tests for it, or we may be able to test that the .drv files we expect are copied to ~/temp-nix-store by the process (preferred, since it tests Nix CLI interop).

### Input derivations can be copied to the store and retrieved

It is critical that after pushing to depot (this system), a system that is not connected to the Internet, but is pre-configured to use depot can run `nix run github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl` and see the output of `sl`.

That means that it must be able to build '/nix/store/w9ayfbzn5piwi5j500iy21zzr6v0ifn8-config.guess-948ae97.drv' and '/nix/store/awxkpvmsmxlfdx7q12853ysngrqy907n-source.drv', since those are part of the process.

One of those derivations references the store path `/nix/store/pyyddwilxjwq3n7065zd6xpk8r01hqjm-source`, which is the store path of the source code of `sl`. It must be in the target store, or nix will try to download it inside the airgapped builder, which won't work.

I think that we need to copy realised paths of inputs to the derivation so that we can build the path to `sl` on the target machine:

```bash
export paths=$(nix derivation show github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl | jq -r '.[].inputDrvs | keys[]')
export realised_paths=$(nix-store --realise $paths)
nix copy --to http://localhost:8080 $realised_paths
```

After doing this (or previous operations), I expect `/nix/store/pyyddwilxjwq3n7065zd6xpk8r01hqjm-source` to be in the remote store / cache. 

### The source code of a Nix flake can be pushed to the remote

```bash
nix flake archive --to http://localhost:8080 github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526
```

The source code of the Nix flake should end up in the store.

## Testing process

This project uses Go integration tests with the Nix CLI.

The tests are located in `integration/integration_test.go` and can be run with:

```bash
go test ./integration -v
```

For test coverage:

```bash
go test -cover ./integration -v
```

The integration tests start an in-process depot server and use the real Nix CLI to test operations like:

- Uploading packages from public cache
- Copying derivations
- Archiving flakes
- Round-trip copy operations

Each test validates both server functionality and proper narinfo parsing using the go-nix library.

Then, run integration unit tests etc.

```bash
go test -cover ./... -coverpkg ./... -args -test.gocoverdir="$PWD/coverage"
```

Generate a text coverage profile for tooling to use.

```bash
go tool covdata textfmt -i=./coverage -o coverage.out
```

Print total coverage.

```bash
go tool cover -func coverage.out | grep total
```
