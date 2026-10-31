# 02: Groups Model (Policy/Binding/Resolve) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD against the branch/parent gate tests.
> Do NOT commit - the orchestrator handles all git operations after review.
> Do NOT use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** The `internal/groups` package: Policy, Binding, Resolved, Resolve
  (the precedence engine), config schema section, boot-time validation hook,
  cycle detection.
- **Dependencies:** 00T (TESTS_VALIDATED)
- **Estimated Context:** 65K
- **Concurrency Group:** B

## Goal

One pure-Go resolution function that answers "given this agent's spec tools,
groups, and the policy/binding tables, what is this agent allowed?" -
deterministic, table-tested, and shared by every consumer (tool gates, memory
ceiling, diagnosis). No I/O in Resolve; I/O lives at config load and CLI.

## Interface Contracts (From Parent)

C3 (verbatim shapes): Policy / Binding / Resolved structs; frozen precedence
(denies first, then allows; most-restrictive-wins for scopes/classes; ONE
level of group nesting; cycle = boot config error); config surface top-level
`groups` key with `default_managed` + `decision_log_level`.

C6 dependency direction: validation against the LIVE tool registry is wired by
leaf 04 - this leaf defines the hook signature and validates STRUCTURE only
(names non-empty, no cycles, referenced policies exist, binding subjects
well-formed `agent:|group:|user:`).

## Verified Anchors

- internal/config/schema.go - add `GroupsConfig` near MultiUserConfig
  (schema.go:117 neighborhood); DefaultConfig() entry REQUIRED (zero-value
  defaults pitfall); json+toml tags + `//gendoc:` comments.
- config/meept.json5 - template section with comments (drift annotations per
  scripts/config-drift-check.py conventions).
- Grant-name normalization precedent: AgentSpec GrantedToolNames
  (internal/agent/spec.go) - reuse the SAME normalization function shape
  (export a helper from internal/groups taking a normalizer func, so leaf 04
  injects the registry-backed one WITHOUT internal/groups importing
  internal/agent - import-cycle direction is fixed).
- Migration/pattern reference for a new config section:
  docs/plans/20261030-memory-second-brain/ (structure/style precedent only).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | internal/groups package: types + Resolve (pure fn: specTools []string, groups []string, policies, bindings, opts) with frozen precedence, one-level nesting, most-restrictive-wins merge | internal/groups/groups.go, resolve.go, resolve_test.go | `go test -p 2 ./internal/groups/` | package absent | matrix table test: deny-beats-allow; intersection never widens spec; empty effective allowlist => AllowedTools empty + Managed=true (D8 - caller decides fail-closed); scope/class narrowing |
| T2 | Cycle detection + structural validation (bad subject forms, missing policy refs) returning per-defect errors with names | internal/groups/validate.go, validate_test.go | `go test -p 2 ./internal/groups/ -run TestValidate` | absent | cyclic group => named error; dangling policy ref => named error |
| T3 | GroupsConfig in schema + DefaultConfig + template section + gendoc tags | internal/config/schema.go, config/meept.json5 | `go test -p 2 ./internal/config/ -run TestConfigLoads` | section absent | config loads; defaults: default_managed=false, decision_log_level="deny-warn" |
| T4 | Boot validation hook: daemon config load invokes groups.Validate (structure only this leaf) and fails boot on cycle/dangling-policy | internal/daemon/components.go (single insertion point), daemon test | `go test -p 2 ./internal/daemon/ -run TestGroupsBootValidate` | hook absent | bad groups config => boot error naming the defect |

## Self-Verification Checklist

- [ ] Resolve has NO I/O and NO clock dependency (overrides injected via opts)
- [ ] Precedence exactly C3; table test covers every cell of the matrix
- [ ] No import of internal/agent or internal/tools from internal/groups
- [ ] gofmt clean; no debug artifacts; gate tests GREEN and unmodified

**DO NOT COMMIT.** The orchestrator reviews and commits.

## Completion Report Format

1. Files touched: [paths]
2. Verify command + output: [last 20 lines per command]
3. Deviations: [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Precedence matches C3 verbatim (denies-first; nesting depth exactly 1)
- [ ] D8 shape: Managed flag present, no behavior change for ungrouped agents
- [ ] Boot hook is ONE insertion point, typed-nil safe, fails closed
- [ ] Config template + gendoc tags present; DefaultConfig has the section
