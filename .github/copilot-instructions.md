This project is a Nix binary cache server and HTTP Nix Store.

When writing code, please ensure that:

- Logging is on a single line, not spread over multiple lines.
- Comments end with a period.
- Minimise comment use in code that you write.
- HTTP error messages do not expose internal details and are a static string.
- Use `defer r.Body.Close()` to ensure request bodies are closed.
- Run `xc -s` to list available tasks.
- Run tests `xc test` to verify correctness.
- Follow the Go line-of-sight principle.