# Memory Second Brain: Default-On + Gap Closure - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 10 (1 test-gate leaf, 1 branch with 4 children, 5 standalone leaves)
- **Scope:** Flip every memory subsystem default-on and close the audited second-brain gaps: memory scoping, recall telemetry, cache-friendly injection, claim-status leak, doctor visibility, bridges validation.

## Goal

Meept's memory stack (internal/memory, ~31k LOC) has complete organs - episodic
FTS5, task memory, knowledge graph, vector hybrid search, epistemic claims with
human review, L0/L1/L2 distillation, usefulness voting, bridging facts,
knowledge assets - but a stock install runs with all of them OFF, memory recall
is GLOBAL (cross-project and bench-fixture leakage is a proven defect class),
recall injection is a 2000-token volatile block, and nothing measures whether an
injected memory helped.

This tree makes the full pipeline the out-of-box behavior and closes the audited
gaps so "second brain" is honest: the brain knows whose facts it holds (scoping),
records everything by default (L0/L1/L2 default-on), learns what is useful
(telemetry + voting), and reports its own health (doctor).

## User decisions (frozen 2026-10-30)

| # | Decision | User words |
|---|----------|------------|
| D1 | Bridges (IndexRAG) default-ON now; the substitution-experiment leaf (07) may flip it back if it loses | "Q1 a" - enable by default now, experiment validates after and can flip back |
| D2 | Scoping: per-project default; agent definitions declare the widest scope they may recall (project/user/global) so a chief-of-staff agent can span projects | "Memory should initially be scoped per project for project-scoped agents/sessions - however, the case may be different for an agent that's intended for eg. chief of staff over numerous projects" |
| D3 | Full default-on: L0 capture + L1 extraction + L2 scenes on every daemon; cost is local compute | "Q3) sure" (chose full default-on) |

These flips are product decisions and the user authorized them here. Record
them in `docs/agents/opt-in-defaults.md` and `docs/workflows/memory.md` in the
same commits as the flips (leaf 02).

## Architecture

Memory capture is turn-terminal driven: a daemon bus subscriber
(`internal/daemon/memory_capture.go`, `WireMemoryCapture`) appends L0 raw turns,
queue jobs (`memory.l1_extract`, `memory.l2_scene`) distill claims and scenes,
and recall lands in the system prompt as a `<memory-context>` fence bounded by
`MaxMemoryContextTokens`. This tree threads a Scope field through storage and
recall (branch 01), flips the default-off knobs (leaf 02), splits recall into
stable-append vs volatile-prepend for prompt-cache health (leaf 03), records
recall outcomes for the calibration loop (leaf 04), stamps claim transitions in
place (leaf 05), surfaces pipeline state in `meept doctor` (leaf 06), and runs
the bridges substitution experiment that can flip bridges back off (leaf 07).

File-contention map (internal/memory is ONE package - serialize these):
- Leaves 01-scoping/01 and 01-scoping/03 both touch internal/memory store files - sequence them.
- Leaves 03 and 04 both touch internal/agent memory-assembly (loop.go budget path) - 04 depends on 03.

## Interface Contracts

### C1: Memory Scope enum + field

```
// Owner: 01-scoping/01-scope-schema.md
package memory
type Scope string
const (
    ScopeProject Scope = "project" // visible only to sessions bound to the same project
    ScopeUser    Scope = "user"    // visible to all sessions of the user (multiuser) / install (single-user)
    ScopeGlobal  Scope = "global"  // visible everywhere (explicit cross-project facts; legacy rows)
)
// Memory gains: Scope Scope `json:"scope,omitempty"`
// Storage: metadata key "scope" everywhere; SQLite stores also gain a
// `scope TEXT NOT NULL DEFAULT 'global'` column (episodic ftstore + task store).
// LEGACY BACKFILL = 'global' so no existing memory disappears from recall.
// Consumers: 01-scoping/02, 01-scoping/03, 00T-e2e
```

### C2: Scope-aware recall filter

```
// Owner: 01-scoping/01-scope-schema.md
type SearchOption func(*searchOptions)
func WithScopeFilter(maxScope Scope, projectID, userID string) SearchOption
// Manager.Search and GetContext accept variadic ...SearchOption (additive;
// existing callers compile unchanged). A row is recallable iff scope==global,
// or scope==user AND userID matches (empty userID matches in single-user),
// or scope==project AND projectID matches.
// Consumers: 01-scoping/02 (wires agent scope + tools), 00T-e2e
```

### C3: Agent memory max-scope declaration

```
// Owner: 01-scoping/02-agent-recall.md
// AgentSpec / employee definition gains:
MemoryMaxScope string `json:"memory_max_scope,omitempty"` // "", "project" (default), "user", "global"
// Resolution: empty -> ScopeProject. chief-of-staff style agents set "global".
// The loop passes (maxScope, session projectID, userID) into WithScopeFilter
// at recall time; memory_store/memory_retain stamp writes with the session's
// narrowest applicable scope (explicit tool arg can widen up to maxScope).
// Consumers: 00T-e2e
```

### C4: Default flips (single source of truth)

```
// Owner: 02-defaults-on.md
// internal/config/schema.go defaults become:
//   memory.l0_enabled = true
//   memory.usefulness.enabled = true
//   memory.epistemic.ambient_extraction.enabled = true
//   memory.knowledge.enabled = true
//   memory.bridges.enabled = true
// Template config/meept.json5 mirrors them with // drift: annotations where
// installed homes differ. Docs updated in same commits:
// docs/workflows/memory.md, docs/agents/opt-in-defaults.md.
// Consumers: 00T-e2e, 07-bridges-eval
```

### C5: Recall outcome calibration row

```
// Owner: 04-recall-telemetry.md
// <memory.data_dir>/recall_outcomes.jsonl, one JSON object per line:
{"ts":"RFC3339","session_id":"...","memory_id":"...","query":"...","injected":true,"cited":true,"tokens":123}
// 10 MiB cap + one rotated generation, same mechanism as the other
// calibration JSONL logs (rejected_candidates/claim_verdicts/raw_responses).
// Gitignored: contains user text. "cited" = injected item's salient text
// appears (fuzzy, lowercase, >=8-token overlap) in the final assistant reply.
// Consumers: 00T-e2e
```

### C6: Stable/volatile recall split

```
// Owner: 03-stable-volatile-split.md
// Memory items carry stable bool (metadata "stable"; persona/standing facts
// are stable, per-turn recall is volatile). Assembly splits:
//   stable  -> builder.AddSectionWithStability("Memory (standing)", ..., true)
//   volatile -> existing <memory-context> fence, budgeted as today
// Whole-item budget still enforced over the volatile set; the stable set is
// small and changes rarely, preserving prompt-cache prefixes.
// Consumers: 04 (builds on the split assembly), 00T-e2e
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-e2e-memory-pipeline.md | leaf (test gate) | none | 70K | T |
| 01 | 01-scoping/ (branch) | branch | 00T | - | A/B |
| 02 | 02-defaults-on.md | leaf | 00T | 55K | B |
| 03 | 03-stable-volatile-split.md | leaf | none | 65K | A |
| 04 | 04-recall-telemetry.md | leaf | 03 | 65K | C |
| 05 | 05-claim-status-stamping.md | leaf | none | 40K | A |
| 06 | 06-doctor-memory.md | leaf | 02 (reports new defaults) | 45K | D |
| 07 | 07-bridges-substitution-eval.md | leaf | 02 | 60K | D |

Dispatch order: 00T gate -> group A (03, 05) -> group B (01-scoping after its
own gate, 02) -> group C (04) -> group D (06, 07).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T-e2e-memory-pipeline.md first: tests only (hermetic e2e suites
under e2e/suites/ + manifest path_map entries), fail RED for
missing-implementation reasons. Validate in-session (never delegate review)
against the Test Gate matrix; apply the delta protocol; commit gate tests;
set children to TESTS_VALIDATED. See references/tests-first-gate.md.

Repo e2e rules the test author must follow (root AGENTS.md): suites in
`e2e/suites/<name>/` with the `e2e` build tag, manifest + path->suite mapping
in `e2e/manifest.json` in the same commit, scratch MEEPT_HOME harness pattern
(harness.WithPreBootHook writes the REAL on-disk store the boot wiring
resolves; assert through live RPC surface, not store internals).

### Phase 1: Dispatch Concurrency Groups

Dispatch same-letter children simultaneously via `delegate_task`: goal =
"Implement all tasks from <leaf path>"; context = full leaf text + the frozen
contracts it consumes + conventions below + relevant source INLINED; include
the no-commit, no-git-add, no-read_file-on-source, write-once-and-stop rules.

internal/memory is a single package: never dispatch two leaves that edit it in
the same batch (01-scoping serializes its own store-file leaves internally).

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run the leaf's verify commands; gate-test
diff must be empty. Pass: commit (explicit pathspecs, single-line messages;
orchestrator is the only committer) + status REVIEWED. Gaps: re-dispatch with
findings, max 3 cycles, then escalate.

Commit hygiene for this tree (repo-proven): run the affected gates directly
(gofmt, go vet scoped, `go test -p 2 <touched packages>`); `git add` every new
file (test files included) BEFORE `git commit`; normalize any new .md artifact
to ASCII (the pre-commit-ascii gate); every commit reviews root AGENTS.md.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts C1-C6 across the combined tree,
run `go build ./...` + `make test` (bounded, background, output to file) +
the new e2e suites in isolation, run `make graphs` (RPC/config surface
changed) and `make docs-generate` if config gendoc drifted. Gaps: re-dispatch
the failing child. Pass: set children COMPLETE, record final statuses.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
      (any such diff = automatic NEEDS_FIXES)
- [ ] Tests written and passing (TDD followed; e2e for features, unit pins
      allowed only for bug fixes per repo e2e policy)
- [ ] Code follows project conventions (no ignored errors, no mutex across
      I/O, typed-nil guards on wiring seams)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept). GOTOOLCHAIN is pinned
  in-repo (go.mod + Makefile) - plain make targets need no env prefix.
- **Naming:** exported for cross-package seams (Scope, WithScopeFilter);
  unexported internals. Config structs carry `json:` AND `toml:` tags plus
  `//gendoc:` comments - copy the neighboring style.
- **Error handling:** wrap with %w; no ignored errors; no bare panic(err).
  Capture/distill paths log and never fail the turn.
- **Testing:** new FEATURE tests are hermetic e2e suites (e2e/suites/ +
  manifest); unit tests only for bug-fix pins (leaf 05). Table-driven.
- **Defaults:** every flip records the user authorization (D1-D3) in
  docs/agents/opt-in-defaults.md; flipping a default is a product decision -
  it is AUTHORIZED here, cite this plan.
- **Formatting:** gofmt before reporting.
- **Never:** time.Now().UnixNano()/math/rand for IDs (pkg/id.Generate);
  os.Getwd() fallbacks in internal/; heredoc file writes inside package dirs.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element | Verified by |
|-----|---------------------------------------------|--------------|-------------|
| R1 | "enable it all by default" | 02-defaults-on.md; C4 | 00T suite memory-defaults; `meept config get` assertions |
| R2 | "Q1 a" - bridges default-on now, experiment may flip back | 02-defaults-on.md (bridges=true) + 07-bridges-substitution-eval.md | eval harness artifact + flip-back decision recorded |
| R3 | "scoped per project... different for an agent that's intended for eg. chief of staff over numerous projects" | 01-scoping/ branch; C1-C3 | 00T suite memory-scoping (isolation + chief-of-staff recall) |
| R4 | gap: recall injection small/static/cache-hostile | 03-stable-volatile-split.md; C6 | 00T suite asserts stable section + budget behavior |
| R5 | gap: no recall-effectiveness telemetry | 04-recall-telemetry.md; C5 | 00T suite asserts recall_outcomes.jsonl row with cited flag |
| R6 | gap: claim status transitions leak stale rows | 05-claim-status-stamping.md | unit pin tests (bug-fix class) |
| R7 | gap: operator cannot see memory pipeline state | 06-doctor-memory.md | `meept doctor` check output assertions |
| R8 | repo e2e policy: all new feature tests hermetic e2e | 00T gate leaves + per-leaf suites | e2e/manifest.json entries land with suites |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-e2e-memory-pipeline.md | PENDING | 0 | |
| 01-scoping/orchestrator.md | PENDING | 0 | |
| 02-defaults-on.md | PENDING | 0 | |
| 03-stable-volatile-split.md | PENDING | 0 | |
| 04-recall-telemetry.md | PENDING | 0 | |
| 05-claim-status-stamping.md | PENDING | 0 | |
| 06-doctor-memory.md | PENDING | 0 | |
| 07-bridges-substitution-eval.md | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T reaches
TESTS_VALIDATED. Branch 01 governs its own children's statuses.

## Integration Test Plan

1. `go build ./...` - clean combined tree.
2. `go test -p 2 ./internal/memory/... ./internal/agent/... ./internal/config/... ./internal/daemon/...` - green.
3. `go run ./e2e/... -suite memory-scoping` and `-suite memory-defaults` (or
   the manifest runner: `make e2e-fast-area AREA=<name>`) - green in isolation.
4. Scratch-daemon live gate (repo live-gate rule): boot with DEFAULT config in
   a scratch MEEPT_HOME, run one real chat turn, assert on disk:
   l0/YYYY-MM-DD.jsonl gained lines, a memory.l1_extract job enqueued,
   recall_outcomes.jsonl created after a second turn, doctor shows the memory
   check armed.
5. `make graphs` + `make graphs-check` fresh (RPC/CLI surface changed).
6. `scripts/config-drift-check.py` - any new drift is annotated in the
   template or classified.

## Test Gate

Test leaf: 00T-e2e-memory-pipeline.md - dispatched in Phase 0, validated by
the control agent (orchestrator, in-session). Branch 01-scoping has its own
gate leaf (01-scoping/00T-tests.md).

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | All memory subsystems default-on | C4; 02-defaults-on.md | n/a (config defaults, e2e covers) | memory-defaults suite: l0 file + l1 job + knowledge tool registered on default boot | PENDING |
| F2 | Project-scoped memory isolation | C1, C2; 01-scoping/01 | n/a | memory-scoping suite: fact planted in project A not recalled in project B | PENDING |
| F3 | Chief-of-staff cross-project recall | C3; 01-scoping/02 | n/a | memory-scoping suite: global-max agent recalls project A fact | PENDING |
| F4 | Stable/volatile recall split | C6; 03-stable-volatile-split.md | n/a | memory-defaults suite: stable item renders in stable section, volatile in fence | PENDING |
| F5 | Recall outcome telemetry | C5; 04-recall-telemetry.md | n/a | memory-defaults suite: recall_outcomes.jsonl row written with cited flag | PENDING |
| F6 | Claim status stamping in place | 05-claim-status-stamping.md | TestMarkResolvedInPlace-style pins (bug-fix unit allowed) | n/a (store-level) | PENDING |
| F7 | Doctor memory pipeline check | 06-doctor-memory.md | n/a | scratch boot: `meept doctor` lists memory check | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: (empty - no validations yet)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda (dated, appended to the dispatch brief). Never edit a
dispatched leaf document mid-execution.

## Notes

- Scope is defense-in-depth, not the bench-fixture fix: the REAL fix for bench
  contamination is the harness running with a scratch MEEPT_HOME (recorded in
  the meept-development skill). When the e2e harness gains scope support, also
  plant bench markers with ScopeProject("bench") if the harness still shares a
  home - cheap belt-and-braces, note in 01-scoping/03's report, do not block on it.
- Attribution fields are BUILT but UNWIRED at recall: Memory.AgentID/
  SessionID/TaskID/BotID exist and are written (internal/memory/types.go:54-61;
  internal/memory/manager.go:553 enriches metadata on store), but no Search
  path filters by them - ScopedMemoryManager's bot_id filter is the only
  working tag-filter precedent. Leaf 01-scope-schema must RECONCILE with
  these fields (reuse or extend, never mint parallel storage) and model
  WithScopeFilter on the ScopedMemoryManager filter pattern. A project field
  is still NEW (sessions carry projects; memories do not record them).
- Existing ScopedMemoryManager (bot_id tagging) is a DIFFERENT mechanism
  (per-bot). Do not repurpose it for project scoping; leave it intact - but
  REUSE its filter pattern as the WithScopeFilter reference implementation.
- internal/memory contention: leaves 01-scoping/01 and 01-scoping/03 both edit
  store files; the branch orchestrator must sequence them.
- Bridges evidence for D1: docs/research/eval/bridges-live-eval.json (tie at
  87.5%, ~10% more tokens). Leaf 07 owns the flip-back decision procedure.
- Claim-leak caveat source: references/memory-extension-seams.md ("verify
  MarkResolved/RecordReview before reusing"). Leaf 05 verifies, then fixes.
- Known pre-existing dead code the tree must NOT absorb silently:
  InjectContextBounded (internal/agent/conversation.go, zero callers) and the
  daemon-unused SetEpistemicHook agent seam - out of scope; flag in reports
  if touched, deletion is a separate cleanup pass.
