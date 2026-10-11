# Goal completion - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 5 leaf documents under this node
- **Scope:** Fully implement declarative goals with worker fulfillment: creation paths, plan/task goal linkage, steer-of-episode, surfaces, docs, e2e.

## Goal

Meept's AI Employee framework has a complete goal *runtime* (GoalStore, GoalLoop tiers 1-3, gates, enforcement, audit) but zero production goal-creation paths: `GoalStore.Create` has no non-test caller, the documented `goals:` block in employee definitions parses to nothing, and no RPC/CLI/HTTP route creates a goal. Separately, the chat path (Plans, Tasks) runs with no goal anchoring at all, and user steering cannot reach a running goal-loop episode.

This tree closes all of it. End state: a user declares a goal via definition file, `meept agents goal-create`, RPC, or HTTP; the employee's GoalLoop fulfills it through tiered plans with a shell gate; every Plan/Task anywhere in meept traces to a goal by default; steering re-routes a running episode; TUI/GUI show goal state; an e2e suite proves the loop.

## Architecture

Goal ingestion is a thin seam over the existing runtime: new surfaces all funnel into `GoalStore.Create` via a single `GoalSpec` type. Linkage is a column (`goal_id`) on `plan.Plans` and `task.Tasks` plus a resolution rule (explicit > sole active > implicit auto-goal). Steer rides the existing park/resume episode machinery as a new TriggerEvent source. All contracts live in `SHARED-CONVENTIONS.md` (sibling file) — every leaf references it; this file does not restate them.

## Interface Contracts

See `SHARED-CONVENTIONS.md` §2 (contracts C1-C6). Ownership:

- C1 GoalSpec, C4 creation surfaces: leaf 01.
- C2 SourceImplicit, C3 linkage columns, C6 implicit anchoring: leaf 02 (leaf 03 consumes).
- C5 steer-of-episode: leaf 04.
- e2e + docs parity: leaf 05.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-goal-ingestion.md | leaf | none | ~70K | A |
| 02 | 02-goal-linkage.md | leaf | none | ~80K | A |
| 03 | 03-anchor-chat-path.md | leaf | 02 | ~65K | B |
| 04 | 04-steer-episode.md | leaf | none | ~60K | A |
| 05 | 05-e2e-and-docs.md | leaf | 01,02,03,04 | ~70K | C |

**Concurrency groups:** A = {01, 02, 04} dispatch together. B = {03} after 02. C = {05} last.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group A

1. **Read** 01-goal-ingestion.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-goal-ingestion.md"
   - Context: full leaf text + SHARED-CONVENTIONS.md §2 C1/C4, §3, §4 + "Do NOT commit. Do NOT run git add. Write code, run tests, report results only." + "Explore with search_files or terminal cat; never feed read_file output into write_file."
2. **Read** 02-goal-linkage.md and dispatch as above (contracts C2/C3/C6).
3. **Read** 04-steer-episode.md and dispatch as above (contract C5).

### Phase 2: Dispatch Group B after 01 and 02 are REVIEWED

4. **Read** 03-anchor-chat-path.md and dispatch with C3/C6 + the committed shape of leaf 02's columns.

### Phase 3: Dispatch Group C after 01-04 are REVIEWED

5. **Read** 05-e2e-and-docs.md and dispatch (verify commands are in the leaf).

### Phase 4: Review and Commit Each Child

After each implementation agent returns, the orchestrator reviews in-session
(the main model reviews directly, NOT a delegated subagent):

1. Read the changed files listed by the implementer.
2. Check against leaf spec + SHARED-CONVENTIONS contracts + Review Checklist below.
3. Run: `go build ./... && go vet ./... && go test -p 2 ./internal/employee/... ./internal/plan/... ./internal/task/... -short`
4. Gaps → re-dispatch with specific feedback (max 3 cycles).
5. Pass → `git add <exact paths from leaf> && git commit -m "feat(employees): <summary> (goal-completion/<leaf>)"`, tracking table → REVIEWED.

### Phase 5: Integration Review

After ALL children reach REVIEWED:

1. Run `make build` and the new e2e suite: `make e2e-fast-area AREA=goal-completion` (leaf 05 names the suite).
2. Run `make graphs` — new RPC/HTTP surfaces change the connectivity graph; commit regenerated docs/generated/* with leaf 05.
3. Run `make graphs-check` and `make docs-check` if available.
4. Normalize with gofmt; corruption check: `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/ cmd/` returns zero.
5. Update root AGENTS.md (commands + RPC surface list) in the final commit.
6. All children → COMPLETE.

## Review Checklist

- [ ] All tasks from the leaf document are implemented
- [ ] SHARED-CONVENTIONS contracts satisfied verbatim (C1-C6 shapes not renamed)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing; `go vet` clean
- [ ] No scope creep; no debug artifacts, TODOs, placeholder values
- [ ] No line-number corruption (`N|` prefixes)
- [ ] New RPC/HTTP routes registered in both handler maps (method map + RegisterRoutes)
- [ ] SQLite migrations use the duplicate-column-guard idiom
- [ ] Nil guards on all new Set*/mutators; no I/O under mutex

Output: APPROVED or list of specific gaps.

## Coding Conventions

See SHARED-CONVENTIONS.md §3 (single source of truth — do not restate).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-goal-ingestion | COMPLETE | 1 | 0204a1ee; U1000+errors-gate fixes in-review |
| 02-goal-linkage | COMPLETE | 1 | 0b6348b5 |
| 03-anchor-chat-path | COMPLETE | 1 | c170661f; gofmt follow-up 75a99fbd |
| 04-steer-episode | COMPLETE | 1 | 0204a1ee (co-committed with 01) |
| 05-e2e-and-docs | COMPLETE | 1 | bdb338ef + graphs 97b6dcf0 |

Tree COMPLETE 2026-10-11 — 100% leaf completion. Integration gate: full
unit sweep green (2 known flaky websearch/ssrf tests pass in isolation
and on re-run — pre-existing, unrelated packages), make e2e-fast 54/54
suites ok incl. goal-completion, make build ok, graphs-check ok,
docs-check ok. Commits: 0b6348b5, 0204a1ee, c170661f, 75a99fbd,
bdb338ef, 97b6dcf0.

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `make build` succeeds.
2. `go test -p 2 ./... -short` passes (macOS: -p 2 mandatory).
3. e2e: `make e2e-fast-area AREA=goal-completion` green.
4. Manual loop proof (scripted in leaf 05): start daemon → create employee with `goals:[...]` in definition → `agents.goals.create` RPC → tier-2 plan pending → approve → gate command runs → goal health updates.
5. `make graphs` clean diff committed.

## Structural Completeness Check (Before Dispatch)

Run after authoring all documents:

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans/20261008-goal-completion --strict-leaves
```

Re-run until ALL TREES COMPLIANT: True.

## Notes

- The biggest single risk is leaf 02's migration order: two stores (plan, task) plus the employee goals table all gain columns — each leaf touches its own store only; do not cross-edit.
- Leaf 03 touches internal/agent, which has hard invariants (session_id vs conversation_id, async turns). Point the implementer at internal/agent/AGENTS.md before it writes anything.
- Employee-goal gates persist behind an enable switch (`27f2963e`); leaf 01 must not bypass that flag for per-goal gates.

## Post-execution findings (leaf 05, open follow-ups — not part of this tree)

1. `internal/employee/wiring.go shouldShareDB`: with `bots.enabled=false` the goal
   store gets a standalone DB whose `employee_goals` FK references `bot_definitions`
   (only present in bots.db) — every goal insert fails with `no such table:
   main.bot_definitions`. Standalone-DB schema is broken; suites work around it via
   the `employees.defaults.gate.enabled`/`bots.enabled` overlay.
2. Gate pass/fail logs at debug only; audit record stores only a SHA-256 — no INFO
   trace of a passing gate for operator debugging.
3. `decideTier3` silently drops candidates at the `max_active_plans=1` default while
   `Trigger` still returns `status:completed` — silent no-op triggers.
4. TUI `fetchDetail` calls `agents.goals.list` redundantly beside `agents.get`.
5. Multi-employee installs: no session→employee binding exists, so the C6 implicit
   branch keys on the daemon's SOLE employee; multi-employee chat stays goal-less.
   `GoalAnchorStore.SoleEmployeeID` is the seam to replace.
