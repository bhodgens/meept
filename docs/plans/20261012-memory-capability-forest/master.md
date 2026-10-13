# Memory Capability Forest — Root Master

> **For Hermes:** Use the `hierarchical-plan-execution` skill. Dispatch leaf
> agents via `delegate_task` batch shape: `tasks:[{goal, context}]`. Review
> in-session (main model), never via delegated reviewer.

**Goal:** Extend meept's memory system with four capabilities adapted from
TencentDB-Agent-Memory (@ b1f79fc, `/tmp/tencent-memory-review`): layered
async distillation, LLM conflict merging, human governance surfaces, and
opt-in knowledge assets.

**Architecture:** Meept memory is one in-process Go package (`internal/memory`).
Each capability lands as a new subpackage wired through the existing Manager,
PersistentQueue, RPC, and auditlog seams. No new external services. Async work
reuses `internal/queue.PersistentQueue`; no Redis.

**Tech stack:** Go 1.26 (toolchain pinned in-repo), modernc SQLite, existing
`memory_model` LLM slot, existing FTS5/vector stores.

## Child Index (four trees, one per observation)

| Tree | Directory | Scope | Leaves | Est. context/leaf |
|------|-----------|-------|--------|-------------------|
| 01 | `01-layered-async-distill/` | L0 raw tier + async L1/L2 distill pipeline (LLM scene segmentation) | 3 | 60-90K |
| 02 | `02-conflict-merge/` | LLM batch conflict detection with merge decisions | 2 | 50-80K |
| 03 | `03-human-governance/` | Review queue CLI/RPC + TUI view, verdicts, audit | 3 | 50-70K |
| 04 | `04-knowledge-assets/` | Opt-in knowledge store: wiki pages + Go code index | 4 | 60-80K |

Conventions, frozen contracts, waves: `SHARED-CONVENTIONS.md` (all leaves and
orchestrators reference it; nothing re-declares it).

## Dependency graph and waves

```
Wave 1: T01-L1 (l0 recorder)   T03-L1 (review CLI/RPC)   T04-L1 (knowledge store)
Wave 2: T01-L2 (async worker)  T02-L1 (conflict pkg)     T04-L2 (retrieval tools)
Wave 3: T01-L3 (wiring+e2e) → T02-L2 (wiring) → T03-L2 (audit wiring) → T04-L3 (e2e) → root integration
```

- T02 depends on T01-L2 only at wiring time (conflict detection runs inside
  the L1 extract job); the `conflict` package itself is independent.
- T03 is independent of T01 (review operates on existing `status=auto` claims
  today; T01 increases the population it reviews).
- T04 is fully independent until root integration.

## Interface contracts

Frozen in `SHARED-CONVENTIONS.md` §2 (L0 record schema, job types, conflict
Decision type, review RPC surface, knowledge Page type). Any contract change
is an edit to that file plus a note in the affected master, never silent leaf
drift.

## Dispatch Protocol

1. For each leaf, in wave order: dispatch via `delegate_task`, one call per
   leaf batch (goal + context with the full leaf text inline).
2. Every dispatch context includes: leaf path, relevant SHARED-CONVENTIONS §,
   "Do NOT commit. Do NOT run git add.", exact verify commands, license-gate
   rule for any new dep.
3. On return: review in-session — build (`go build ./...`), targeted test run,
   read the diff, check contracts and stray artifacts (TODO/FPrintln/debug).
4. Gaps → re-dispatch with specific feedback (max 3 iterations), then
   orchestrator-completes-directly for small residue.
5. Commit with explicit paths per leaf; update tracking table.
6. Wave gate: no wave-2 dispatch until wave-1 leaves are committed.

## Review Checklist (per leaf)

- [ ] `go build ./...` clean; `go vet ./internal/memory/...` (or leaf pkg) clean
- [ ] Targeted tests pass; e2e leaves pass `make e2e-fast-area AREA=<name>`
- [ ] Contract fields match SHARED-CONVENTIONS §2 exactly
- [ ] No GPL/AGPL dependency introduced
- [ ] No debug artifacts, TODOs, placeholder values, line-number corruption
      (`grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/` = 0)
- [ ] New config fields default false/opt-in with docs
- [ ] Commit uses explicit paths; no sibling sweep

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| T01-L1 l0-recorder | COMPLETE 2026-10-13 | commit 13c14b56; -race clean; ReadSession takes l0 dir |
| T01-L2 async-worker | COMPLETE 2026-10-13 | commit 3d902682; worker.Process matches worker.JobProcessor shape; L2 summary derived (no 2nd LLM call) |
| T01-L3 wiring+e2e | COMPLETE 2026-10-13 | commit eb997b5c; session-end hook seam; day-boundary marker fix; e2e suite green; shared-file hunks pre-swept by 370a1057 (verified in-commit) |
| T02-L1 conflict-detector | COMPLETE 2026-10-13 | commit 9b87968c; 13/13 tests, -race clean; additive RecallStore.EmbeddingsAccepted |
| T02-L2 merge wiring | COMPLETE 2026-10-13 | commit 4cd352fb; recall adapter uses MatchAny FTS (SearchSemantic AND-binds, structurally blind to near-conflicts); e2e scenarios in memory-persistence |
| T03-L1 review-cli-rpc | COMPLETE 2026-10-13 | re-scoped → review-filters-audit; commit c83f97c5; daemon wiring rode into sibling commit bcf1e87c (intact at daemon.go:1181) |
| T03-L2 audit+daemon-only wiring | SUPERSEDED | folded into re-scoped L1 (audit done there); daemon-only was already true |
| T03-L3 tui-review-view | COMPLETE 2026-10-13 | leaf L2 of tree 03; commit on main after 8cd51ede; key v, tableutil rules honored, status count rides modal fetches; GUI deviation documented in gui_inventory.json |
| T04-L1 knowledge-store | COMPLETE 2026-10-13 | commit 47d99578; additive Page.UnresolvedLinks accepted; -race clean |
| T04-L2 knowledge-tools | COMPLETE 2026-10-13 | commit e3f82864; consolidation hook absorbed into sibling 03dfcf3a (intact); ToolActionMap entries + config default false |
| T04-L3 code-index | COMPLETE 2026-10-13 | commit 8cd51ede; additive code_edges schema; suffix-based bare-symbol matching documented as v1 heuristic |
| T04-L4 knowledge-e2e | COMPLETE 2026-10-13 | knowledge_wiring.go closures, knowledge_impact tool, e2e suite (3 scenarios) green, knowledge stats CLI; docs-check green |
| Root integration review | PENDING | full `make test` + `make e2e-fast` + `make graphs-check` (graphs need regen — reviewQueue params changed) |

## Integration Test Plan

After all trees COMPLETE:
1. `make test` (records efficacy history; -p 2 enforced).
2. `make e2e-fast` — full hermetic sweep including the three new suites.
3. `make lint-ci` — analyzers + audit scripts.
4. `make graphs` then `make graphs-check` — new RPC surfaces update topology.
5. `make docs-check` — `docs/workflows/memory.md` updates in same commits.
6. Manual gate (user): `meept memory review` round-trip on a scratch daemon;
   opt-in flags verified OFF by default in a fresh `$MEEPT_HOME`.

## Coding Conventions

See SHARED-CONVENTIONS §3. Summarized: sentinel errors, no ignored errors,
pkg/id IDs, JSON5 config opt-in, lowercase UI text, no Getwd in daemon paths.

## Open Questions

Collected in `OPEN-QUESTIONS.md` — resolve before wave 1 dispatch (the two
blocking ones: persona regeneration scope in T01; whether T03 verdicts
require the daemon running or support offline CLI mode).
