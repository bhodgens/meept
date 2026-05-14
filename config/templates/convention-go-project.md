---
name: convention-go-project
description: "enforce standard Go project conventions: testing, logging, error handling, and code organization"
scope: session
---

Follow these Go project conventions for all code you write or modify:

**Testing:**
- Write table-driven tests using `t.Run` for subcases.
- Test file naming: `*_test.go` in the same package (whitebox) unless testing requires blackbox access (then use `package_test`).
- Use `testify/assert` only if the project already depends on it; otherwise use stdlib `testing`.
- Cover error paths, not just happy paths.
- Use `t.Parallel()` where safe.

**Logging:**
- Use `log/slog` exclusively. No `log.Println` or `fmt.Println` for logging.
- Use structured logging: `slog.Info("message", "key", value, "key2", value2)`.
- Log at the appropriate level: Debug for internals, Info for state changes, Warn for degraded operation, Error for failures.

**Error handling:**
- Wrap errors with context: `fmt.Errorf("reading config: %w", err)`.
- Use `errors.Is()` and `errors.As()` for error checking, not string matching.
- Define sentinel errors with `var ErrSomething = errors.New("something")` in a `errors.go` file.
- Return errors up the call stack. Handle exactly once (log OR return, never both).

**Code organization:**
- Group by domain, not by type (no `models/`, `handlers/`, `services/` top-level dirs). Instead: `internal/user/`, `internal/order/`, etc.
- Keep `internal/` for packages not importable by external code.
- Use `cmd/` for entry points, one directory per binary.
- Place shared types in `internal/` sub-packages, not in a root `pkg/` directory.

**Naming:**
- Packages: lowercase, single word, no underscores. `user`, not `userManagement`.
- Exported names: use `CamelCase`. Avoid stutter (`user.User` is acceptable; `user.UserModel` is not).
- Interfaces: typically one method, named with `-er` suffix (`Reader`, `Stringer`, `TaskRunner`).
- Acronyms are consistently cased: `HTTPClient`, not `HttpClient`.
