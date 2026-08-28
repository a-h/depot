# Depot

Depot is a self-hosted package registry for Go modules, Python packages, Nix binary caches, and NPM packages. It is designed for air-gapped environments and private networks where you need reliable, fast access to dependencies without relying on public registries.

## Supported package types

| Package type | Endpoint | Configuration guide |
|---|---|---|
| Go modules | `{{.BaseURL}}/go` | [Go Modules](/docs/go) |
| Python (PyPI) | `{{.BaseURL}}/python/` | [Python (PyPI)](/docs/python) |
| Nix binary cache | `{{.BaseURL}}/nix` | [Nix Binary Cache](/docs/nix) |
| NPM | `{{.BaseURL}}/npm` | [NPM](/docs/npm) |

## Authentication

By default the server requires no authentication. Read [Authentication](/docs/auth) to learn how to restrict access using SSH public keys.

## Pushing packages

Use the `depot` CLI to save packages from public registries and push them to your depot server. See [Pushing Packages](/docs/push) for step-by-step instructions.
