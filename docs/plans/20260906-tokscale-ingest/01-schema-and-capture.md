# Schema + Token Capture — Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Widen the per-call token record: three new `llm_calls` columns
  (session_id, reasoning_tokens, cache_creation_tokens), two new
  `TokenUsage` fields, and capture wiring at every `recordUsageStore` site.
- **Dependencies:** none (this leaf runs first)
- **Estimated Context:** ~55K (exploration ~15K + generation ~20K + iteration ~15K + overhead ~5K)
- **Concurrency Group:** A (dispatched alone; leaf 02 waits for this leaf's commit)

## Goal

Meept records one `llm_calls` row per LLM provider call, but drops the
session id, reasoning tokens, and cache-creation tokens. Tokscale (external
aggregator) needs all three. This leaf:

1. Adds `session_id`, `reasoning_tokens`, `cache_creation_tokens` columns to
   `llm_calls` (migration + fresh-install schema + index).
2. Adds `ReasoningTokens` / `CacheCreationTokens` to `llm.TokenUsage`.
3. Threads the session id (already present on every call path as
   `chatOptions.sessionID`) into `LLMCallRecord`.
4. Captures reasoning + cache-creation tokens where providers report them
   (Anthropic messages API and streaming; OpenAI-compatible
   `completion_tokens_details.reasoning_tokens` in the generic client;
   Codex if its payloads carry usage details).

## Context

Meept is a Go daemon (`github.com/caimlas/meept`). Its metrics package
(`internal/metrics`) owns a SQLite DB at `~/.meept/metrics.db` via sqlx +
modernc.org/sqlite. LLM clients (`internal/llm`) call `Store.RecordLLMCall`
after each provider response.

Key files to understand before implementing:
- `internal/metrics/store.go` — schema (CREATE TABLE block ~line 245),
  migration tolerance (~347-356), `LLMCallRecord` (~592), `RecordLLMCall`
  (~610). The store is append-oriented; `RecordLLMCall` logs storage
  failures, never fatals.
- `internal/llm/models.go` — `TokenUsage` (~168). Central usage struct.
- `internal/llm/client.go` — generic OpenAI-compatible client;
  `chatOptions.sessionID` exists (~921); `recordUsageStore` wrapper (~1477);
  call sites ~675, ~688.
- `internal/llm/anthropic.go` — `anthropicUsage` (~877: InputTokens,
  OutputTokens, CacheCreationInputTokens, CacheReadInputTokens);
  streaming accumulation (~1579-1634); `recordUsageStore` (~274) and its
  call sites (~288, ~463-468, ~696-701, ~1334-1336, ~1493-1498).
- `internal/llm/codex.go` + `codex_sse.go` — Codex client
  `recordUsageStore` (~292) and call sites (~324, ~339, ~346, ~361).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/llm/models.go
type TokenUsage struct {
    PromptTokens        int `json:"prompt_tokens"`
    CompletionTokens    int `json:"completion_tokens"`
    TotalTokens         int `json:"total_tokens"`
    CachedTokens        int `json:"cached_tokens,omitempty"`
    ReasoningTokens     int `json:"reasoning_tokens,omitempty"`
    CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
}

// internal/metrics/store.go
type LLMCallRecord struct {
    Timestamp           time.Time
    Provider            string
    ModelID             string
    AgentID             string
    SessionID           string // "" when unknown
    TokensSent          int
    TokensRecv          int
    TokensCached        int
    ReasoningTokens     int    // 0 = provider did not report
    CacheCreationTokens int    // 0 = provider did not report
    IsError             bool
    ErrorMessage        string
    LatencyMs           int64
    DurationMs          float64
}
```

SQL schema delta (see master.md Contract B for the full ALTER statements):
three new columns on `llm_calls` — `session_id TEXT NOT NULL DEFAULT ''`,
`reasoning_tokens INTEGER NOT NULL DEFAULT 0`,
`cache_creation_tokens INTEGER NOT NULL DEFAULT 0` — applied idempotently to
existing DBs (log-and-continue on duplicate-column error, matching the
existing pattern at store.go:347-356), inline in CREATE TABLE for fresh
installs, plus `CREATE INDEX IF NOT EXISTS idx_llm_calls_session_ts ON
llm_calls(session_id, timestamp DESC);`

Semantics (frozen): `0` in ReasoningTokens/CacheCreationTokens means
"provider did not report", NOT "zero occurred". Error rows keep error=1 with
zero usage — still recorded. Session id is the raw conversation/session id;
no normalization.

### What This Leaf Consumes

Nothing from a sibling leaf. All inputs exist today:
`chatOptions.sessionID` (client.go:921), `anthropicUsage` fields
(anthropic.go:877-880), the four `recordUsageStore` implementations.

## Tasks

### Task 1: `llm.TokenUsage` gains ReasoningTokens + CacheCreationTokens

**Objective:** Extend the shared usage struct; no behavior change yet.

**Files:**
- Modify: `internal/llm/models.go` (TokenUsage, ~line 168)
- Test: `internal/llm/models_test.go` (create if absent — check first with
  `ls internal/llm/models_test.go`)

**Step 1: Write failing test**

```go
func TestTokenUsage_NewFieldsJSON(t *testing.T) {
	u := TokenUsage{
		PromptTokens:        10,
		CompletionTokens:    5,
		TotalTokens:         15,
		ReasoningTokens:     3,
		CacheCreationTokens: 7,
	}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var back TokenUsage
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ReasoningTokens != 3 {
		t.Fatalf("ReasoningTokens = %d, want 3", back.ReasoningTokens)
	}
	if back.CacheCreationTokens != 7 {
		t.Fatalf("CacheCreationTokens = %d, want 7", back.CacheCreationTokens)
	}
	// Zero fields omit from JSON (omitempty) — semantic: not reported.
	if !strings.Contains(string(b), `"reasoning_tokens":3`) ||
		!strings.Contains(string(b), `"cache_creation_tokens":7`) {
		t.Fatalf("expected new keys in %s", b)
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestTokenUsage_NewFieldsJSON -v`
Expected: FAIL — TokenUsage has no ReasoningTokens / CacheCreationTokens.

**Step 3: Write minimal implementation**

Add the two fields to `TokenUsage` exactly as the contract shows, keeping
existing field order and tags. `internal/metrics/analyzer.go` may construct
`TokenUsage`-shaped aggregates — grep `TokenUsage{` across `internal/` and
fix any positional struct literals (keyed literals compile unchanged).

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestTokenUsage_NewFieldsJSON -v`
Expected: PASS

### Task 2: `llm_calls` schema migration (session_id, reasoning_tokens, cache_creation_tokens)

**Objective:** Existing DBs get the three columns + index idempotently;
fresh installs get them inline.

**Files:**
- Modify: `internal/metrics/store.go` (CREATE TABLE llm_calls block ~245,
  migration block ~347-356, index block ~337)
- Test: `internal/metrics/store_test.go`

**Step 1: Write failing test**

```go
func TestStore_LLMCallsSchemaHasSessionAndDetailTokens(t *testing.T) {
	store := newTestStore(t) // existing helper in store_test.go; else open Store at t.TempDir() path
	defer store.Close()

	var cols []string
	if err := store.db.Select(&cols,
		`SELECT name FROM pragma_table_info('llm_calls') ORDER BY cid`); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"session_id": false, "reasoning_tokens": false, "cache_creation_tokens": false,
	}
	for _, c := range cols {
		if _, ok := want[c]; ok {
			want[c] = true
		}
	}
	for c, found := range want {
		if !found {
			t.Fatalf("llm_calls missing column %q; have %v", c, cols)
		}
	}
}
```

If `newTestStore` does not exist, mirror the construction in
`TestStore_RecordAndQuery` (read the test file first) — same pattern.

**Step 2: Run test to verify failure**

Run: `go test ./internal/metrics/ -run TestStore_LLMCallsSchemaHasSessionAndDetailTokens -v`
Expected: FAIL — missing column session_id.

**Step 3: Write minimal implementation**

1. Inline the three columns in the `CREATE TABLE IF NOT EXISTS llm_calls`
   block (session_id after agent_id; reasoning_tokens and
   cache_creation_tokens after tokens_cached).
2. In the migration tolerance section (~347), add for each new column:

```go
if _, err := s.db.Exec("ALTER TABLE llm_calls ADD COLUMN session_id TEXT NOT NULL DEFAULT ''"); err != nil {
	if !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("failed to add llm_calls.session_id: %w", err)
	}
}
```

(same shape for reasoning_tokens INTEGER, cache_creation_tokens INTEGER —
copy the existing agent_id migration and adjust).
3. Add the index next to the other llm_calls indexes (~337):
   `CREATE INDEX IF NOT EXISTS idx_llm_calls_session_ts ON llm_calls(session_id, timestamp DESC);`

**Step 4: Run test to verify pass**

Run: `go test ./internal/metrics/ -run TestStore_LLMCallsSchemaHasSessionAndDetailTokens -v`
Expected: PASS. Also run the whole package: `go test ./internal/metrics/ -count=1`.

### Task 3: `RecordLLMCall` persists the new fields

**Objective:** `LLMCallRecord` gains SessionID/ReasoningTokens/
CacheCreationTokens; the INSERT writes all three; queries can read them back.

**Files:**
- Modify: `internal/metrics/store.go` (LLMCallRecord ~592, RecordLLMCall
  INSERT ~623; also update the model_performance rollup call if it passes
  record fields positionally)
- Test: `internal/metrics/store_test.go`

**Step 1: Write failing test**

```go
func TestStore_RecordLLMCallPersistsSessionAndDetailTokens(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.RecordLLMCall(LLMCallRecord{
		Timestamp:           time.Now().UTC(),
		Provider:            "anthropic",
		ModelID:             "claude-test",
		AgentID:             "coder",
		SessionID:           "conv-123",
		TokensSent:          100,
		TokensRecv:          50,
		TokensCached:        20,
		ReasoningTokens:     30,
		CacheCreationTokens: 40,
	})
	store.RecordLLMCall(LLMCallRecord{
		Timestamp: time.Now().UTC(),
		Provider:  "anthropic",
		ModelID:   "claude-test",
		AgentID:   "coder",
		// SessionID "" — unknown path must still record
		TokensSent: 1,
		TokensRecv: 2,
		IsError:    true,
	})

	var got []struct {
		SessionID           string `db:"session_id"`
		ReasoningTokens     int    `db:"reasoning_tokens"`
		CacheCreationTokens int    `db:"cache_creation_tokens"`
		TokensSent          int    `db:"tokens_sent"`
		Error               int    `db:"error"`
	}
	if err := store.db.Select(&got,
		`SELECT session_id, reasoning_tokens, cache_creation_tokens, tokens_sent, error
		 FROM llm_calls ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	if got[0].SessionID != "conv-123" || got[0].ReasoningTokens != 30 ||
		got[0].CacheCreationTokens != 40 {
		t.Fatalf("row0 = %+v", got[0])
	}
	if got[1].SessionID != "" || got[1].Error != 1 {
		t.Fatalf("row1 (unknown session, error) = %+v", got[1])
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/metrics/ -run TestStore_RecordLLMCallPersistsSessionAndDetailTokens -v`
Expected: FAIL — LLMCallRecord has no such fields / SELECT fails on
missing columns (Task 2 must land first).

**Step 3: Write minimal implementation**

Extend `LLMCallRecord` per contract; extend the INSERT column list and
values in `RecordLLMCall` (store.go:623) with session_id, reasoning_tokens,
cache_creation_tokens. Keep the error-swallowing behavior (log, never
panic/fatal). Update `QueryLLMCallUsage` (~690) only if it uses `SELECT *`
with positional scan — prefer leaving its shape untouched unless the
compiler forces it.

**Step 4: Run test to verify pass**

Run: `go test ./internal/metrics/ -run TestStore_RecordLLMCallPersistsSessionAndDetailTokens -v`
Expected: PASS

### Task 4: Thread session id + new tokens through all `recordUsageStore` sites

**Objective:** Every client (generic, Anthropic, Codex) stamps the new
fields when its provider reports them.

**Files:**
- Modify: `internal/llm/client.go` (recordUsageStore ~1477; sites ~675, ~688)
- Modify: `internal/llm/anthropic.go` (recordUsageStore ~274; sites ~288,
  ~463-468, ~696-701, ~1327-1336, ~1491-1498; usage accumulation
  ~1579-1634 for OutputTokens — add ReasoningTokens if the stream exposes
  it; populate CacheCreationTokens from CacheCreationInputTokens already
  parsed at ~879)
- Modify: `internal/llm/codex.go` (~292, ~324, ~339, ~346, ~361) and
  `internal/llm/codex_sse.go` (~324, ~346)
- Test: `internal/llm/client_test.go`, `internal/llm/anthropic_test.go`
  (extend existing tests near existing recordUsageStore coverage)

**Step 1: Write failing tests** (one per client, same shape)

```go
// client_test.go — adapt to the existing fake-store pattern used by
// TestClient_RecordsUsageStore (read it first; keep its harness).
func TestClient_RecordUsageStoreCarriesSessionID(t *testing.T) {
	// arrange client with metrics store; chatOpts{SessionID: "sess-9"}
	// drive the success path that calls recordUsageStore (existing test does this)
	// assert captured LLMCallRecord.SessionID == "sess-9"
}
```

For Anthropic, assert `CacheCreationTokens` equals the fixture's
`cache_creation_input_tokens` and, if the fixture carries
`completion_tokens_details`/thinking usage, `ReasoningTokens` is populated;
otherwise assert 0 (not reported) for a plain completion. Follow the
existing fixture style in anthropic_test.go.

**Step 2: Run tests to verify failure**

Run: `go test ./internal/llm/ -run 'RecordUsage' -v`
Expected: FAIL — records carry empty SessionID / zero new fields.

**Step 3: Write minimal implementation**

1. Generic client: `recordUsageStore(providerID, modelID, agentID, ...)`
   gains a `sessionID string` parameter (or takes `chatOpts` — match the
   Anthropic client's signature style for consistency; Anthropic already
   passes `chatOpts`). Update both call sites (~675 success, ~688 failure —
   failure keeps `chatOpts.sessionID` too, session id is known even when
   the call failed).
2. Anthropic: in `recordUsageStore` (~274), copy
   `usage.CacheCreationTokens` into the record's `CacheCreationTokens` and
   `chatOpts.sessionID` into `SessionID`. At usage-construction sites
   (~1327-1336 non-stream, ~1579-1634 stream), set
   `CacheCreationTokens: apiResp.Usage.CacheCreationInputTokens` (resp.
   `usage.CacheCreationInputTokens` for stream). If the Anthropic usage
   payload exposes reasoning/thinking output tokens in your parse target,
   map them; the current `anthropicUsage` struct does not — extend it ONLY
   if the API field exists in the payload you already decode (do not
   invent fields).
3. Codex: populate SessionID from its chatOpts; map
   `completion_tokens_details.reasoning_tokens` if the Codex usage payload
   decodes it; else leave 0.
4. OpenAI-compatible generic path: if `Response.Usage` decoding includes
   `completion_tokens_details.reasoning_tokens` (OpenAI o-series), add
   that decode and set ReasoningTokens. Check
   `internal/llm/content_parts.go` / the response decode struct first —
   add the nested struct only if absent.

**Step 4: Run tests to verify pass**

Run: `go test ./internal/llm/ -count=1` — all pass.
Then whole-repo compile + vet: `go build ./... && go vet ./internal/llm/... ./internal/metrics/...`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing (`go test ./internal/llm/... ./internal/metrics/... -count=1`)
- [ ] Interface contracts (above) satisfied exactly — field names, types, JSON tags, column names
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] `go build ./...` clean; `go vet` on touched packages clean; `gofmt -l` empty on touched files
- [ ] Grep production callers: `rg -n "recordUsageStore" internal/llm/ --glob '!*_test.go'` — every site compiles against the new signature and passes session id
- [ ] Positional-literal sweep: `rg -n "TokenUsage\{" internal/ --glob '!*_test.go'` — no positional literals broken
- [ ] No debug artifacts, no TODOs, no placeholder values

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths, SQL column names)
- [ ] Migration is idempotent (duplicate-column tolerated, fresh install inline)
- [ ] Error-path call sites still record (error=1 rows keep zero usage)
- [ ] Zero-value semantics respected: 0 = not reported, no sentinels invented
- [ ] Code follows project conventions (naming, error handling, structure)
- [ ] No bugs, no security issues
- [ ] No scope creep beyond specified tasks
- [ ] No debug artifacts

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The migration tolerance pattern catches `duplicate column name` by string
  match — modernc.org/sqlite's error text. Keep that shape; do not switch to
  error codes.
- `TotalTokens` is provider-reported and sometimes wrong; do not recompute it
  from parts. Tokscale sums buckets, not totals.
- If the OpenAI-compatible response decoder already has a
  `completion_tokens_details` struct, reuse it — do not add a duplicate.
- Keep `QueryLLMCallUsage` / `model_performance` rollup behavior unchanged;
  the rollup does not need the new fields (tokscale reads `llm_calls` raw).
- Store test helpers: check `internal/metrics/store_test.go` for an existing
  store constructor before writing one; `t.TempDir()` + `NewStore` with a
  custom `StoreConfig.DatabasePath` is the fallback.
