# Coding style

This project is a package storage and cache server for Nix, NPM, and Python.

## Markdown

- Headings do not end with punctuation.
- Ensure that markdown linting passes.

## General

- End comments with a full stop (period).
- Use path escaping for constructed URLs: fmt.Sprintf("/flows/%s/delete", url.PathEscape(flow.ID)).
- Check that new and updated files are not corrupted.
- Minimise comment use in written code.
- Keep logging on a single line.
- Follow the Go line-of-sight principle.

## Go style

- Write test names as positive assertions of behaviour, e.g. "functions can return multiple values".
- Use subtests, and table-driven tests where appropriate.
- Use `any` instead of `interface{}`.
- Prefer `for range 10` and `b.Loop()` over `for i := 0; i < 10; i++`.
- Don't write 1-3 line helper functions that only get used once, they're not required.
- Return static HTTP error messages that do not expose internal details.
- Use `defer r.Body.Close()` to ensure request bodies are closed.

## Running tasks and tests

- Run `xc -s` to list available tasks.
- Run tests `xc test` to verify correctness.
