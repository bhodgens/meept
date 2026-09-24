# E2E Testing Expansion — Master Plan (2026-09-23)

## Goal

Meept has 500+ unit test files and essentially one e2e harness (the live-model
naive-user chat regression). This plan audits where e2e testing pays off,
builds the infrastructure to make e2e tests cheap to write and run, adds
~100 e2e scenarios grouped so a commit only runs the suites its paths touch,
and makes e2e mandatory for features while freezing new unit tests.

## Definitions

- **e2e test (this plan)**: a Go test (build tag `e2e`) in `e2e/suites/<area>/`
  that boots a REAL scratch daemon (fresh-built binaries, sandboxed MEEPT_HOME,
  free ports — the proven `e2e-naive-user-chat.sh` isolation model) and drives
  it through its real surfaces (RPC, HTTP, WS, CLI, sqlite stores). The LLM is
  a FAKE OpenAI-compatible server so suites are hermetic, fast (seconds), and
  CI-safe. Live-model e2e stays as-is (pre-push/nightly tier).
- **Suite**: one area directory (e.g. `e2e/suites/rpc-chat/`). The
  `e2e/manifest.json` maps repo path prefixes -> suites.
- **No new unit tests**: policy change. New tests land in the e2e tier. Existing
  unit tests are untouched.

## Deliverables

1. **Harness** (`e2e/harness/`): scratch-daemon lifecycle (build once per run,
   sandboxed home, port allocation, fake LLM server with scripted responses,
   CLI/RPC/HTTP drivers, turn-result polling helpers).
2. **Inventory**: ~100 e2e-worthy flows across subsystems, each with paths and
   a target suite. Recorded in `e2e/manifest.json` + `e2e/SCENARIOS.md`.
3. **Suites**: `e2e/suites/<area>/` covering the inventory. Run via
   `make e2e-fast` (all) or `make e2e-fast AREA=rpc-chat`.
4. **Path-scoped execution**: `scripts/e2e-affected.sh` maps staged/changed
   paths to affected suites and runs only those.
5. **Git hooks**:
   - `.githooks/pre-commit-e2e`: runs affected suites for staged Go/Flutter
     files; FAILS if a new package/feature lands with no manifest coverage and
     no e2e files staged.
   - `.githooks/pre-commit` chain: wired as check 18.
   - Commit-msg-level rule is enforced in pre-commit by staged-content
     detection (pre-commit cannot see the message).
6. **Repo rule** (docs + AGENTS.md): every new feature must ship an e2e test;
   unit tests are frozen (bug-fix updates to existing unit tests allowed; no
   new `_test.go` unit files for new features).
7. **CI wiring**: code-quality.yml gains an `e2e-fast` job (hermetic).

## Constraints

- macOS: suites must respect `-p 2` port-exhaustion rule; the harness caps
  concurrent daemons.
- Never touch `~/.meept`; sandbox everything under t.TempDir().
- Fake LLM must answer classifier, planner, and general slots or the daemon
  boot gate (`classifier_boot_fail_fast`) kills the scratch daemon.
- Shared-tree discipline: each subagent owns a DISJOINT file set; nobody
  commits; the orchestrator verifies and commits per concern with explicit
  paths. Sibling-session files (strategic.go, config schema/daemon.go edits,
  plan_critique*.go) are OFF-LIMITS.

## Execution phases

- **P0 Harness** (1 agent): `e2e/harness/` + fake LLM + `make e2e-fast` +
  one exemplar suite (`e2e/suites/smoke/`: boot, chat turn, tool executes,
  artifact created). Gate: exemplar passes locally in <60s.
- **P1 Inventory** (4 parallel read-only agents): enumerate e2e-worthy flows
  (numbered, path-mapped) across (a) RPC+HTTP+WS transport, (b) agent
  loop/dispatcher/planner/tasks, (c) tools + MCP + skills, (d)
  session/memory/employee/audit/effects/backup/CLI. Target: >=100 items.
- **P2 Suites** (parallel waves, 3-4 agents per wave): implement suites from
  the manifest, disjoint areas, using only the P0 harness API.
- **P3 Enforcement**: manifest script + hooks + Makefile + CI + AGENTS.md +
  docs/workflows/e2e-testing.md. Verify hook liveness with a staged-file dry
  run.
- **P4 Final gates**: `make test`, `make e2e-fast`, `go vet`, gofmt on touched
  files, `make graphs-check` if any bus/RPC surface comments changed (they do
  not — read-only on daemon code).

## Verification of completion

- `bash scripts/e2e-affected.sh` demo: touch one file per area, see only that
  area's suites run.
- Hook dry-run with a staged feature file and no e2e file -> hook fails; with
  an e2e file staged -> passes.
- Manifest scenario count >= 100 with implemented/deferred status per item.
- `make test` green (untouched), `make e2e-fast` green.

## Status

- [x] P0 harness + exemplar
- [x] P1 inventory (104 scenarios, e2e/SCENARIOS.md)
- [x] P2 suites (15 areas implemented; status per area in e2e/SCENARIOS.md)
- [x] P3 enforcement (hooks, manifest, docs, AGENTS.md)
- [x] P4 final gates
