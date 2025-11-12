# Python Package Storage

This package implements Python package storage for the depot server, supporting the PyPI Simple Repository API (PEP 503/691).

## API Endpoints

### Simple API

- `GET /python/simple/` - List all packages (HTML or JSON)
- `GET /python/simple/{package}/` - Get package files (HTML or JSON)

HTML is returned by default. JSON is returned if the request includes `Accept: application/vnd.pypi.simple.v1+json`

- `PUT /python/simple/{package}/{filename}` - Upload package file.
- `PUT /python/simple/{package}/{filename}.json` - Upload package file metadata.

## Command Line Interface

### Save Packages

Save packages from PyPI to local storage:

```bash
# Save specific packages
depot python save "package1==1.0.0" "package2~=2.0"

# Save packages from stdin
echo "package1==1.0.0" | depot python save --stdin

# Use custom storage directory
depot python save --dir ./my-storage package1==1.0.0
```

### Push Packages

Push packages from local storage to a remote depot:

```bash
# Push all packages
depot python push https://depot.example.com

# Push with authentication
depot python push --token $TOKEN https://depot.example.com

# Push from custom directory
depot python push --dir ./my-storage https://depot.example.com
```

## Configuration

The Python package storage can be configured via environment variables:

- `DEPOT_PYTHON_DIR` - Directory for local package storage (default: `.depot-storage/python`)
- `DEPOT_AUTH_TOKEN` - JWT authentication token for push operations

## Usage with pip

Configure pip to use your depot server as a package index:

```bash
# Install from your depot
pip install --index-url http://localhost:8080/python/simple/ package-name

# Add as extra index URL
pip install --extra-index-url http://localhost:8080/python/simple/ package-name
```

Or configure in `pip.conf`:

```ini
[global]
index-url = http://localhost:8080/python/simple/
```

## Database Schema

Package metadata is stored with the following key structure:

```
/python/{normalized-package-name}/{version}
```

Package names are normalized according to PEP 503:
- Converted to lowercase
- Hyphens, underscores, and dots are treated as equivalent

## File Storage Structure

Package files are stored in the following structure:

```
files/{package-name}/{filename}
```

For example:
- `files/requests/requests-2.28.0-py3-none-any.whl`
- `files/requests/requests-2.28.0.tar.gz`

