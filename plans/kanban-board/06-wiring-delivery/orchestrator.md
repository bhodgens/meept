# 06-wiring-delivery

## Goal
Complete the delivery of Kanban features via MCP tools, e2e testing, and user documentation.

## Interface Contracts (inherited: master.md C1-C10 + branch-local only)
- C1: Go types
- C2: Bus topics
- C4: RPC methods
- C5: Grants (`board.read`, `board.mutate`, `board.takeover`)

## Child Index

| Child | Type | Deps | Est ctx | Files |
|---|---|---|---|---|
| 01-mcp-tool-provider | Leaf | all | 60K | internal/tools/builtin/kanban_register.go |
| 02-e2e-suite | Leaf | all | 85K | e2e/suites/kanban/ files + repo-root e2e/manifest.json entry |
| 03-docs | Leaf | all | 40K | docs/workflows/kanban.md, internal/kanban/AGENTS.md |

## Dispatch Protocol
1. Do NOT commit. Do NOT run git add.
2. Dispatch each leaf with:
   - Leaf path
   - Pinned contracts (C1-C10)
   - Coding Conventions
   - Verification commands (`go test -p 2 ./...`)
3. Upon async completion:
   - Run `git status --short` and `git diff --stat HEAD --`
   - Review in-session by main model
   - Commit per leaf: `feat(kanban): <leaf-name>`
4. Update the Completion Tracking Table.

## Coding Conventions
- Go stdlib + existing deps only.
- Error wrapping with `%w`.
- Table-driven tests.
- Documentation must follow `docs/workflows/` pattern.

## Completion Tracking Table

| Task | Status | Notes |
|---|---|---|
| 01-mcp-tool-provider | DONE | dbe8cb00 (kanban_* tool provider) |
| 02-e2e-suite | DONE | 365c21ea (kanban live-wiring suite, cases (a)-(g)) |
| 03-docs | DONE | f9c0bc95 (package invariant docs + as-built workflows) |

> Reconciliation note (2026-11-16, issue #156): rows above were under-reporting merged work; statuses updated from git (each SHA verified an ancestor of main). Tables were last refreshed d492867c (2026-11-14).

## Integration Test Plan
(See master.md Integration Test Plan for 06-wiring-delivery specifics)

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] `make graphs` re-run after any topic/RPC/WS change
- [ ] `e2e-manifest.json` updated for new suites
- [ ] `AGENTS.md` package table updated
