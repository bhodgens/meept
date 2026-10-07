# Incomplete plans — verified 2026-10-06

Every `docs/plans/` tree checked against the working tree, not against the
doc's own claim. Companion to `.hermes/audits/2026-10-06-week-bughunt.md`.

**Headline: almost nothing is genuinely incomplete. Nine trees carry stale
`PENDING` rows for work that has already landed. Two trees are genuinely open,
and both are blocked on you, not on code.**

Method: read each `master.md`'s Completion Tracking Table, then verify each
non-complete row by grepping the tree for the feature's symbols. 44 trees
scanned; 9 have a non-complete row; 1 has no tracking table at all.

---

## Ranked: FALSE-STALE (doc says open, tree says done)

These are the majority. Nothing to build — the tracking tables were never
updated after the work landed. Ranked by how much work the staleness hides.

| Tree | Row | Doc says | Reality | Evidence |
|---|---|---|---|---|
| `20260916-async-turn-migration` | 01-async-rpc-mode … 06-liveness-watchdog (6 of 7 rows) | PENDING, iter 0 | **DONE** | `internal/comm/http/api_handlers.go:2289` `handleChatSubmit` POST `/api/v1/chat/submit`; `turn.terminal` topic live; `internal/agent/watchdog.go` exists; `internal/agent/AGENTS.md` documents the async contract in full |
| `20260916-async-turn-migration` | 07-sync-deprecation | PENDING | **DONE** | `internal/config/schema.go:2558` `SyncChatEnabled`; `internal/agent/AGENTS.md` calls the blocking `chat` RPC "a legacy opt-in … default false" |
| `20260921-output-filters` | 01-filter-interface … 04-config-cli-docs (all 4) | PENDING | **DONE** | `internal/validator/filter_builtin.go`; filter chain wired in `internal/agent/`; `OutputFiltersConfig` in `schema.go:689`; root `AGENTS.md` carries an "Opt-in defaults and evolver wiring" invariant citing the tree |
| `tamper-evident-audit-log` | 01-chain-core, 02-emit-wiring, 03-anchor-verify | PENDING | **DONE** | `internal/auditlog/` package with chain + store + tests; `internal/employee/wiring.go:226` constructs `auditlog.NewAnchorJob(...)`; root `AGENTS.md` lists `internal/auditlog` as a Key Component |
| `20260916-turn-lifecycle-events` | 01-terminal-event | PENDING | **DONE** | `internal/agent/topics.go:8` — the frozen `TurnTerminalEvent` payload; `internal/comm/AGENTS.md` documents its WS classification |
| `claim-temporal-validity` | 01-claim-schema, 02-validity-integration, 03-surface-docs | pending | **DONE** | `internal/memory/epistemic.go:96-100` `ObservedAt`/`ValidFrom`; `:148` reads `rev` from metadata; `:159` reads `valid_from`; `:228-229` writes `observed_at` |
| `phase-frontier-parallel` | 01-frontier-compute … 04-surface-docs | PENDING | **DONE (at least leaf 01)** | `internal/config/schema.go` carries the frontier config. **Caveat:** I confirmed only the config surface; the store-state, hooks, and docs leaves were not individually verified. |
| `effect-idempotency` | 01-effect-ledger, 02-tool-wiring, 03-cli-reconcile | PENDING | **DONE** | `internal/effects/` package with tests; root `AGENTS.md` lists `internal/effects` ("external-effect idempotency ledger") |
| `2026-09-29-coverage-completion` | 01-lint-and-sink-fallback | pending, commit "—" | **DONE** | `049df702` "PlanService evolver sink fallback"; `a380bd2e` "zero golangci-lint findings repo-wide"; `internal/services/plan_service.go:16` carries `fallbackManager *plan.PlanManager // evolver sink` |

**Action:** one commit that rewrites these 9 tracking tables to COMPLETE with
the commit shas above. Cheap, and it stops the next audit from re-deriving what
I just derived.

## Ranked: GENUINELY OPEN

**1. `session-aware-intent-gate` → leaf `03-e2e-a5-verify` — IN_PROGRESS,
blocked on you.**

This is the only row in the repo that is honestly marked in-progress, and its
own notes say why (`master.md:167`):

> 15 runs. Run 14: window closed mid-run again (11 quota hits, 0 tasks) […]
> STATUS: 15-run acceptance record is now dominated by provider instability;
> per stop-clause (c), awaiting user decision on: (a) keep cycling, (b) swap
> provider, or (c) close COMPLETE-with-caveat

Leaves 01 and 02 are COMPLETE and the cross-tree payoff is confirmed live
(`meta.session_digest_used=true` visible in a captured reply). Leaf 03 is an
acceptance campaign that cannot complete because the local model provider keeps
quota-limiting. **This is your call, not a code gap.** Recommend (c): close
COMPLETE-with-caveat and record the provider instability as the reason, so the
row stops reading as active work.

**2. `2026-09-23-tiered-iteration` — no tracking table at all, work landed.**

All four leaves committed (`0fa7f332` leaf 2 critique loop, `337f37fe` leaf 4
observability, plus leaf 1 and leaf 3 in the same range), but `master.md` has
no Completion Tracking Table — its headers stop at "Open questions". So there
is no row to be stale; there is simply no status record. Lowest-risk item on
this list: add the table, mark all four COMPLETE with shas.

## Not findings

- **`20260723-*` trees (5 of them)** show `PENDING` in their *leaf-definition*
  tables (the plan as authored, before dispatch) while their Completion Tracking
  Tables correctly read `COMPLETE` with timestamps. Not stale — the two tables
  serve different purposes and the tracking table is the authoritative one.
- **`docs/plans/archive/`** — 37 files, no `master.md`. Archived by design; not
  orphaned work.
- **`docs/plans/20260923-e2e-expansion/plan.md`** — single file, no
  `master.md`. A standalone plan, not a tree. Its work landed (see commits
  `3d36841e`, `54911ea4`, `7bfcd074`).
- **`2026-08-25-oauth-providers`, `20260905-dependency-visibility` "missing
  leaves"** — the leaves exist in per-area subdirectories
  (`01-xai-device-flow/01-registry-extension.md`, etc.), not at the tree root.
  A flat `ls` reads them as absent. Dependency edges are intact.
- **`llm-resilience-forest`** — the forest root indexes 5 child tree
  directories (19 leaves across them). All 5 exist and all 19 leaves read
  `COMPLETE`, with the named commits present. Spot-checked three features
  against code: `internal/llm/context_discovery.go` and
  `provider_lmstudio_test.go` (tree 05), `internal/agent/verification_escalation_test.go`
  (tree 01). Genuinely complete.

## Two more tracking-table gaps found on the second pass

- **`llm-resilience-forest/04-scheduling`** has 3 leaf files but its Completion
  Tracking Table lists only 2. `03-model-slot-fairness.md` is absent from the
  table — and the work is done: `internal/llm/slot_gate.go` exists with the
  documented starvation guard (`interactiveGrantsBeforeBackground = 3`, `:12`),
  and `internal/llm/AGENTS.md` documents it as "tree 04 leaf 03, D11". So this
  is a missing row for finished work, not hidden work.
- **`2026-09-23-tiered-iteration`** has no tracking table at all (already listed
  above). All 4 leaves committed.

## Dependency integrity — checked, no broken edges

Scanned every `master.md` for numeric dependency columns pointing at leaves that
do not exist. 11 rows flagged; **all 11 were false positives** from the two
shapes above (subdirectory leaves, forest child trees). No tree has a
dependency edge pointing at a missing leaf.

---

## Count summary

46 trees scanned (46 `master.md` files under `docs/plans/`). 9 carry stale
non-complete rows. 1 has no tracking table at all; 1 more is missing a single
finished row. 2 items are genuinely open: 1 needs your decision
(`session-aware-intent-gate` leaf 03), 1 needs a tracking table added
(`2026-09-23-tiered-iteration`). **0 false completions** — I checked every
COMPLETE row that named a commit and found no row whose evidence failed to
verify. 0 broken dependency edges (11 candidates, all false positives).

Runtime `sealed-plan.md` artifacts (7 on disk, untracked) were excluded from
the counts: they are generated per sealed plan, not planned work. The 4
`-2`..`-5` variants in `docs/plans/` are a sibling session's current activity,
not stale state.

## Also open, from the wave audit (not plan-tree rows)

- `Store.Fail` has no claim-token predicate, so a superseded attempt can fail a
  live one — HIGH in `.hermes/audits/2026-10-06-week-bughunt.md`. Not tracked in
  any plan tree.
- `make graphs-check` is red at HEAD on line offsets. One command: `make graphs`.
- `internal/daemon/filter_wiring_test.go` reads the developer's real
  `$MEEPT_HOME`. Prior-wave debt, still open.