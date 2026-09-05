# leaf 01 — MCP Catalog Entries (obscura + excel fallback)

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task in this document
using TDD where applicable. **Do NOT commit. Do NOT run `git add`.** Write
changes, run verification, report results. The orchestrator handles all
git operations.

- Parent: `docs/plans/20260905-research-audit-tools/master.md`
- Scope: `config/mcp_servers.json5` ONLY. No Go code changes.
- Dependencies: none.
- Estimated context: ~25K.

## Goal

Add two MCP server catalog entries to `config/mcp_servers.json5` per
master.md Contract D: (1) obscura browser MCP, enabled; (2) excel
fallback MCP, disabled.

## Tasks

### Task 1: obscura entry (enabled)

Edit `config/mcp_servers.json5`. Insert into the `servers` array, in the
npx/stdio section near the `playwright` entry:

```json5
    // obscura: local-build headless browser engine (Rust, Apache-2.0) with
    // built-in anti-detect, ~30 browser_* tools over stdio MCP. Binary is
    // the 2026-09-05 local release build at an absolute path (not on the
    // daemon's PATH). Supports geo-vantaged reads via OBSCURA_GEO_LOCATION
    // ("lat,lon") + OBSCURA_TIMEZONE + OBSCURA_PROXY env passthrough.
    // upstream: https://github.com/h4ckf0r0day/obscura
    {
      "name": "obscura",
      "enabled": true,
      "category": "browser",
      "description": "obscura headless browser MCP: navigate/snapshot/markdown/evaluate/forms/cookies; anti-detect; geo+tz+proxy shim",
      "type": "stdio",
      "command": ["/Users/caimlas/git/obscura/target/release/obscura", "mcp"],
    },
```

Match the exact field naming/format of neighboring entries (trailing
commas are valid JSON5).

### Task 2: excel fallback entry (disabled)

Insert after Task 1's entry:

```json5
    // excel: higher-capability xlsx manipulation fallback (pivot tables,
    // charts, conditional formatting) via uvx. DISABLED by default: a
    // subprocess write bypasses the pending_changes fence. Enable only
    // when the native spreadsheet_write tool's CSV/XLSX surface is not
    // enough. upstream: https://github.com/haris-musa/excel-mcp-server
    {
      "name": "excel",
      "enabled": false,
      "category": "data",
      "description": "excel-mcp-server fallback: pivot tables, charts, formatting for xlsx when native spreadsheet_write is insufficient",
      "type": "stdio",
      "command": ["uvx", "excel-mcp-server", "stdio"],
    },
```

### Task 3: verification

1. Parse check: `python3 -c "import json5,sys; json5.load(open('config/mcp_servers.json5'))"`
   (meept repo root; json5 is available via pip if missing — verify with
   the daemon instead if pip install is not an option:
   `go run ./cmd/meept config get mcp` or launch daemon briefly).
2. Confirm the daemon's catalog loader accepts both entries: build and
   run any existing MCP-catalog unit test, e.g.
   `go test -p 2 ./internal/tools/mcp/ -run Catalog -count=1`
   (discover the actual test name with `search_files` first; if no
   catalog test exists, run the package tests wholesale).
3. Binary sanity: `/Users/caimlas/git/obscura/target/release/obscura --version`
   prints `obscura 0.1.0-dev+...`. Do NOT rebuild obscura; do NOT download
   anything.

## Self-Verification Checklist

- [ ] Both entries present, JSON5-parse clean, field names match neighbors
- [ ] obscura `enabled: true`, excel `enabled: false`
- [ ] obscura command is the absolute build path (Contract D verbatim)
- [ ] Comments state local-build provenance, geo/proxy env, and the
      fence-bypass caveat on excel
- [ ] Existing catalog entries untouched (diff shows insertions only)
- [ ] Catalog-loading test (or package tests) green

## Review Checklist (for orchestrator)

- [ ] Diff touches only config/mcp_servers.json5
- [ ] Contract D fields verbatim (names, enabled flags, command arrays)
- [ ] No secrets, no env values inlined
- [ ] Catalog tests green

Do NOT commit.
