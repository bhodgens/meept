# 05 E2E suite, GUI parity, docs - Implementation Leaf

> **DISPATCH INSTRUCTION:** Any agent may implement this document end-to-end.

## Meta

- **Parent:** ../master.md
- **Scope:** Hermetic e2e suite proving the full goal loop; TUI goals view; GUI parity; docs + graphs.
- **Dependencies:** 01, 02, 03, 04 all REVIEWED.
- **Estimated context:** ~70K.
- **Contracts:** all of SHARED-CONVENTIONS §2 as the behavior under test.

## Goal

Per the repo e2e policy, new feature tests live in the hermetic e2e tier. This leaf adds one suite that exercises the whole capability and closes the UI/docs/graph obligations the other leaves open.

## Tasks

### Task 1: e2e suite goal-completion

- Dirs: e2e/suites/goal-completion/ (build tag `e2e`).
- Scenarios (harness pattern: see e2e/harness/daemon.go; build via the standard scratch-bin flow):
  1. declare → fulfill: daemon with an employee whose definition carries `goals:[{title, mandate, gate:{command:"true"}}]` at tier 3 → trigger → assert plan row has goal_id, gate ran, goal health updated.
  2. create via RPC: agents.goals.create on a running daemon → ListGoals shows it.
  3. implicit anchoring: chat turn that creates a plan on a session with no explicit goal → EnsureImplicitGoal row appears; plan row references it.
  4. steer: tier-1 employee mid-episode receives agents.steer → next reflect records Source "steer"; steer of an idle employee is a clean no-op.
- Update e2e/manifest.json: path→suite mapping for internal/employee, internal/plan (goal_id), internal/task (goal_id).
- Verify: `make e2e-fast-area AREA=goal-completion` green.

### Task 2: TUI goals view

- File: internal/tui — add a goals panel/section to the agent detail model (mirror how audit findings display, per docs/workflows/employees.md:392). Table sized through internal/tui/tableutil.Size + SetRows (TUI table invariant). All labels lowercase.
- Parity note in the commit-visible doc line: GUI (Flutter) gets the same via leaf Task 3.
- Verify: `go build ./...`; existing TUI tests still pass.

### Task 3: Flutter GUI parity

- File: ui/flutter_ui/lib — goals list + health badge on the agent detail page, reading /api/v1/agents/{id}/goals. Web-safe (no dart:io top-level imports; PlatformService pattern).
- Verify: `flutter analyze` if SDK available; otherwise note skipped in report (orchestrator decides whether fmt-check-gui gate applies).

### Task 4: Docs + graphs + AGENTS.md

- docs/workflows/employees.md: mark `goals:` definition block live; add goal-create, bind_session, steer to the CLI/RPC/HTTP tables (lines ~349-412 region).
- `make graphs` — new RPC (agents.goals.create, agents.goals.bind_session, agents.steer) + HTTP routes change the topology; commit docs/generated/* regen.
- Root AGENTS.md: employees command list gains goal-create/steer.
- Verify: `make graphs-check`.

## Interface Contract (exposed to siblings)

- None (terminal leaf). Exposes the suite name `goal-completion` to master's integration review.

## Self-Verification Checklist

- [ ] `make e2e-fast-area AREA=goal-completion` green
- [ ] `make e2e-fast` still green (manifest addition did not break the sweep)
- [ ] `make graphs-check` passes
- [ ] TUI table built via tableutil.Size/SetRows; all text lowercase
- [ ] Flutter code compiles for web (kIsWeb guards; no top-level dart:io)
- [ ] No commits; no read_file corruption
