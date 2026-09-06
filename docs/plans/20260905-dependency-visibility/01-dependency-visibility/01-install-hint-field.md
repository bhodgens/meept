# leaf 01-dependency-visibility/01 — InstallHint field + catalog annotations

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: docs/plans/20260905-dependency-visibility/01-dependency-visibility/orchestrator.md
- Scope STRICTLY: internal/tools/mcp/manager.go (one field), config/mcp_servers.json5 (annotations), internal/config/catalog_test.go (one new test), internal/tools/mcp/manager_test.go (field test). No other files.
- Dependencies: none.
- Estimated context: ~35K.

## Contract A (verbatim from parent master)

```go
// internal/tools/mcp/manager.go — add to ServerConfig:
    // InstallHint is the shell command that installs the server's
    // dependency when it is missing from PATH (e.g. "npm install -g
    // @modelcontextprotocol/server-github", "brew install uv",
    // "cargo install --path <obscura-checkout>/crates/obscura"). Empty
    // means the entry has no external binary dependency (http transport)
    // or no known installer. Display-only: meept NEVER executes it
    // without explicit user consent (see doctor --install-missing).
    InstallHint string `json:"install_hint,omitempty"`
```

## Tasks

### Task 1: TDD the field

manager_test.go: parse a ServerConfig with install_hint set / absent;
assert field round-trips and omitempty keeps absent entries clean.

### Task 2: annotate the catalog

config/mcp_servers.json5 — add `"install_hint": "..."` to EVERY stdio
entry (22 entries). Truthful hints:

- npx entries: `npm install -g <the-package-in-command>` (e.g.
  server-github → `npm install -g @modelcontextprotocol/server-github`)
- uvx entries: `brew install uv  (or: pip install uv)` — the runtime is
  the dependency; uvx fetches each package itself on first run
- cua-driver: `brew install cua-driver  (or: install per
  github.com/trycua/cua)` — verify from the repo's own docs
  (docs/workflows/external-integrations.md cua-driver section) what the
  documented install is and use THAT
- obscura: `cargo install --path <obscura-checkout>/crates/obscura  (or:
  copy target/release/obscura into ~/.local/bin)` — match the existing
  comment in the entry
- http-transport entries (if any): NO install_hint

Field placement: after "description", before "type", matching the
entry's existing style.

### Task 3: loader test

catalog_test.go: new `TestCatalogInstallHints` — every entry with
`type: "stdio"` MUST have a non-empty install_hint; http entries must
NOT. Loop the parsed catalog; fail naming offending entries. This is the
regression fence for future catalog additions.

### Task 4: verify

```
go build ./internal/... 
go vet ./internal/tools/mcp/ ./internal/config/
go test -p 2 ./internal/tools/mcp/ ./internal/config/ -run 'Catalog|ServerConfig|Install' -count=1 -timeout 120s
```

## Self-Verification Checklist

- [ ] Field + comment match Contract A byte-for-byte
- [ ] 22/22 stdio entries annotated; http entries untouched
- [ ] TestCatalogInstallHints green and would catch a missing hint
- [ ] No other catalog fields touched; diff is additions only
- [ ] gofmt/vet clean; no TODOs

## Review Checklist (for orchestrator)

- [ ] Hints truthful per entry type (spot-check 3: one npx, one uvx, obscura)
- [ ] Contract A verbatim; json tag install_hint
- [ ] Loader test counts match (stdio==hints)

Do NOT commit.
