# 06-wiring-delivery/01-mcp-tool-provider

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
Parent: 06-wiring-delivery
Dependencies: all (master.md)

## Scope
Register Kanban-specific tools within the MCP provider system so they can be discovered and used by external agents.

## Estimated Context
60K

## Interface Contract
Exposes:
- `kanban_list`
- `kanban_show`
- `kanban_create`
- `kanban_move`
- `kanban_takeover`
- `kanban_assign`
- `kanban_comment`

Note: `kanban_claim` is EXCLUDED per master.md decision (stays on internal queue path).

## Tasks
- [ ] Implement `internal/tools/builtin/kanban_register.go`: Register the list of `kanban_*` tools with the `toolprov`.
- [ ] Implement `internal/tools/builtin/kanban_register.go`: Ensure tools are only visible when `[kanban].enabled` is true.
- [ ] Implement `internal/tools/builtin/kanban_register.go`: Implement the `Discover` logic to return the correct tool schemas.
- [ ] Write `internal/tools/builtin/kanban_register_test.go`: Test tool discovery and visibility based on configuration.
- [ ] Verify: `go test -v ./internal/tools/builtin/...`

## Self-Verification Checklist
- [ ] No `os.Getwd()` used.
- [ ] All errors wrapped with `%w`.
- [ ] No I/O inside any mutex.
- [ ] `kanban_register_test.go` uses `$TMPDIR` for SQLite.

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] `make graphs` re-run after topic/RPC/WS change
- [ ] `e2e-manifest.json` updated for new suites
- [ ] `AGENTS.md` package table updated
