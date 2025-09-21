# NPM Registry

The purpose of this registry is to provide an offline cache of public NPM packages.

The specification of how NPM registries works is https://github.com/npm/registry/blob/main/docs/responses/package-metadata.md

## Downloading packages

For depot, the CLI can be used to fetch packages from the NPM registry and store them in this repository. The CLI tool takes a list of package names and, optionally, a version. If no version is specified, the latest version is fetched. If a version is specified, that specific version is fetched. Multiple versions can be selected, line by line.

```
express
lodash@4.17.21
@types/node
```

The CLI tool can be used as follows:

```
depot npm save <dir: default ./depot/npm> < packages.txt
```

The tool sorts the inputs, removes duplicates, then downloads package metadata and tarball to the specified directory in a directory structure that can be served as a static file server.

Rather than add it as a dependency, take a copy of the download tool at https://github.com/a-h/flakegap/blob/main/export/download/download.go which handles concurrent downloads and hash verification.

At the root of the `--save` directory, a list of all packages and versions is stored in `index.txt`. Any packages that are already present in the index.txt file, or the `--save` directory, are skipped (it's likely that the user will trim the contents of the --save directory between runs, after copying the contents to the depot server).

## Fetching package metadata from the NPM registry

NPM allows you to receive metadata about all versions of a package in a single request. We only care about being able to install packages, so we can use the abbreviated metadata format.

By passing the `Accept:application/vnd.npm.install-v1+json` header, the response content is the https://github.com/npm/registry/blob/main/docs/responses/package-metadata.md#abbreviated-metadata-format

Otherwise, the full metadata object https://github.com/npm/registry/blob/main/docs/responses/package-metadata.md#full-metadata-format is returned, which can be very large (>10MB) for packages with many versions, authors, and dependencies.

```bash
curl https://registry.npmjs.org/accepts -H "Accept:application/vnd.npm.install-v1+json"
```

```json
{
	"_id": "accepts",
	"_rev": "1-967a00dff5e02add41819138abb3284d",
	"name": "accepts",
	"description": "Higher-level content negotiation",
	"dist-tags": {
		"latest": "1.3.8"
	},
	"versions": {
		"1.3.8": {
		  // Content from https://registry.npmjs.org/accepts/1.3.8
		},
	},
}
```

The metadata object contains a number of fields that reference other packages, such as `dependencies`, `devDependencies`, `peerDependencies`, and `optionalDependencies`. These fields are objects where the keys are package names and the values are version ranges:

```json
  "dependencies": {
    "mime-types": "~2.1.34",
    "negotiator": "0.6.3"
  },
```

The CLI tool recursively fetches the metadata for these packages and versions, ensuring that all dependencies are also cached in the registry. Where a version range is specified, the latest version matching that range is fetched.

The CLI tool keeps track of which packages and versions have already been fetched, to avoid redundant network requests.

## Fetching package tarballs

The package version metadata contains a `dist` field with a `tarball` URL. This URL points to the location of the package tarball, which is a `.tgz` file containing the package contents.

```
  "dist": {
	"integrity": "sha512-...",
	"shasum": "abc123...",
	"tarball": "https://registry.npmjs.org/accepts/-/accepts-1.3.8.tgz"
  },
```

The CLI tool fetches the tarball from this URL and stored on the filesystem. If the tarball is not from `https://registry.npmjs.org`, the tool warns the user, but continues.

The domain is stripped from the URL, and the remainder of the path used to determine where to store the tarball on disk, e.g. `npm/accepts/-/accepts-1.3.8.tgz` for the above example.

### Pushing

Pushing to depot is simply a HTTP PUT request to the location where the metadata or tarball should be stored, e.g. `PUT /npm/express/-/express-4.17.1.tgz` for the tarball, or `PUT /npm/express/4.17.1` for the metadata.

When a new package version is pushed, the top level metadata for the package is updated to include the new version in the `versions` field, the latest version in the `dist-tags` field is updated if the new version is greater than the current latest version, and the location of the tarball is updated in the `dist` field to match the domain name of the depot server.

Depot simply serves the files as static files, so a HTTP GET request to the same URL will return the file.

Depot uses JWT authentication middleware, which can be passed using the `DEPOT_TOKEN` or `--token` argument.

```bash
DEPOT_TOKEN=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9... depot npm push --token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9... <dir: default ./depot/npm>
```

Before pushing a file, the tool checks if the file already exists on the server using a HTTP HEAD request. If the file does not exist (the server responds with a `404 Not Found` status code), the tool uploads the file using a HTTP PUT request.

If a partial upload was made, the server doesn't save the file, so the tool can safely retry the upload.

The CLI tool looks like:

```bash
depot npm push <dir: default ./depot/npm> <depot-url: default http://localhost:8080> [--token token]
```

During pushing the index.txt file is updated to include all packages and versions that have been pushed. This action is protected by a mutex to prevent concurrent uploads from corrupting the index.txt file. The index.txt file is incrementally flushed to disk after each package is pushed, so if the process is interrupted, it can be resumed without re-uploading packages that have already been pushed.

## DB Design

key: /npm/@{scope}/{package} 
value: package metadata in JSON format, excluding versions

key: /npm/@{scope}/{package}/{version}
value: package version metadata

To get a list of all versions, execute a GetPrefix query on the package and you can pull all versions.

To get a single version, Get just the required key.