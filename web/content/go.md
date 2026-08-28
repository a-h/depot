# Go Modules

Depot implements the [Go module proxy protocol](https://go.dev/ref/mod#module-proxy), so any Go toolchain can use it as a drop-in replacement for `proxy.golang.org`.

## Saving modules

Use the `depot` CLI to fetch modules from the public proxy and store them locally:

```bash
# Save specific modules.
depot go save golang.org/x/text@v0.21.0 rsc.io/quote@v1.5.2

# Save all modules declared in a go.mod file and their transitive dependencies.
depot go save ./go.mod
```

Modules are stored in `.depot-storage/go` in the current directory.

## Pushing to depot

```bash
depot go push {{.BaseURL}}
```

## Configuring the Go toolchain

Set `GOPROXY` to point at your depot server. Append `,direct` to fall back to the public internet when a module is absent, or omit it for fully air-gapped environments.

### Shell environment

```bash
# With public fallback.
export GOPROXY={{.BaseURL}}/go,direct
export GONOSUMCHECK='*'
export GONOSUMDB='*'

# Air-gapped: no fallback.
export GOPROXY={{.BaseURL}}/go
```

`GONOSUMCHECK` and `GONOSUMDB` disable checksum database lookups, which are unreachable in air-gapped environments.

### Persistent configuration

```bash
go env -w GOPROXY={{.BaseURL}}/go,direct
go env -w GONOSUMCHECK='*'
go env -w GONOSUMDB='*'
```

## Endpoint

```
{{.BaseURL}}/go
```
