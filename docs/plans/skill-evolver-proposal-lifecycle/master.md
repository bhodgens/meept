# Skill-Evolver Proposal Lifecycle Governance - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 leaf documents under this node
- **Scope:** Close four verified gaps in meept's skill-evolver proposal
  pipeline: machine plans polluting the repo, writer root ≠ discovery root,
  the approval dead end, and verifier verdict mismatch for archive proposals.

## Goal

The skill evolver (internal/skills/lifecycle) generates proposals on a 1h
interval with `auto_apply=false`, parks each proposal as a plan file, and
waits for a human decision. Four verified gaps make that loop non-functional
end-to-end:

1. **Machine plans pollute the repo** — the daemon's CWD is arbitrary (often
   the meept repo itself), so `PlanManager.resolvePlanDir`
   (internal/plan/manager.go:787-800 = `projectPath + docs/plans`) drops
   evolver-generated plans into the repo: **17 uncommitted
   `docs/plans/skill-evolution-*.md` files exist right now.** Machine-originated
   plans need a user-scoped sink (`~/.meept/plans/evolver/`); human-authored
   plans stay in the repo.
2. **Writer root ≠ discovery root** — discovery spans five tiers:
   `.meept/skills` (project), `~/.meept/skills` (user),
   `~/.config/meept/skills` (system), `~/.hermes/skills` (hermes), and
   `~/.claude/skills` (ClaudeSource, internal/skills/discovery.go:36-50, :28).
   The daemon constructs Writer+Versioner at `cfg.Daemon.DataDir + "/skills"`
   = `~/.meept/skills` (internal/daemon/components.go:1039-1040) — a directory
   that does not exist on this machine. The evolver-managed skills actually
   live in `~/.claude/skills/` (189 dirs). `Writer.ArchiveSkill`
   (internal/skills/lifecycle/writer.go:309) would fail: skill not found at
   the writer root. Archive/refine must resolve the skill's actual discovery
   tier and operate within it.
3. **Approval dead end** — `ApprovePlan` (internal/plan/manager.go:156) only
   records a signoff and triggers generic plan synthesis. Nothing bridges an
   approved skill-evolution plan back to the evolver's `applyProposal`
   (internal/skills/lifecycle/evolver.go:697) or
   `Writer.ArchiveSkill` (writer.go:309). Grep confirms **zero consumers of
   approved evolver plans**. The human gate has no actuator.
4. **Verifier verdict mismatch** — every proposal passes the 4-dimension
   content rubric (internal/skills/lifecycle/verifier.go: min score 0.75 over
   grounded_in_evidence / preserves_existing_value / reusable_elsewhere /
   safe_to_publish). Archive proposals carry **empty `CandidateContent`**
   (evolver.go:528-533) — there is no content to judge, so the rubric
   rubber-stamps and gates nothing. Archive proposals must gate on usage
   statistics alone (MinEffectiveness threshold, ≥10 injections — already
   computed in `passCPrune`, evolver.go:511); refine/create proposals keep
   the content rubric.

Runtime context (verified today): user config has `skills.evolver` enabled,
interval 1h, `run_on_start` true, `auto_apply` false; wiki enabled at
`~/.meept/wiki`. 10 archive + 5 improve + 1 decision-framework plan files
exist as evidence of the current behavior.

## Architecture

The evolver pipeline is: discover skills → propose (archive/refine/create) →
verify → park as plan (auto_apply=false) → [human approves] → apply. The four
leaves fix the pipeline at its seams without changing the propose/verify
core:

- **Leaf 01** moves the *sink*: the evolver's PlanManager is constructed with
  a user-scoped `Storage.ExternalPath` (the override already exists at
  internal/plan/manager.go:789; it just defaults to repo-relative) and stamps
  evolver-origin provenance into each machine plan so downstream leaves can
  identify them.
- **Leaf 02** fixes the *root*: skill name → discovery-tier resolution, so
  the Writer edits/moves skills inside the tier they were discovered from
  instead of a hard-coded `~/.meept/skills`.
- **Leaf 03** adds the *actuator*: an approved evolver plan triggers
  `applyProposal`/`ArchiveSkill` through the daemon's existing approval path.
- **Leaf 04** fixes the *gate*: per-action verifier semantics — usage-stats
  gate for archive, content rubric for refine/create.

Dependency order: sink (01) and root resolution (02) precede the actuator
(03); the verifier split (04) is independent.

## Interface Contracts

### Contract 1: Plan sink + provenance

```
// Config (internal/config, skills.evolver section, JSON5 quoted keys):
"plan_dir": ""   // default "" resolves to ~/.meept/plans/evolver (user-scoped)

// Daemon construction (internal/daemon/components.go):
// The evolver's PlanManager is built with Storage.ExternalPath set to the
// resolved plan_dir (absolute, ~-expanded). resolvePlanDir's default
// (projectPath + docs/plans) remains ONLY for human-authored plans.
// File: internal/plan/manager.go:789 — ExternalPath override already exists.

// Provenance: every evolver-created plan records machine origin so the
// approval actuator (Contract 3) can identify them. Encoding follows the
// existing plan file format (VERIFY with an actual docs/plans/skill-evolution-*.md
// file + internal/plan manager before coding):
//   origin: "skill-evolver"
//   proposal_id: <evolver proposal id>
//   action: "archive" | "refine" | "create"
```

Owner: 01-evolver-plan-sink.md. Consumers: 03.

### Contract 2: Discovery-tier resolution

```
// internal/skills/discovery.go — expose a tier lookup alongside the existing
// source list (discovery.go:36-50, ClaudeSource at :28). Shape to be adapted
// to the actual Discoverer API during implementation:
//   ResolveTierPath(skillName) -> (tierRoot, skillPath, source, error)
//   - searches the same tiers, same precedence, as discovery
//   - error when the skill exists in no tier

// internal/skills/lifecycle/writer.go — Writer (and Versioner, constructed
// together at components.go:1039-1040) resolves the skill's tier before any
// read/write/move. ArchiveSkill (writer.go:309) keeps its current archive
// semantics and naming; only the ROOT resolution changes: operate inside the
// tier that actually contains the skill (e.g. ~/.claude/skills/<name> on this
// machine), never the fixed ~/.meept/skills root.
```

Owner: 02-skill-root-resolution.md. Consumers: 03.

### Contract 3: Approval actuator

```
// internal/skills/lifecycle/evolver.go
// ApplyApprovedPlan applies an approved evolver-origin plan:
//   func (e *Evolver) ApplyApprovedPlan(planPath string) error
//   - requires provenance origin == "skill-evolver" (Contract 1); non-evolver
//     plans are rejected, not applied
//   - dispatch by action:
//       archive -> Writer.ArchiveSkill (tier-resolved per Contract 2)
//       refine  -> the applyProposal path (evolver.go:697)
//   - idempotent: an already-applied proposal is a no-op (verify existing
//     proposal state tracking before coding)

// Wiring (AGENTS.md wiring requirement — data structures without user-facing
// interfaces are INCOMPLETE): after a successful ApprovePlan
// (internal/plan/manager.go:156), a plan with evolver provenance triggers
// ApplyApprovedPlan. The seam is chosen in-leaf (ApprovePlan caller path,
// or a plan-approved callback/event) after reading how approval is surfaced
// today (TUI/CLI/HTTP). Every application logs an audit line:
//   applied evolver plan <file> action=<a> proposal=<id> result=<ok|err>
```

Owner: 03-approval-actuator.md. Depends on 01, 02.

### Contract 4: Per-action verifier gating

```
// internal/skills/lifecycle (verifier.go + evolver.go)
// Gate dispatch by proposal action:
//   archive  -> usage gate ONLY: injections >= 10 AND
//               effectiveness >= MinEffectiveness
//               (both thresholds already computed in passCPrune,
//               evolver.go:511 — single source of truth; extract shared
//               constants if duplicated)
//   refine /
//   create   -> existing 4-dim content rubric, min 0.75, unchanged
//               (grounded_in_evidence / preserves_existing_value /
//                reusable_elsewhere / safe_to_publish — verifier.go)
// The verdict records WHICH gate produced it (usage|content) for logs and
// tests. Archive proposals with empty CandidateContent (evolver.go:528-533)
// are never scored by the content rubric.
```

Owner: 04-verifier-per-action-semantics.md. Depends on none.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-evolver-plan-sink.md | leaf | none | 60K | A |
| 02 | 02-skill-root-resolution.md | leaf | none | 70K | A |
| 03 | 03-approval-actuator.md | leaf | 01, 02 | 70K | B |
| 04 | 04-verifier-per-action-semantics.md | leaf | none | 50K | A |

**Concurrency groups:** same letter = no inter-dependencies, dispatch
together (max 3 per batch).

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group [A]

Dispatch these children simultaneously:

1. **Read** 01-evolver-plan-sink.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-evolver-plan-sink.md"
   - Context: full leaf text + Contract 1 + coding conventions below +
     the current resolvePlanDir (internal/plan/manager.go:787-800), the
     ExternalPath override (:789), the evolver CreatePlan site
     (internal/skills/lifecycle/evolver.go:604), the skills.evolver config
     section, and ONE real `docs/plans/skill-evolution-*.md` file INLINED
     (so the agent can see the actual plan file format).
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests,
     report results only."
   - Include: "Do NOT use read_file on existing source files — explore with
     search_files or terminal cat instead. If you read a file, never feed its
     output into write_file."

2. **Read** 02-skill-root-resolution.md and dispatch via `delegate_task`:
   - Context: full leaf text + Contract 2 + conventions + INLINED source:
     internal/skills/discovery.go (tier list, :28, :36-50),
     internal/skills/lifecycle/writer.go (ArchiveSkill area, :309 and its
     archive-path construction), internal/daemon/components.go:1030-1050.
   - Same no-commit / no-read_file clauses.

3. **Read** 04-verifier-per-action-semantics.md and dispatch via
   `delegate_task`:
   - Context: full leaf text + Contract 4 + conventions + INLINED source:
     internal/skills/lifecycle/verifier.go (rubric + thresholds),
     internal/skills/lifecycle/evolver.go:500-540 (passCPrune + the empty
     CandidateContent site at :528-533).
   - Same no-commit / no-read_file clauses.

### Phase 2: Review and Commit Each Child

After each implementation agent returns, the orchestrator reviews in-session
(the main model reviews directly, NOT a delegated subagent):

1. **Orchestrator reviews in-session:** read the changed files from the
   implementer's file list; check against leaf spec + contracts + the Review
   Checklist below; run the leaf's specified test commands.

2. **If review finds gaps:** re-dispatch with specific feedback; max 3
   cycles, then escalate (halt and report).

3. **If review passes:** `git add <exact paths> && git commit -m
   "feat(skills): <leaf summary>"`. Update tracking table to REVIEWED.

### Phase 3: Group [B]

Dispatch 03-approval-actuator.md after BOTH 01 and 02 reach REVIEWED.
Context: full leaf text + Contracts 1-3 + conventions + INLINED source:
internal/plan/manager.go:140-200 (ApprovePlan) and its callers,
internal/skills/lifecycle/evolver.go:590-720 (CreatePlan site, applyProposal),
plus the leaf 01/02 diffs. Same review/commit protocol.

### Phase 4: Integration Review

After ALL children reach REVIEWED:

1. Run `go build ./...` and
   `go test ./internal/skills/... ./internal/plan/... ./internal/config/... ./internal/daemon/... -count=1`.
2. Verify the end-to-end loop on a temp HOME (see Integration Test Plan #2):
   evolver creates a plan in the sink (not the repo) → approve → skill
   archived inside its discovery tier → verifier gate matched the action.
3. Run `make lint-ci`.
4. Verify no line-number corruption:
   `grep -rcE '^\s+[0-9]+\|' --include='*.go' .` returns zero.
5. Commit integration changes; update tracking table to COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs (especially: no raw proposal content or key material
      in logs; no writes outside the resolved tier/sink roots)
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder
      values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source files
- [ ] Wiring present (AGENTS.md): 01 config documented + reachable; 03
      approval path triggers the actuator end-to-end

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+ (server)
- **Error handling:** wrap with `%w`; never `_ = err()`; return errors
  instead of swallowing when a skill/tier cannot be resolved
- **Mutex scope:** never hold a mutex across I/O; collect-under-lock then
  operate (mutexio analyzer enforces)
- **IDs:** never `time.Now().UnixNano()`/`math/rand`; use `pkg/id.Generate()`
- **Config:** JSON5 with quoted keys; behavioral settings in config, not env
- **Paths:** never hard-code `~/.meept/skills` or `~/.claude/skills` literals
  in product code — resolution comes from discovery/config; `~` expansion
  goes through the existing helper (find it, don't reinvent)
- **Tests:** table-driven where natural; `_test.go` alongside; use temp-dir
  HOME fixtures for tier resolution tests, never the developer's real home
- **Clean fixes (AGENTS.md):** prefer the clean architectural fix over
  workarounds — e.g. fix root resolution at the source (leaf 02) rather than
  special-casing archive paths per tier at call sites
- **Formatting:** `gofmt` before reporting completion

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-evolver-plan-sink | COMPLETE | 287e1817 | sink + provenance + docs; tests: TestEvolverPlanDir(7)/Provenance(4)/Sink(3); plan regression green |
| 02-skill-root-resolution | COMPLETE | acbbdaa1 | ResolveTierPath + Writer TierResolver seam; components.go unchanged; writer/discovery suites green |
| 03-approval-actuator | COMPLETE | ddaeae96 | ApplyApprovedPlan + plan.approved bus bridge; ExtraMeta round-trip fix (approval was stripping provenance); SQLite WAL DSN fix; no import cycle |
| 04-verifier-per-action-semantics | COMPLETE | 287e1817 | usage gate for archive (polarity corrected: gate re-verifies passCPrune selection), rubric frozen for refine/create, Gate field; 0 rubric deletions |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. **Unit + package:** `go test ./internal/skills/... ./internal/plan/...
   ./internal/config/... ./internal/daemon/... -count=1`.
2. **End-to-end (temp HOME fixture):** with a fixture home containing a fake
   skill in a non-default tier (e.g. `<home>/.claude/skills/fixture-skill`)
   and usage stats past the archive thresholds:
   (a) an evolver run with auto_apply=false writes its plan under the
   configured sink (`~/.meept/plans/evolver/`), NOT the repo's docs/plans;
   (b) the plan carries evolver provenance;
   (c) approving it via the plan manager triggers ApplyApprovedPlan;
   (d) the skill is archived inside its discovery tier;
   (e) the repo working tree gained no new `docs/plans/skill-evolution-*.md`.
3. **Verifier gate boundary:** archive proposal with stats below thresholds
   → rejected with a usage-gate verdict; refine proposal with weak content →
   rejected by the content rubric; no cross-gating.
4. **Regression:** existing plan manager, discovery, and verifier tests stay
   green unchanged.
5. **Analyzers:** `make lint-ci` green.

## Open Questions

- **Archive destination naming:** leaf 02 keeps Writer's existing archive
  semantics and fixes only root resolution. If Writer's current archive path
  encoding assumes the `~/.meept/skills` root in a way that can't be
  parameterized cleanly, escalate before inventing a new layout.
- **Stray repo files:** the 17 existing uncommitted
  `docs/plans/skill-evolution-*.md` files are evidence, not work items.
  Moving/deleting them is a user decision, explicitly out of scope for all
  leaves.
- **Approval surface:** if no user-facing approval path exists for plans
  today beyond code/API, leaf 03 wires through whatever exists (manager
  caller) and reports the gap — do not build a new UI in this tree.

## Notes

- All file:line evidence was verified on 2026-08-31 against this working
  tree. Re-verify line numbers if the tree has moved since.
- The evolver config reality: enabled, interval 1h, run_on_start true,
  auto_apply false. Do not change auto_apply semantics — the human gate
  stays; leaf 03 only adds the actuator behind it.
- Leaf 01 must NOT relocate human-authored plans: the repo's `docs/plans/`
  trees (including this one) keep working exactly as before. Only
  machine-originated (evolver) plans move to the sink.
- Leaf 02 fixture tests must use temp HOMEs. Never touch the developer's
  real `~/.claude/skills` (189 live dirs) in tests.
- internal/daemon components wiring is the production construction path for
  Writer/Versioner/evolver; keep changes backward-compatible with any other
  construction sites found via search.
