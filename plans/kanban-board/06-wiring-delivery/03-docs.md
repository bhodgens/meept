# 06-wiring-delivery/03-docs

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
Parent: 06-wiring-delivery
Dependencies: all (master.md)

## Scope
Generate and verify all required documentation for the Kanban board feature, ensuring parity across user-facing and agent-facing surfaces.

## Estimated Context
40K

## Tasks
- [ ] Author `docs/workflows/kanban.md`: Feature specification covering projector, rebuild, and human command workflows.
- [ ] Author `internal/kanban/AGENTS.md`: Package invariant documentation (store-is-projection rule, lease rule, sidecar no-logic rule, opt-in defaults).
- [ ] Update root `AGENTS.md`: Add `internal/kanban` to the package table and include an invariant summary.
- [ ] Add a section to `docs/workflows/kanban.md` providing a Caddy/nginx proxy example for sidecar TLS termination.

## Self-Verification Checklist
- [ ] Docs match implementation (no silent drift).
- [ ] All `AGENTS.md` files follow the hierarchical pattern.
- [ ] `make docs-check` passes.

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] `make graphs` re-run after any topic/RPC/WS change
- [ ] `e2e-manifest.json` updated for new suites
- [ ] `AGENTS.md` package table updated
