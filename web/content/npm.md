# NPM

Depot implements the NPM registry protocol, so `npm`, `yarn`, and `pnpm` can use it as a drop-in package registry.

## Saving packages

Use the `depot` CLI to download packages from the public NPM registry and store them locally:

```bash
# Save specific packages.
depot npm save express lodash

# Save all packages listed in a package.json file.
depot npm save ./package.json
```

Packages are stored in `.depot-storage/npm` in the current directory.

## Pushing to depot

```bash
depot npm push {{.BaseURL}}
```

## Configuring npm

### One-off install

```bash
npm install --registry {{.BaseURL}}/npm express
```

### .npmrc (persistent)

```ini
# ~/.npmrc  or  <project>/.npmrc
registry={{.BaseURL}}/npm
```

### yarn

```bash
yarn config set registry {{.BaseURL}}/npm
```

### pnpm

```bash
pnpm config set registry {{.BaseURL}}/npm
```

## Endpoint

```
{{.BaseURL}}/npm
```
