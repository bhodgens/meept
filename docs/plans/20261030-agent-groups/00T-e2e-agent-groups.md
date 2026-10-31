# 00T: E2E Test Gate for Agent Groups - Test-Only Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Write TESTS ONLY. No implementation. Every test must fail RED for a
> missing-implementation reason (undefined symbol, missing config section,
> absent RPC method) - never for a syntax error. Do NOT commit. Do NOT use
> read_file on existing source files - explore with search_files or terminal
> cat. After writing a file, do NOT read it back - write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Hermetic e2e suites covering every master Test Gate row (F1-F10)
  that is implementable without the implementation, plus manifest entries.
- **Dependencies:** none
- **Estimated Context:** 70K
- **Concurrency Group:** T

## Goal

The gate proves isolation before implementation: a grouped agent cannot call
denied tools, cannot mutate membership, cannot recall above its ceiling, and
unsigned cluster dispatches are rejected - all asserted through live RPC/tool
surfaces from a scratch daemon, failing RED today.

## Context

- e2e rules (root AGENTS.md): suites in `e2e/suites/<name>/`, `e2` build tag,
  manifest + path_map in `e2e/manifest.json` SAME COMMIT, scratch MEEPT_HOME
  via the shared harness; assert through live surfaces (RPC/CLI), never store
  internals.
- RED discipline: each test names what is missing. Suggested RED anchors:
  `groups` config section (internal/config/schema.go), `internal/groups`
  package (Resolve), `groups.explain` RPC, `meept groups` CLI binary subcommand,
  FenceStage symbol in pkg/security, MemoryMaxScope intersection symbol.
- Features gated on OTHER trees (F5 memory ceiling needs issue #70 branch 01;
  F9 cluster needs the existing cluster suite rig): write these as SKIPPED
  with a dated skip reason naming the dependency, not as fake-passing tests.

## Tasks

| Task ID | Objective | Files | RED reason | Given/When/Then |
|---------|-----------|-------|------------|-----------------|
| T1 | Suite `agent-groups`: F1 deny-beats-allow (grouped agent: denied tool absent from GetDefinitions AND Get()=nil), F2 ungrouped backward compat, F3 assignment validation reject (RPC groups.assign with unregistered tool name => error naming it), F4 mutation gate (non-privileged agent => RPC deny + file_write deny on config paths), F6 fence stage (write outside fence root via a NON-opt-in tool => denied), F7 decision log line present in captured log with rule_source=group_deny | e2e/suites/agent-groups/*.go | symbols/config absent | Given policy {allow: file_read, deny: shell}, When grouped agent runs; Then shell uncallable, file_read allowed, log line has rule_source=group_deny |
| T2 | Suite `groups-diagnosis`: F8 `meept groups explain` names binding+policy per grant; `meept groups check` reports dangling name/cycle/unknown-group/empty-allowlist; F10 override expiry (env-injected clock: expired override skipped + Warn) | e2e/suites/groups-diagnosis/*.go | CLI subcommand absent | Given the C3 fixture config; When explain coder; Then output lists shell via binding policy coders; check exits non-zero on injected defects |
| T3 | Suite `cluster-identity` member (or skip-tagged member of the existing cluster suite): F9 unsigned DispatchJob rejected with Warn, no execution | e2e/suites/<cluster member>/*.go | Signature verification symbol absent | Given require_node_signatures=true; When unsigned dispatch arrives; Then rejected |
| T4 | Manifest entries for both suites in e2e/manifest.json path_map | e2e/manifest.json | suites unlisted | Given the new suite dirs; When manifest loads; Then both map |

## Self-Verification Checklist

- [ ] Every test RED for missing-implementation (run each; record reason)
- [ ] Skips only where this tree depends on another tree (dated reason)
- [ ] Manifest entries land in the same commit (orchestrator commits)
- [ ] gofmt clean; no line-number corruption; no debug artifacts
- [ ] Do NOT weaken or delete any existing suite

**DO NOT COMMIT.** The orchestrator validates against the master Test Gate
matrix (delta protocol), commits, and sets TESTS_VALIDATED.

## Completion Report Format

1. Files touched: [paths]
2. RED evidence: [per-test failure reason, last 5 lines each]
3. Skips with dependency + date: [list]

## Review Checklist (For Review Agent)

- [ ] Matrix F1-F10 covered by a test or a dated skip
- [ ] RED reasons are missing-implementation, never compile-slop
- [ ] Scratch-home harness pattern used; live-surface assertions only
