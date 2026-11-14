# 06-wiring-delivery/02-e2e-suite

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
Parent: 06-wiring-delivery
Dependencies: all (master.md)

## Scope
Implement and verify the hermetic e2e test suite for the Kanban board, covering all critical integration scenarios.

## Estimated Context
85K

## Interface Contract
(See master.md Integration Test Plan - Cases (a)-(g))
- (a) Rebuild equality
- (b) Lease refusal (ErrLeaseHeld)
- (c) Takeover audit sequence
- (d) WIP counter accuracy (block refuses / warn moves+notes)
- (e) Sidecar auth/bind failure
- (f) Parity snapshot (GUI)
- (g) Live-daemon wiring (real boot answers kanban.board.list)

## Tasks
- [ ] Add the kanban suite entry (path->suite mapping) to the repo-root `e2e/manifest.json`, same commit as the new suite (pre-commit gate [18]). Follow the existing suite entry shape: `{"name": "kanban", "dir": "e2e/suites/kanban", "status": "implemented"}` plus a `path_map` entry covering `internal/kanban|internal/kanbansvc|cmd/meept-kanban|internal/daemon/kanban_wiring.go|internal/rpc/kanban.go`.
- [ ] Implement `e2e/suites/kanban/kanban_test.go`: Implement test logic for cases (a)-(g). Match the existing suite file convention (`<suite>_test.go` + `helpers_test.go`, see `e2e/suites/async-turn/`).
- [ ] All suite files carry the `e2e` build tag.
- [ ] Run `make e2e-fast` to verify all cases pass in the hermetic environment.

Case (g) — live-daemon wiring gate: boot the real daemon via the e2e
harness with `[kanban].enabled=true` and assert `kanban.board.list`
answers over the Unix socket. A suite that hand-replicates the daemon's
kanban construction in the test binary proves nothing; the real boot
must fire the registration (07/01).

## Self-Verification Checklist
- [ ] No `os.Getwd()` used.
- [ ] No new `internal/` files (e2e logic only in `e2e/`).
- [ ] Build tag `e2e` applied to all new test files.
- [ ] `e2e/manifest.json` updated for the new suite.

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] `make graphs` re-run after topic/RPC/WS change
- [ ] `e2e/manifest.json` updated for new suites
- [ ] `AGENTS.md` package table updated
