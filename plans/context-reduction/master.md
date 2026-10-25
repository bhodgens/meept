# Context Reduction Implementation Plan — P2, P4, P5, P7, P10

> **For Hermes:** Execute with the hierarchical-planning skill. The orchestrating
> agent dispatches each leaf via `delegate_task`, reviews IN-SESSION (main model,
> never a delegated reviewer), commits only after review passes, and updates the
> tracking table after every transition.

**Goal:** Cut context tokens and GPU prefill in meept by (1) making the response
cache actually hit and observable (P2), (2) aligning provider prompt caches (P4),
(3) fixing memory budgeting + making the firewall trust contract real (P5),
(4) restorable tool-output spilling with 30-day retention (P7), and (5) IndexRAG
bridging facts in memory, eval-gated (P10).

**Research basis:** `docs/research/01-context-reduction-landscape-and-proposals.md`
(+ 02/03/04 companions). All file:line anchors below were source-verified 2026-10-12
by the leg-5 audit; re-verify with search_files before editing if the tree sits
unexecuted for weeks.

---

## Design Review — component diagram (attach points)

```
agent loop (internal/agent/loop.go)
  |
  |-- [L05] buildMemoryContext budget: WHOLE-ITEM drops to fit
  |         MaxMemoryContextTokens (conversation.go:33), replacing the
  |         proportional mid-string cut at loop.go:7163-7168
  |
  |-- [L06] tool results enter history wrapped in <<<TOOL_OUTPUT:id>>> ... <<<END
  |         (constants already exist: context_firewall.go:15-26; emitter missing)
  |
  v
llm.Client (internal/llm/client.go)
  |
  |-- [L01] cache lookup/put keyed by BuildPrefixAwareKey
  |         (cache_key_builder.go:271-279, currently test-only; live path
  |         hashes full message list at client.go:557-561)
  |-- [L04] OpenAI-compatible payload: prompt_cache_key field (session-scoped);
  |         tools sorted by name before serialization (prefix-cache hygiene)
  |
  |-- [L07] pruneToolOutputs (context_compactor.go:766-853): spill full output
  |         to $MEEPT_HOME/spill/<session>/<hash>.txt, insert pointer line
  |
  v
providers
  |-- [L03] anthropic.go:1183-1191: use BuildSystemPromptBlocks (prompt_cache.go:60)
  |         end-to-end instead of re-splitting systemPrompt on "\n\n" (:1184);
  |         cache_control on the Static block (and Session block: decide — see leaf)
  |
  |-- [L02] daemon/components.go:756-781: pass the real metrics store into
  |         NewTokenCacheCoordinatorWithMetrics (currently nil at :770)

daemon background
  |-- [L08] spill retention sweep: delete spill files older than
  |         spill.retention_days (default 30)

memory subsystem (internal/memory/) — branch 09-bridges, offline only
  distill.go --> [B01] bridge store (BridgeShard in vector/shard_manager.go)
             --> [B02] Consolidator bridge-generation pass (df in [2, tau])
             --> [B03] entailment verify + provenance cascade-invalidation
  recall path --> [B04] balanced selection (cap bridges at k_b) before prompt
  eval/internal --> [B05] bridges-on/off cross-session recall eval  (GATE)
  cmd/ + docs  --> [B06] CLI/TUI visibility, e2e manifest entry, docs
```

Dispatch order: L01-L08 are independent of each other (parallel up to the
concurrency cap). Branch 09 is sequential B01 -> (B02, B04) -> B03 -> B05/B06.
B05's report gates any thought of enabling bridges by default (default stays
false regardless, per opt-in-defaults).

## Design Review — knob table

| Knob / behavior | Default | What changes | Complexity |
|---|---|---|---|
| (behavior) prefix-aware response-cache keys | active only when `[llm.cache] enabled` | identical/dynamic-stable re-asks can hit L1/L2; no new key | S |
| (behavior) OpenAI `prompt_cache_key` sent | always, when session id known | one extra wire field for OpenAI; ignored by llama-server | S |
| (behavior) tools sorted by name pre-serialization | always | byte-stable tool list; changes wire bytes only on previously-unstable orders | S |
| (behavior) memory budget = whole-item drops | always | no more mangled half-memory; item ranking order decides what survives | S |
| (behavior) `<<<TOOL_OUTPUT:id>>>` markers | always | +10-40 tokens/turn; makes firewall trust contract real; gives summarizers refold points | S-M |
| (behavior) Anthropic cache breakpoints via structured blocks | follows existing `prompt_cache` config (default on) | fixes blank-line re-partition bug; static-first byte-stable system array | S-M |
| (new) `compaction.spill_enabled` | **false** (opt-in) | pruned tool outputs land in `$MEEPT_HOME/spill/` with a path pointer | M |
| (new) `spill.retention_days` | **30** (user-set requirement) | sweep deletes spill files older than N days | S |
| (new) `memory.bridges.enabled` | **false** (opt-in) | bridge generation + recall participation | M |
| (new) `memory.bridges.kb` | 2 | max bridging facts per recall (paper App. C optimum 2-3) | S |
| (new) `memory.bridges.bridge_df_max` | 10 | tau: entities with df above this are too generic to bridge | S |
| (new) `memory.bridges.verify` | **true** | entailment gate drops unverifiable bridges (ship-on quality default) | M |

Defaults that change existing behavior when shipped: the five no-knob behavior
rows above. All config-keyed features default OFF except `bridges.verify`
(quality gate — misfires get fixed, per owner's ship-on directive for gates)
and `spill.retention_days` (meaningless until spill_enabled=true).

## Interface Contracts (root-level, binding on all children)

1. **Cache key shape (L01):** `CacheKey.PromptHash` for live lookups becomes
   `BuildPrefixAwareKey` output: `staticHash[:16] + ":" + dynamicHash[:16] + ":" +
   messagesHash[:16]` (cache_key_builder.go:271-279). L2 SQLite keys derive from
   it unchanged (sha256 over model+promptHash+files). Old L2 rows simply miss
   once and age out via TTL — no migration.
2. **Marker contract (L06):** tool results are stored wrapped:
   `<<<TOOL_OUTPUT:<tool_call_id>>>` ... `<<<END_TOOL_OUTPUT>>>`. The context
   firewall's existing `ContextSummaryStart/End` wrapping is unchanged. Summarizer
   prompts already reference these marker names (context_firewall.go:50-54).
3. **Spill pointer contract (L07):** pointer line format:
   `[Output spilled to <absolute path> — re-read with the file tool if needed (N tokens)]`.
   Spill file path: `$MEEPT_HOME/spill/<session_id>/<sha256(content)[:16]>.txt`.
   Spill write failure MUST fall back to today's `[Output truncated]` behavior,
   never fail the turn.
4. **Bridge record contract (branch 09):** stored in a new `BridgeShard`
   (`vector.ShardTypeBridge`); metadata: `unit_type: "bridge"`,
   `evidence_ids: []string` (source memory IDs), `created_at`, `verified: bool`.
   Text = the bridging fact sentence(s). Recall treats any unit without
   `unit_type` as an AKU (backward compatible).
5. **Config keys:** exactly the eight new keys in the knob table, added to
   `internal/config/schema.go` with the defaults shown. JSON5 tags lowercase.
6. **Conventions for all leaves:** Do NOT commit (orchestrator commits). Do NOT
   feed `read_file` output into `write_file` (line-number corruption). Use
   `search_files`/`terminal cat` for exploration. Tests: `go test -p 2
   ./internal/... -run <TestName>` (macOS port-exhaustion rule). gofmt clean.
7. **AGENTS.md rule:** leaves that change config schema or user-facing behavior
   note it in their report; the orchestrator updates the root AGENTS.md summary +
   relevant docs in the integration commit (per repo maintenance rule).

## Child Index

| # | Document | Proposal | Depends on | Status |
|---|---|---|---|---|
| L01 | `01-prefix-cache-key.md` | P2 | — | NOT STARTED |
| L02 | `02-cache-metrics.md` | P2 | — | NOT STARTED |
| L03 | `03-anthropic-cache-blocks.md` | P4 | — | NOT STARTED |
| L04 | `04-openai-cache-and-tool-order.md` | P4 | — | NOT STARTED |
| L05 | `05-memory-item-budget.md` | P5 | — | NOT STARTED |
| L06 | `06-untrusted-markers.md` | P5 | — | NOT STARTED |
| L07 | `07-spill-service.md` | P7 | — | NOT STARTED |
| L08 | `08-spill-retention.md` | P7 | L07 | NOT STARTED |
| B01 | `09-bridges/01-bridge-store.md` | P10 | — | NOT STARTED |
| B02 | `09-bridges/02-bridge-generation.md` | P10 | B01 | NOT STARTED |
| B03 | `09-bridges/03-verify-invalidate.md` | P10 | B02 | NOT STARTED |
| B04 | `09-bridges/04-balanced-recall.md` | P10 | B01 | NOT STARTED |
| B05 | `09-bridges/05-bridge-eval.md` | P10 | B02-B04 | NOT STARTED |
| B06 | `09-bridges/06-wiring-docs.md` | P10 | B04 | NOT STARTED |

## Dispatch Protocol

Per child: (1) dispatch implementation agent via `delegate_task` with the leaf
document content as `context`, plus root contracts 6-7 verbatim; (2) review
in-session — read the diffs, run the leaf's verify commands, check for debug
artifacts/TODOs; (3) re-dispatch with specific feedback on gaps (max 3 rounds);
(4) commit with `git add <exact leaf files>` + descriptive message; (5) update
the tracking table. Batch independent leaves up to
`delegation.max_concurrent_children`. L08 waits for L07; branch 09 respects its
internal order.

## Review Checklist (applied to every leaf)

- [ ] Verify commands from the leaf pass (`go test -p 2 ...`, `go build ./...`, `gofmt -l` empty)
- [ ] Interface contract implemented exactly (key shapes, marker text, pointer format)
- [ ] No debug prints, no TODOs, no placeholder values, no commented-out code
- [ ] No `grep -rcE '^\s+[0-9]+\|'` hits in touched files (line-number corruption)
- [ ] Behavior defaults match the knob table exactly
- [ ] New config keys appear in schema.go with JSON5 tags + defaults

## Completion Tracking Table

| Document | Status | Reviewed | Committed |
|---|---|---|---|
| 01-prefix-cache-key.md | COMPLETE | REVIEWED | bcf1e87c |
| 02-cache-metrics.md | COMPLETE | REVIEWED | bcf1e87c |
| 03-anthropic-cache-blocks.md | COMPLETE | REVIEWED | 86497e12 |
| 04-openai-cache-and-tool-order.md | COMPLETE | REVIEWED | 86497e12 |
| 05-memory-item-budget.md | COMPLETE | REVIEWED | d1d6a415 |
| 06-untrusted-markers.md | COMPLETE | REVIEWED | d1d6a415 |
| 07-spill-service.md | COMPLETE | REVIEWED | 4d24e258 |
| 08-spill-retention.md | COMPLETE | REVIEWED | ddcded2a |
| 09-bridges/01-bridge-store.md | COMPLETE | REVIEWED | 6104cfdf |
| 09-bridges/02-bridge-generation.md | COMPLETE | REVIEWED | 3fdd7e0b |
| 09-bridges/03-verify-invalidate.md | COMPLETE | REVIEWED | 03dfcf3a |
| 09-bridges/04-balanced-recall.md | COMPLETE | REVIEWED | 3fdd7e0b |
| 09-bridges/05-bridge-eval.md | COMPLETE | REVIEWED | cb5f8b31 |
| 09-bridges/06-wiring-docs.md | COMPLETE | REVIEWED | 370a1057 |

## Open Questions (defaults chosen; owner can override before/at dispatch)

1. L03 trailing-turn breakpoint: implemented in-leaf if trivial, else follow-up. Default: attempt in-leaf.
2. B05 question count 20-40 and pass thresholds: suggested values, owner-tunable at review.
3. GUI parity for bridge listings: deferred with documented deviation (B06). Default: TUI/CLI first.
4. spill dir session_id sanitization: leaf L07 must define the allow-set; default `[a-zA-Z0-9-_]`.

## Integration Test Plan

INTEGRATION RESULT (2026-10-13): full sweep across all 10 touched packages
GREEN (llm 36.7s, agent 74.9s, daemon 7.9s, config, security, memory, vector,
eval, services, cmd/meept). One transient internal/memory FAIL during the
parallel session's mid-edit window; clean on re-run (x2). Docs + graphs gates
green (B06). PLAN COMPLETE: 14/14 leaves reviewed + committed.

## Integration Test Plan

After all leaves: `go build ./... && go test -p 2 ./...` (short mode full sweep).
Then the e2e affected sweep for touched areas: `make e2e-affected`. Manual probe
for L01/L04: run daemon against local llama-server, issue two identical
follow-up asks, confirm second hits L1 (`meept` metrics or debug log), and
confirm llama-server log shows prefix reuse (n_past << total tokens on turn 2).
For branch 09, B05's eval report is the integration evidence; attach it to the
final commit message.

## Coding Conventions

Standard repo Go style (see root AGENTS.md + docs/agents/coding-practices.md):
no ignored errors, no mutex across I/O, two-value map assertions, typed-nil
guards on Set*, no `time.Now().UnixNano()`/`math/rand` for IDs (use
`pkg/id.Generate()`). UI text lowercase if any surfaces. New user-facing
surfaces need CLI/TUI parity (B06 handles this for bridges).

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | L01 — Prefix-Aware Response-Cache Keys (P2) | 01-prefix-cache-key.md | (pending) | (pending) | PENDING |
| F2 | L02 — Response-Cache Metrics Store Wiring (P2) | 02-cache-metrics.md | (pending) | (pending) | PENDING |
| F3 | L03 — Anthropic Cache Breakpoints via Structured Blocks (P4) | 03-anthropic-cache-blocks.md | (pending) | (pending) | PENDING |
| F4 | L04 — OpenAI prompt_cache_key + Deterministic Tool Order (P4) | 04-openai-cache-and-tool-order.md | (pending) | (pending) | PENDING |
| F5 | L05 — Whole-Item Memory Budgeting (P5) | 05-memory-item-budget.md | (pending) | (pending) | PENDING |
| F6 | L06 — Emit Untrusted-Content Markers at the Tool-Result Seam (P5) | 06-untrusted-markers.md | (pending) | (pending) | PENDING |
| F7 | L07 — Tool-Output Spill Service (P7) | 07-spill-service.md | (pending) | (pending) | PENDING |
| F8 | L08 — Spill Retention Sweep (P7) | 08-spill-retention.md | (pending) | (pending) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)

### Control-agent survey 2026-10-25 — verdict: VALIDATED (tree-level)

- **Survey verdict:** TESTED
- **E2E evidence (manifest path_map → suites):** contin, memory, bridge, daemon, runtim
- **Unit packages with tests:** internal/agent, internal/compress, internal/context, internal/eval, internal/llm, internal/memory
- **Sample verified tests:** internal/llm:TestDecisionClient_Unconfigured
- **Baseline:** `make test` 2026-10-25: 10,873 recorded results; the 26 initial failures were ephemeral-port exhaustion (`can't assign requested address`, AGENTS.md known issue) — `go test -count=1 -p 1 ./internal/tools/builtin/ ./internal/tui/` GREEN on isolated re-run.

Row-level statuses above stay as seeded; this tree-level verdict records that every
planned feature has located test evidence (e2e suite mapping and/or package tests).
Row-by-row test-name mapping happens at next dispatch touch of this tree.
