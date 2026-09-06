# master.md — Tokscale Ingest (meept token metrics for tokscale)

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 2 leaf documents under this node
- **Scope:** Make meept's per-call LLM token records complete enough for the tokscale CLI to ingest, then add tokscale-side ingestion of meept's `~/.meept/metrics.db`.

## Goal

tokscale reads clients' local token records and aggregates model/client/session
usage. Meept already persists per-call token counts to `~/.meept/metrics.db`
(table `llm_calls`: timestamp, provider, model_id, agent_id, tokens_sent,
tokens_received, tokens_cached — `internal/metrics/store.go:245`), but four
gaps block a good tokscale ingest:

1. **No session_id in `llm_calls`.** Tokscale's session view and
   `client,session,model` group-by need it. The session id already reaches
   every call site via `chatOptions.sessionID`
   (`internal/llm/client.go:921`) — it is simply never persisted.
2. **No reasoning tokens.** `llm.TokenUsage` (`internal/llm/models.go:168`)
   has no reasoning field; Anthropic output_tokens (already parsed at
   `internal/llm/anthropic.go:1634`) and OpenAI-style
   `completion_tokens_details.reasoning_tokens` are dropped.
3. **Cache-write tokens dropped.** Anthropic `cache_creation_input_tokens` is
   parsed (`internal/llm/anthropic.go:879`) then only logged
   (`internal/llm/anthropic.go:1354`), never stored.
4. **30-day retention purge deletes `llm_calls`**
   (`internal/metrics/store.go:513`, `DefaultRetentionDays = 30`). Tokscale
   aggregates year-scale history; meept records would vanish under the purge.

Half of the work lands in meept (schema + capture). The other half — the
tokscale client registry entry and `sessions/meept.rs` parser — is planned in
the tokscale repo, NOT here. This tree fixes meept only. Leaf 02 pins the
read-side contract (see Interface Contracts) so the tokscale parser can be
authored against a frozen schema.

## Architecture

Meept's metrics stack: `internal/llm` clients (generic Client, AnthropicClient,
CodexClient) call `recordUsageStore(...)` after each provider response, which
writes one row to `llm_calls` via `appmetrics.Store.RecordLLMCall`
(`internal/metrics/store.go:610`). The store owns schema creation and
migration (existing ALTER TABLE tolerance pattern at `store.go:347-356`) and a
daily retention purge loop.

Plan shape: one leaf widens the data path (schema migration + struct fields +
capture at all four client call sites), one leaf makes history durable and
proves the pipeline end-to-end (retention policy + an integration test that
writes a row and reads it back through the exact SQL the tokscale parser will
run). Leaves touch overlapping files in `internal/metrics` and
`internal/llm` — **dispatch sequentially, Leaf 01 first** (see Dispatch
Protocol). A third leaf (tokscale parser) lives in the tokscale repo and is
out of scope for this tree.

## Interface Contracts (frozen)

All contracts below are verified against current source, 2026-09-06.

### Contract A: `llm.TokenUsage` gains two fields

```go
// internal/llm/models.go — SPEC (models.go:168-173, current):
// type TokenUsage struct {
//     PromptTokens     int `json:"prompt_tokens"`
//     CompletionTokens int `json:"completion_tokens"`
//     TotalTokens      int `json:"total_tokens"`
//     CachedTokens     int `json:"cached_tokens,omitempty"`
// }
type TokenUsage struct {
    PromptTokens         int `json:"prompt_tokens"`
    CompletionTokens     int `json:"completion_tokens"`
    TotalTokens          int `json:"total_tokens"`
    CachedTokens         int `json:"cached_tokens,omitempty"`
    ReasoningTokens      int `json:"reasoning_tokens,omitempty"`
    CacheCreationTokens  int `json:"cache_creation_tokens,omitempty"`
}
```

Owner: Leaf 01. Consumers: Leaf 01 (all `recordUsageStore` sites),
Leaf 02 (retention untouched by these fields).

### Contract B: `appmetrics.LLMCallRecord` and `llm_calls` schema

```go
// internal/metrics/store.go — SPEC (store.go:592-604, current LLMCallRecord
// gains three fields):
type LLMCallRecord struct {
    Timestamp          time.Time
    Provider           string
    ModelID            string
    AgentID            string
    SessionID          string // NEW — conversation/session id ("" unknown)
    TokensSent         int    // prompt tokens
    TokensRecv         int    // completion tokens
    TokensCached       int    // prompt cache reads (0 when unreported)
    ReasoningTokens    int    // NEW — reasoning/thinking tokens (0 when unreported)
    CacheCreationTokens int   // NEW — cache writes (0 when unreported)
    IsError            bool
    ErrorMessage       string
    LatencyMs          int64
    DurationMs         float64
}
```

```sql
-- llm_calls gains three columns, appended via ALTER TABLE (existing
-- migration pattern at store.go:347-356):
ALTER TABLE llm_calls ADD COLUMN session_id          TEXT    NOT NULL DEFAULT '';
ALTER TABLE llm_calls ADD COLUMN reasoning_tokens    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE llm_calls ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0;
-- New index:
CREATE INDEX IF NOT EXISTS idx_llm_calls_session_ts ON llm_calls(session_id, timestamp DESC);
-- Fresh installs get the same columns inline in CREATE TABLE llm_calls.
```

Owner: Leaf 01. Consumers: Leaf 02 (reads schema in integration test),
tokscale `sessions/meept.rs` (reads DB read-only; column names above are the
frozen read surface).

### Contract C: retention policy for `llm_calls`

`llm_calls` is exempt from the time-based purge. The purge loop
(`internal/metrics/store.go:513`) removes `llm_calls` from `retentionTables`;
raw `llm_calls` rows persist indefinitely (bounded in practice by call rate).
Alternative (config cap, e.g. `retention.llm_calls_days`) rejected for scope:
tokscale only needs the rows to exist. Other tables keep current behavior.

Owner: Leaf 02.

### Contract D: tokscale read surface (informational — implemented in the tokscale repo)

Frozen here so a tokscale-side parser can be authored without re-deriving it:

```
DB path:      ~/.meept/metrics.db (path expansion per store.go expandPath)
Table:        llm_calls
Row per call. Projection the tokscale parser relies on:
  timestamp            TEXT  ISO-8601 UTC ('%Y-%m-%dT%H:%M:%SZ', store.go:246)
  provider             TEXT
  model_id             TEXT  -> UnifiedMessage.model_id
  agent_id             TEXT  -> UnifiedMessage.agent
  session_id           TEXT  -> UnifiedMessage.session_id  ("" allowed)
  tokens_sent          INTEGER -> TokenBreakdown.input
  tokens_received      INTEGER -> TokenBreakdown.output
  tokens_cached        INTEGER -> TokenBreakdown.cache_read
  cache_creation_tokens INTEGER -> TokenBreakdown.cache_write
  reasoning_tokens     INTEGER -> TokenBreakdown.reasoning
  error                INTEGER (error rows carry no usage; tokscale skips error=1)
  latency_ms           INTEGER -> duration_ms (best-effort)
Rows are append-only per call; duplicates have distinct ids — dedup by
id (monotonic AUTOINCREMENT), not by (timestamp, model).
Client id in tokscale registry: "meept", display "Meept",
root: HOME / ".meept", relative: "metrics.db".
```

No meept code implements Contract D; it is the pinned interface between this
tree and the future tokscale feature branch.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-schema-and-capture.md | leaf | none | ~55K | A |
| 02 | 02-retention-and-verify.md | leaf | 01 | ~40K | A (after 01) |

**Concurrency groups:** Both leaves edit overlapping files in
`internal/metrics` (store.go, store tests). Despite the same letter group,
**dispatch strictly sequentially: 01, review+commit, then 02.** Leaf 02's
integration test reads the schema Leaf 01 creates; running them concurrently
would race the migration.

## Dispatch Protocol

For each leaf, in strict order (01 fully committed before 02 dispatches):

### Phase 1: Dispatch Leaf 01

1. **Read** `01-schema-and-capture.md` and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-schema-and-capture.md"
   - Context: Full leaf document text + Contracts A and B + coding
     conventions below + relevant existing source INLINED (store.go schema
     block, models.go TokenUsage, the four recordUsageStore sites).
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files — explore with
     search_files or terminal cat. If you read a file, never feed its output
     into write_file."
   - Include: "After writing a file, do NOT read it back to verify. Write once and stop."
2. Orchestrator reviews in-session (main model reviews directly, NOT a
   delegated subagent — delegate_task children inherit delegation.model).
   Verify against Contracts A/B and the Review Checklist below.
3. Gaps → re-dispatch with specific feedback (max 3 cycles). Pass → commit
   leaf's exact files: `git add <exact paths> && git commit -m "feat(metrics): record session id, reasoning and cache-creation tokens per llm call"`. Update tracking table → REVIEWED.

### Phase 2: Dispatch Leaf 02

1. **Read** `02-retention-and-verify.md`, dispatch with the same inclusions
   as Phase 1, plus Contracts C and D and Leaf 01's committed diff summary.
2. Review in-session against Contract C/D. Pass → commit: `git add <exact paths> && git commit -m "feat(metrics): exempt llm_calls from retention purge + ingest verification test"`. Tracking table → REVIEWED.

### Phase 3: Integration Review

1. Run full package tests: `go test ./internal/metrics/... ./internal/llm/... -count=1`
   plus `go vet ./internal/metrics/... ./internal/llm/...` and
   `gofmt -l internal/metrics internal/llm` (empty output).
2. Cross-leaf checks: Contract A fields flow into Contract B columns; the
   Leaf 02 verification query returns usage for a synthetic multi-session
   fixture exactly matching Contract D's projection.
3. Line-number corruption check: `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/metrics internal/llm` returns zero.
4. Stray-artifact sweep: `git status --short` — commit everything the leaves
   touched (subagents under-report test files); no probe files remain.
5. All children → COMPLETE. Report COMPLETE (this is the root).

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues
- [ ] No debug artifacts: no print/stdout debugging, no TODOs, no placeholder values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source files

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go (module `github.com/caimlas/meept`), Go 1.2x, stdlib + sqlx + modernc.org/sqlite.
- **Naming:** exported PascalCase, unexported camelCase; no stutter (`metrics.Store`).
- **Imports:** stdlib group, then third-party, then local; goimports grouping; no unused imports.
- **Error handling:** wrap with `%w`; storage failures in metrics paths log and continue (never fatal to the caller) — see `RecordLLMCall` doc comment at store.go:606.
- **Testing:** stdlib `testing` + existing table-driven style in `internal/metrics/*_test.go`; `_test.go` alongside; use in-memory/`t.TempDir()` SQLite.
- **Formatting:** `gofmt`; run before reporting completion.
- **SQL:** column names lowercase snake_case; ALTER TABLE migrations follow the tolerate-duplicate-error pattern at store.go:347-356 (log-and-continue on "duplicate column name").
- **Comments:** doc comments on exported symbols; cite file:line for non-obvious behavior.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-schema-and-capture | COMPLETE | 1 | Reviewed in-session vs Contracts A/B: schema, ALTER migration, index, LLMCallRecord, INSERT, all 16 recordUsageStore sites carry sessionID; Anthropic maps CacheCreationInputTokens at both construction sites; generic maps completion_tokens_details.reasoning_tokens; Codex leaves ReasoningTokens 0 per frozen rule. Tests: metrics ok (+ -race), targeted llm tests pass, build/vet/gofmt clean, zero corruption. Pre-existing unrelated failure: TestConfigLoads (uncommitted models.json5 from parallel session). Committed ed23a7ad (--no-verify: hook choked on sibling in-flight transcript_fetch_test.go, not our files; per user standing rule). Deviations accepted: test-only sidecar read connection (appmetrics.Store.db unexported); ChatResponse.Usage gained CompletionTokensDetails decode (required to capture reasoning tokens); 3 pre-existing anonymous struct literals in client_test.go redeclared type — build fix only. |
| 02-retention-and-verify | COMPLETE | 1 | Reviewed in-session vs Contracts C/D: llm_calls removed from retentionTables (aggregateHourly, store.go:537) with WHY comment citing Contract D; WHY comment placement inside aggregateHourly is the only deviation (leaf said "at the definition" — same thing, the slice IS defined there). Control assertion via dispatch_log present. Contract D projection test (internal/metrics/tokscale_ingest_test.go) pins column-for-column; passed first try as anticipated (contract pin). Gates: metrics ok plain + -race, vet clean, gofmt empty, zero corruption. Committed 18432bb2 (--no-verify: sibling transcript_fetch churn still in tree). |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` — clean compile.
2. `go test ./internal/metrics/... ./internal/llm/... -count=1` — all pass.
   Include `-race` for the store tests (`-race ./internal/metrics/...`).
3. `go vet ./internal/metrics/... ./internal/llm/...` — clean.
4. Manual smoke (optional, no fixture committed): open
   `~/.meept/metrics.db` read-only and run Contract D's projection; column
   existence proves the migration ran against a real pre-existing DB.
5. Contract D round-trip: Leaf 02's test writes synthetic rows for ≥2
   sessions × ≥2 models and asserts the exact aggregated projection the
   tokscale parser will issue (SUM per session/model with error=1 excluded).

## Structural Completeness Check (Before Dispatch)

Run after authoring, before dispatch:

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans/20260906-tokscale-ingest --strict-leaves
```

Required on every orchestrator: `## Dispatch Protocol`, `## Interface
Contracts`, `## Review Checklist`, `## Coding Conventions`, `## Completion
Tracking Table`, `## Integration Test Plan`.

## Notes

- **Serialization is deliberate.** Both leaves touch `internal/metrics/store.go`
  and its tests. Pitfall: concurrent same-file leaves race the migration test.
  One wave, two sequential dispatches.
- **Do not widen scope to the tokscale side.** The tokscale parser
  (`ClientId::Meept`, `sessions/meept.rs`) is authored in the tokscale repo
  against Contract D. This tree's only obligation is that the schema Contract D
  describes is real on disk after both leaves land.
- **Zero-value semantics are part of the contract:** `0` reasoning /
  cache-creation means "provider did not report", not "zero occurred". Tokscale
  treats 0 as absent. Do not invent sentinel values.
- **Error rows:** `recordUsageStore` is also called with zeroed `TokenUsage`
  on failures (e.g. codex.go:339). Error rows keep error=1 and zero usage;
  do not skip recording them — tokscale's parser filters `error=1`.
- **Branch:** implement on a feature branch off main (suggest
  `feat/tokscale-ingest`). Repo currently has unrelated uncommitted changes —
  stage ONLY files this tree touches (per meept parallel-session discipline:
  check `git diff --cached` before every commit).
- **Pre-commit lint:** the repo's hooks reject unused code (U1000 class). Run
  `go vet` on touched packages before handing off; if a hook fails on
  unrelated in-flight files from the parallel session, use `--no-verify` per
  user standing rule (own tests green, own files only).
