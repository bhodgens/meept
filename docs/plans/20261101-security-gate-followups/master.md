# Security Gate Follow-ups - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 4 leaf documents under this node
- **Scope:** Close the four follow-ups recorded when the tool-provider-seam
  tree (docs/plans/20261031-tool-provider-seam, issue #78) completed.

## Goal

The tool-provider-seam tree armed a centralized execution gate and recorded
four follow-ups. This tree closes them:

1. Wire `SecurityConfig.BlockedPaths` into the security engine: the field
   is documented and validated at load but NO production code reads it -
   file-category tool calls never consult it.
2. Delete the dead early scheduler-Register block inside
   `registerBuiltinTools` (its `sched` parameter is nil at the only call
   site because the scheduler is constructed later).
3. Delete `githubsvc.RegisterGitHubTools` (zero production callers since
   the provider seam landed) and re-home its tests against the provider
   path.
4. A no-op correction recorded for honesty: the "streaming tools bypass the
   gate" concern from the previous tree is INVALID - GateCheck precedes the
   streaming branch in the executor retry loop, so streaming calls are
   already gated. This tree adds a pin test proving it, nothing more.

## Architecture

Leaf 01 is the only behavior change (BlockedPaths wiring); leaves 02-04 are
dead-code removal and test re-homing. All four are small: one leaf each,
no branches. The BlockedPaths check lives in the security engine's
file-category path stages (where AllowedPaths already works - mirror its
matching logic inverted).

## Interface Contracts

### C1: BlockedPaths engine wiring

```go
// internal/security/engine.go - inside the file-category check stage
// (where AllowedPaths/path checks run for ActionFileRead/Write/Delete):
//   - load cfg.Security.BlockedPaths into the engine at NewEngine time
//     (same field the schema validates at config load)
//   - glob/exact match (reuse the AllowedPaths matcher, inverted)
//   - a match => Decision{Allowed: false, Reason naming the blocked path
//     rule, RiskLevel: high}
```

- Owner: 01-blocked-paths.md
- Consumers: 02-pin-streaming-gate.md (its e2e assert can rely on deny)

### C2: provider-only github registration

```go
// internal/githubsvc/register.go: RegisterGitHubTools DELETED.
// internal/tools/builtin/github_register_test.go + github_readonly_test.go:
// tests re-homed to construct via builtin.SetGitHubProvider + toolprov
// discovery instead.
```

- Owner: 03-remove-registergithubtools.md

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-blocked-paths.md | leaf | none | 60K | A |
| 02 | 02-pin-streaming-gate.md | leaf | none | 35K | A |
| 03 | 03-remove-registergithubtools.md | leaf | none | 50K | A |
| 04 | 04-dead-scheduler-block.md | leaf | none | 40K | A |

All four are independent - dispatch together.

## Dispatch Protocol

### Phase 1: Dispatch Group A

All four leaves simultaneously via `delegate_task`: goal = "Implement all
tasks from <leaf>"; context = full leaf text + contracts + conventions +
relevant source INLINED; include no-commit, no-git-add, no-read_file,
write-once-and-stop rules.

### Phase 2: Review and Commit Each Child

Review in-session (main model, never delegated). Check leaf spec +
contracts; run verify commands; gate tests untouched. Commit with explicit
pathspecs; update the tracking table.

### Phase 3: Integration Review

Full scoped suite across touched packages + `make e2e-fast-area` for any
affected suite; close-out note on the tracking table. No issue link - this
tree executes recorded follow-ups, not a tracked issue.

## Review Checklist

- [ ] All tasks implemented; tests passing; gate tests untouched
- [ ] Interface contracts satisfied exactly
- [ ] No scope creep; no debug artifacts; ASCII-clean added lines
- [ ] gofmt clean; no line-number corruption

Output: APPROVED or specific gaps.

## Coding Conventions

- Go toolchain: GOTOOLCHAIN=go1.26.5 prefix on every go command (bare `go`
  is 1.27 and breaks analyzers)
- Error handling: wrap %w, return early; no ignored errors
- Tests: table-driven; hermetic e2e for new features (manifest entry in
  the same change); unit tests fine for the dead-code + re-homing leaves
- IDs: pkg/id.Generate() never time/math/rand
- No mutex across I/O; typed-nil guards on setters
- gofmt before reporting; ASCII-only added lines

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | BlockedPaths dead knob (tool-provider-seam tracking table) | 01-blocked-paths.md | engine unit test: blocked path denies |
| R2 | streaming-gate concern raised in tree closeout | 02-pin-streaming-gate.md | pin test: streaming call passes GateCheck |
| R3 | RegisterGitHubTools test-only (tool-provider-seam observation) | 03-remove-registergithubtools.md | build green without the symbol; tests re-homed |
| R4 | dead scheduler block (03-migrate observation) | 04-dead-scheduler-block.md | build green; scheduler tools still register via provider |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-blocked-paths.md | COMPLETE | 0 | RED observed on config-driven entries with no DB seed; deny-wins pinned; commit 2331d457 + recursive-glob fix cc23528e (dispatcher-05 e2e surfaced: filepath.Match never crosses /, harness seeds <work>/** - matchEnginePathPattern added, regression unit test) |
| 02-pin-streaming-gate.md | COMPLETE | 0 | pin test green; mutation RED observed (gate call removed => sequence assertion fails) and reverted byte-identical (diff empty); commit with leaf 02 batch; CORRECTION: streaming calls were already gated - this leaf is a pin only |
| 03-remove-registergithubtools.md | COMPLETE | 0 | function deleted, tests re-homed via SetGitHubProvider + RegisterAll preserving all assertions; zero stragglers; commit e84be514; also removed dead 'times' field in loop_turnjournal_test.go (sibling file, U1000-flagged, TestTurnJournal green) |
| 04-dead-scheduler-block.md | COMPLETE | 0 | nil-order evidence re-verified before deletion (call site precedes assignment, not SUPERSEDED); block + sched param + call-site arg removed; NewScheduleCreateTool count now 0 in components.go (provider-only); commit 140c6517; hermes Client deadcode rebaseline attributed to sibling hermes-stub leaf |

## Integration Test Plan

1. `GOTOOLCHAIN=go1.26.5 go build ./...`
2. `GOTOOLCHAIN=go1.26.5 go test -p 2 ./internal/security/ ./internal/githubsvc/ ./internal/tools/builtin/ ./internal/daemon/`
3. `make e2e-fast-area AREA=tool-provider-seam` (regression: gate behaviors unchanged)

## Test Gate

Small tree; per-leaf TDD instead of a separate test-gate leaf. Each leaf's
tasks carry RED-first tests. Matrix:

| # | Feature | Source | Tests | Status |
|---|---------|--------|-------|--------|
| F1 | BlockedPaths denies file tools | 01 T1-T2 | engine unit tests | VALIDATED |
| F2 | streaming calls gated (pin) | 02 T1 | executor pin test | VALIDATED |
| F3 | provider-only github registration | 03 T1-T2 | re-homed tests | VALIDATED |
| F4 | dead block removed | 04 T1 | build + scheduler-provider test | VALIDATED |

## Frozen Documents

Leaves freeze at dispatch; corrections ride dispatch context as dated
addenda.

## Notes

- Shared tree carries ACTIVE sibling WIP (token-efficiency/context trees;
  internal/llm + internal/context have been mid-edit). Leaves touch:
  internal/security/engine.go (01), internal/agent/executor.go + a test
  file (02), internal/githubsvc/register.go + builtin github tests (03),
  internal/daemon/components.go (04 - SHARED, use small surgical edits and
  re-read before writing). Expect gate races; use the proven protocols:
  scoped verification, explicit pathspecs, attribution in commit bodies.
- 04's line numbers WILL have drifted; locate by symbol, not by line.
