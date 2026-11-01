# meept token audit — leaf 00A (branch 03, as-needed tool definitions)

Measured 2026-11-01 by a scratch Go module (`/tmp/pi-eval/token-audit/`) that
imports the production packages via `go.mod replace` — all numbers below come
from real code paths, not hand-copied prompt text.

## Method & provenance

- **Measurement program:** `/tmp/pi-eval/token-audit/main.go`
  (module `github.com/caimlas/meept/tools/token-audit-spike`, go.mod:
  `require github.com/caimlas/meept v0.0.0` + `replace github.com/caimlas/meept => /Users/caimlas/git/meept`
  + `replace github.com/caimlas/meept/sdk/go => /Users/caimlas/git/meept/sdk/go` —
  the second replace is required because the repo's `sdk/go` module is itself a
  nested module whose `replace` is ignored in dependents).
- **Token method (named explicitly):** `llm.CountToolDefinitionsTokens(defs, nil)` —
  the `nil` tokenizer resolves to `llm.HeuristicTokenizer` (chars/3, ceiled),
  which is the production default (`internal/agent/loop.go:4471`
  `toolOverhead = llm.CountToolDefinitionsTokens(tools, nil)`). A
  tiktoken `o200k_base` column is reported alongside for reference.
- **Registry:** mirrors `registerBuiltinTools` + `registerPlatformTools` +
  template/collaboration/team/employee-messaging registrations against a
  scratch `MEEPT_HOME` (initialized `memory.Manager`, real `task.Store`,
  real `scheduler.Scheduler`). Result: **72 tools** = 10 always-full cores +
  62 indexed stubs. Scheduler family arrives via the `toolprov.RegisterAll`
  provider seam exactly as the daemon wires it.
- **System prompt:** production chat-loop cold start
  (`loop.go buildSystemPromptWithContextAndSkills`): identity
  (constitution + restrictions + purpose + cache-boundary sentinel) +
  `prompts.BaselineCapabilities` + `prompts.BaselineGuidelines` +
  `prompts.EvidenceRequirements`, built through
  `agent.NewPromptBuilderFromConfig` + `BuildSystemPromptOrdered(true)`.
  Per-section byte counts are exact incremental builder diffs (each delta =
  that section's shipped bytes including separators).
- **pi anchor:** 18.2K tokens for a bare "Hello" turn (Opus 5.5) —
  `docs/research/token-efficiency-2026-10/pi-evidence.md`
  ("Third-party cold-start numbers: pi ~18.2K vs Claude Code ~29.3K",
  agents.cocode.dk benchmark).

## Table 1 — cold-start system prompt tokens by section

| section | chars | est tokens (chars/3) |
|---|---:|---:|
| constitution + restrictions + purpose + cache boundary | 535 | 179 |
| platform capabilities (`prompts.BaselineCapabilities`) | 246 | 82 |
| platform guidelines + introspection (`prompts.BaselineGuidelines`) | 1,792 | 598 |
| evidence requirements (`prompts.EvidenceRequirements`) | 1,730 | 577 |
| **total cold-start system prompt** | **4,303** | **1,435** |

Per-section estimates sum to 1,436 vs 1,435 on the full prompt text — the 1
gap is join-separator bookkeeping. Note the guidelines + introspection block
is the single biggest section (598 tok); identity is tiny (179 tok).

## Table 2 — tool-definition tokens: full vs indexed (72 tools)

Always-full set: 12 declared (`config.DefaultAlwaysFullTools`), 10 present in
the default registry (`transcript_fetch` and `json_extract` are absent —
both are config-gated optional tools), plus the implicit `tool_view`
(`registry.go` `SetSchemaMode` always adds it).

| measure | full mode | indexed (default) |
|---|---:|---:|
| tool count | 72 | 72 |
| serialized JSON bytes | 48,592 | 25,834 |
| est tokens (heuristic chars/3, production default) | 13,421 | 7,800 |
| est tokens (tiktoken o200k_base) | 9,055 | 5,386 |
| delta (indexed saves vs full) | **5,621 tok (42%)** | — |

Top-10 largest stubs (indexed-mode per-tool est tokens; full cost in parens):

| # | tool | indexed (heuristic) | full (heuristic) |
|---|---|---:|---:|
| 1 | skills_patch | 137 | 278 |
| 2 | resolve | 136 | 239 |
| 3 | request_review | 134 | 296 |
| 4 | purge_auto_claims | 133 | 205 |
| 5 | team_preset_create | 133 | 235 |
| 6 | memory_retain | 132 | 248 |
| 7 | template_invoke | 128 | 258 |
| 8 | spreadsheet_write | 127 | 388 |
| 9 | mark_superseded | 127 | 187 |
| 10 | git_overview | 123 | 175 |

Stub subtotal (62 stubs, indexed): **6,112 tok**. Always-full core subtotal
(10 tools, full): **1,688 tok**. Meta-tool costs: `tool_view` = **109 tok**
(registered today); `tool_search` = **168 tok** (candidate estimate — see
deviation note; the tool does not exist yet, measured by constructing the
branch-03 schema sketch through the production `ToolDefinition.CountTokens`).

## Table 3 — bare 'hello' turn input total vs pi anchor

Cold start, indexed default (schema_mode "" = indexed, the production default):

| component | est tokens |
|---|---:|
| system prompt (production sections, Table 1) | 1,435 |
| user message "hello" | 2 |
| tool definitions (indexed, default) | 7,800 |
| messages-array envelope overhead (roles/keys, measured) | 77 |
| **TOTAL bare-turn input (indexed, default)** | **9,314** |
| TOTAL bare-turn input (full mode) | 14,935 |

**pi anchor: 18.2K tok** cold-start "Hello"
(`docs/research/token-efficiency-2026-10/pi-evidence.md`, agents.cocode.dk,
Opus 5.5). meept indexed: **9,314 tok (0.51× pi)**; meept full: 14,935 tok
(0.82× pi). Caveat: the pi number is provider-reported on Opus; meept's are
heuristic estimates — treat the ratio as indicative, not exact. Tool
definitions dominate the turn (84% of the indexed total) — that is the
branch-03 target, not the prompt.

## Table 4 — candidate-shape savings (shapes from `../orchestrator.md`)

Baseline for all shapes = today's indexed default: **7,800 tok (72 tools)**.
Inputs: core full subtotal 1,688 tok; avg full cost of a stub 189 tok
(mean of 62 stubs); avg full cost of a core 168 tok; meta cost =
tool_search 168 + tool_view 109 = 277 tok; category digest ~150 tok
pessimistic bound (orchestrator: ~10 lines).

| shape | baseline | new cost | savings | arithmetic |
|---|---:|---:|---:|---|
| A (trim stubs to 1 line) | 7,800 | 7,800 | 0 | measured: stubs are ALREADY one-line ("desc use tool_view{name}."); nothing left to trim — A is the zero-risk baseline, not a lever |
| B (search-not-enumerate) | 7,800 | 2,115 | 5,685 | 10 cores full (1,688) + tool_search(168) + tool_view(109) + digest(~150); drop 62 stubs (6,112 tok) |
| C (predicted subset, no fallback) | 7,800 | 3,578 | 4,222 | cores full (1,688) + N=10 predicted full (10 × 189 avg stub full) |
| D (hybrid predicted + meta fallback) | 7,800 | 6,690 | 1,110 | cores full (1,688) + N=25 predicted full (25 × 189 = 4,725) + meta (277) |
| D sensitivity N=50 | 7,800 | 11,415 | −3,615 | cores (1,688) + 50 × 189 (9,450) + meta (277) — cost EXCEEDS the indexed baseline |

Notes for 00D budget sizing:
- The D break-even is between N=30 and N=31: N=30 → 7,635 tok (saves 165);
  N=31 → 7,824 tok (−24, worse than indexed). D beats today's indexed
  default only while N ≤ 30; B (2,115) is always cheaper and pays its cost
  in tool_search/tool_view round-trips instead.
- **Cache-stability trade (the branch's central tension):** B churns the
  tool prefix at every episode boundary; D with append-only promotions
  (StabilizeToolPrefix, loop.go:4442–4448) keeps prefixes byte-identical
  mid-episode.
- Co-occurrence evidence (leaf goal #4): skipped — no per-tool usage
  telemetry exists in-repo to derive it cheaply; `metrics.db llm_calls`
  records calls but tool-call args are not retained. Flagged for 00D.

## Deviations & simplifications (all measured, none hidden)

1. **Evidence section:** the production loop uses the unexported
   `evidenceSection` const (loop.go:89, 1,542 ch) while this audit uses the
   exported `prompts.EvidenceRequirements` (1,704 ch) — the audit prompt is
   ~54 est-tok *heavier* than production. Direction is conservative for the
   shapes table (savings are computed off the tools payload, not the prompt).
2. **tool_search does not exist yet** (branch-03 addition). Its 168-tok cost
   is a schema sketch counted through production `ToolDefinition.CountTokens`
   (2 params: query string + limit integer, 3-line description). If 00D's
   schema is larger, B/D costs shift up.
3. **Registry membership:** github/hermes/acp tool families are provider-seam
   and unbound under default config (no token source / disabled) → absent,
   matching a default-config daemon. A user install with GitHub enabled
   adds 8 github_* tools (~+1,500 est-tok indexed; the github family is
   among the largest stubs). Browser/transcript/knowledge/MCP/template-loaded
   families are all config-off by default → also absent here.
4. **Cold-start floor:** no user preferences, memory snapshot, skills, or
   project/AGENTS.md context is injected — matching the bare-Hello semantics
   of the pi anchor. Real first-turn cost in a project session is higher
   (project context + skills listing add sections the pi turn also lacks;
   not measured here).
5. **Heuristic column is the production one** (chars/3, what loop.go:4471
   uses for the iteration budget). tiktoken o200k_base is the cross-check
   column; ratios between columns are stable, absolute provider counts will
   differ.
6. **Repo untouched:** all writes went to `/tmp/pi-eval/`; the repo had
   pre-existing uncommitted changes (21 files, e.g. `docs/plans/
   github-intake-factory/master.md`) that predate this work — no commit
   made, no new files in the repo.

## Reproduction

```
cd /tmp/pi-eval/token-audit && go run .   # prints all four tables
```
Raw output captured at `/tmp/pi-eval/token-audit-raw.txt`.
Scratch runtime artifacts (memory graph.db, tasks.db, employees.db,
scheduler data) land in a per-run `os.MkdirTemp` home and are removed on exit.
