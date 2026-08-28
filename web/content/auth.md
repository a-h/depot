# Authentication

By default, depot does not require authentication. You can restrict access using SSH public keys, which are used to issue short-lived JWT tokens.

## Auth file format

Create a text file with one SSH public key per line:

```text
<permission> <key-type> <base64-key> <comment>
```

Where `<permission>` is `r` for read-only or `w` for read-write:

```text
w ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... admin@laptop
r ssh-rsa    AAAAB3NzaC1yc2EAAAADAQABAAABA... readonly@desktop
w ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... deploy@ci
```

You can source public keys directly from GitHub:

```bash
curl https://github.com/<username>.keys >> auth.keys
```

## Authentication behavior

- If **any** key has read-only (`r`) permission, **all** access requires authentication.
- If **only** write (`w`) keys are configured, only uploads require authentication.
- If no auth file is provided, no authentication is required.

## Starting the server with authentication

```bash
depot serve --auth-file auth.keys --private-key signing.key
```

## Pushing with authentication

Use `depot proxy` to start a local authenticated proxy. The proxy signs requests with your SSH key so you can push to a depot server that requires authentication, without passing credentials to your package manager directly.

```bash
# Start an authenticated proxy on the default port (43407).
depot proxy {{.BaseURL}}

# Start on a random port.
depot proxy {{.BaseURL}} --port 0
```

Point your package manager at the proxy address instead of the depot server:

```bash
nix copy --to http://localhost:43407 nixpkgs#hello
depot go push http://localhost:43407
depot python push http://localhost:43407
depot npm push http://localhost:43407
```
