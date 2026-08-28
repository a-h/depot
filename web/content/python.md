# Python (PyPI)

Depot implements the [PEP 503 Simple Repository API](https://peps.python.org/pep-0503/), making it a drop-in replacement for PyPI with any `pip`-compatible tool.

## Saving packages

Use the `depot` CLI to download packages from PyPI and store them locally:

```bash
# Save specific packages.
depot python save "requests>=2.0.0" "flask==2.3.0"

# Save from a requirements.txt file.
depot python save --stdin < requirements.txt

# Pipe a list of packages.
echo "requests>=2.0.0" | depot python save --stdin
```

Packages are stored in `.depot-storage/python` in the current directory. Standard version specifiers are supported: `==`, `>=`, `<=`, `~=`, and ranges such as `>=1.0.0,<2.0.0`.

## Pushing to depot

```bash
depot python push {{.BaseURL}}
```

## Configuring pip

### One-off install

```bash
pip install --index-url {{.BaseURL}}/python/ requests
```

### pip.conf (persistent)

```ini
# ~/.pip/pip.conf  or  /etc/pip.conf
[global]
index-url = {{.BaseURL}}/python/
```

### requirements.txt header

```text
--index-url {{.BaseURL}}/python/
requests>=2.0.0
flask==2.3.0
```

### Additional index

To use depot alongside PyPI rather than instead of it:

```bash
pip install --extra-index-url {{.BaseURL}}/python/ requests
```

## Endpoint

```
{{.BaseURL}}/python/
```
