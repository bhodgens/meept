---
name: convention-no-external-deps
description: "prefer stdlib solutions and avoid adding new external dependencies"
scope: session
---

Do not introduce new external dependencies. Before suggesting any package that is not already in go.mod:

1. Check if the Go standard library can accomplish the same goal.
2. Check if an existing dependency already provides the needed functionality.
3. If neither is true, explicitly call out that a new dependency is needed and explain why there is no stdlib or existing-dep alternative.

This applies to:
- HTTP routing, middleware, and server utilities
- JSON/YAML/TOML parsing
- File system operations
- Testing utilities
- CLI argument parsing
- Logging
- Time formatting and parsing
- String manipulation and regex
- Cryptographic operations
- Concurrency primitives

Common stdlib alternatives to prefer:
- `net/http` instead of chi, gin, echo, fiber
- `encoding/json` instead of json-iterator, easyjson
- `log/slog` instead of zap, zerolog, logrus
- `html/template` instead of mustache, pongo2
- `testing` + `net/http/httptest` instead of testify (unless already present)
- `os/exec` instead of go-commandbus or similar
- `sync` primitives instead of concurrent maps from external libs
