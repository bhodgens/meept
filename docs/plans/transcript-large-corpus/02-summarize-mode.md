# Summarize Mode (Local Summarizer Model) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** `summarize` parameter on transcript_fetch: map-reduce the
  transcript INSIDE the tool using an injected local summarizer
  (`llm.Chatter`), returning a dense digest. For "describe the shape"
  requests where verbatim paging is waste.
- **Dependencies:** 01-file-backed-output.md COMMITTED (same file —
  this leaf edits transcript_fetch.go on top of 01's output_path work)
- **Estimated Context:** 40K
- **Concurrency Group:** B (alone)

## Goal

"Describe the shape of this video" does not need 100k verbatim chars in
the conversation — it needs a good digest. The daemon already runs a
local summarizer client (`c.SummarizerClient`, built from
`summarizer_model` falling back to `small_model` — components.go:843)
for session summarization. This leaf gives transcript_fetch an optional
`summarize` mode that maps chunk summaries through that chatter and
reduces them into one digest, keeping the conversation to ~4k chars
while the full text stays reachable via output_path (leaf 01) when the
agent needs verbatim detail.

## Context

transcript_fetch (post-leaf-01) builds `text`, optionally writes
output_path, paginates. This leaf adds a branch BEFORE pagination: when
summarize=true, run the map-reduce over `text` and return the digest.
The chatter arrives via `SetSummarizer` (nil-guarded Set* per house
rules). Config: two new TranscriptConfig fields (this leaf OWNS
schema.go's TranscriptConfig — leaf 01 added FallbackOutputDir, you add
SummarizeEnabled + SummarizeModel; no other struct).

Chatter interface (internal/llm/interface.go:31): Chat(ctx, messages,
opts...) (*Response, error); Response.Content carries the text. Client
implements it. For tests, define a fakeChatter in the test file (the
task summarizer tests may have one — grep first, reuse the pattern).

Key files:
- internal/tools/builtin/transcript_fetch.go
- internal/tools/builtin/transcript_fetch_test.go
- internal/config/schema.go — TranscriptConfig (SummarizeEnabled,
  SummarizeModel ONLY)
- internal/config/schema_test.go — extend TestTranscriptToolConfigDefaults

## Interface Contracts (From Parent)

```
// New Parameters() entry:
//   summarize bool, optional (default false)
//
// New dependency:
//   SetSummarizer(chatter llm.Chatter) — nil-guarded; stores for
//   summarize mode.
//
// Nil summarizer + summarize=true -> error:
//   "transcript_fetch: summarization not configured (set
//   [transcript] summarize_enabled = true and a summarizer_model or
//   small_model in models.json5)"
//
// Algorithm (sequential; no goroutine fan-out):
//   1. text shorter than one window (12k chars): single reduce call
//      over the whole text (skip map)
//   2. else: slice into ~12k-char windows with 500-char overlap
//      (window boundary = whitespace backoff, never mid-word if
//      avoidable)
//   3. MAP per window — Chat with:
//      system: "Summarize this transcript segment. Preserve: steps,
//      decision rules, tool/API names, numbers, and the WHY behind
//      choices. Drop filler and repetition. Max 300 words."
//      user: the window text
//   4. REDUCE over the joined per-chunk summaries ("---"-separated):
//      system: "Merge these segment summaries into one coherent
//      summary. Keep every step, rule, name, and number. Max 800
//      words." user: joined summaries
//   5. result map: content = reduce output trimmed and capped at
//      4000 chars (suffix transcriptTruncationSuffix when capped),
//      summarized = true, chunk_count = len(windows), total_chars,
//      video_id, timestamps; path included when output_path was also
//      given (leaf 01 behavior still applies: full text on disk)
//   - any Chatter failure -> error "transcript_fetch: summarization
//     <map|reduce> stage failed: %w" (no partial digest returned)
//   - the tool's existing timeoutSeconds bounds the WHOLE operation
//     (ctx deadline already set in Execute); deadline ->
//     "transcript_fetch: summarization timed out"
//
// Config (schema.go TranscriptConfig — this leaf's fields):
//   SummarizeEnabled bool   `json:"summarize_enabled" toml:"summarize_enabled"` // default false
//   SummarizeModel   string `json:"summarize_model"   toml:"summarize_model"`   // default ""; resolution chain (summarizer_model -> small_model) is DAEMON-side (leaf 04), not the tool's concern
// Defaults frozen in DefaultTranscriptConfig + DefaultConfig.
//
// The tool itself does NOT check SummarizeEnabled — registration gating
// is leaf 04's wiring job. The tool's contract: nil summarizer ->
// config error; non-nil -> summarize works.
```

## Tasks

### Task 1: Config fields + SetSummarizer + schema defaults

**Objective:** Schema plumbing and the nil-guarded injection seam.

**Files:**
- Modify: internal/config/schema.go (TranscriptConfig only)
- Modify: internal/config/schema_test.go
- Modify: internal/tools/builtin/transcript_fetch.go (field + setter +
  schema entry)
- Test: internal/tools/builtin/transcript_fetch_test.go

**Step 1: Failing tests** — schema defaults (SummarizeEnabled false,
SummarizeModel ""); Parameters() has optional `summarize` bool;
SetSummarizer(nil) leaves the field nil (nil-guard test mirroring
TestTranscriptFetch_SetTranscriptRunner_NilGuard).

**Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.

### Task 2: Map-reduce engine (fake chatter)

**Objective:** The windowing + map + reduce algorithm, fully tested
against a fake Chatter (no real LLM).

**Files:**
- Modify: internal/tools/builtin/transcript_fetch.go
- Test: internal/tools/builtin/transcript_fetch_test.go

**Step 1: Failing tests**
- fakeChatter records calls, returns canned content ("map-summary-N" /
  "reduced-digest"). Table:
  - short text (< 12k): exactly ONE Chat call (reduce only);
    content == "reduced-digest"; chunk_count == 1
  - 30k text: 3-4 windows (assert count > 1 and window boundaries
    respect ~12k + 500 overlap); each map call's user content is a
    window; reduce call's user content contains all map outputs joined;
    content == "reduced-digest"; chunk_count == window count
  - chatter error on a map call -> error mentions "map stage failed"
  - chatter error on reduce -> "reduce stage failed"
  - digest > 4000 chars -> capped with the suffix
  - summarize=true with nil summarizer -> the config error string

**Step 2:** FAIL. **Step 3:** implement `summarizeText(ctx, text)
(string, int, error)` (returns digest + chunk count) + windowing helper
(`splitIntoWindows` — pure function, table-test the boundaries: empty,
one window, exact boundary, overlap correctness).

**Step 4:** PASS.

### Task 3: Execute wiring + interaction with output_path

**Objective:** Branch order in Execute; both features compose.

**Files:**
- Modify: internal/tools/builtin/transcript_fetch.go
- Test: internal/tools/builtin/transcript_fetch_test.go

**Step 1: Failing tests**
- summarize=true + output_path set: file on disk has the FULL text;
  content == digest; result has summarized=true, chunk_count, path
- summarize=true without output_path: content == digest, no path key
- summarize=false (or absent): zero Chat calls recorded on the fake —
  existing behavior untouched

**Step 2:** FAIL. **Step 3:** implement — branch placement: after text
is formatted and after the leaf-01 output_path write, BEFORE pagination
(digest replaces the paginated-window content entirely; summarize and
pagination are mutually exclusive paths in one call — document in a
comment that max_chars/offset are ignored when summarize=true).

**Step 4:** PASS.

### Task 4: gofmt + package green

gofmt -l touched files. `go build ./internal/config/
./internal/tools/builtin/ && go test ./internal/tools/builtin/ -run
TestTranscriptFetch -count=1 && go test ./internal/config/ -run
TestTranscriptToolConfig -count=1` — all pass. (Leaf 01's and the
original suite's tests must still pass unmodified except where this
leaf's contract says otherwise.)

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Contract satisfied: error strings, result keys, branch order
- [ ] SetSummarizer nil-guarded; fake chatter only — no real LLM in tests
- [ ] Sequential map calls; no goroutines
- [ ] SummarizeEnabled/SummarizeModel defaults frozen; other structs
      untouched
- [ ] gofmt clean; no ignored errors; no debug artifacts

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract match: error strings verbatim, result keys, branch order
- [ ] Windowing correct (12k windows, 500 overlap, whitespace backoff)
- [ ] Map/reduce prompts preserved (steps/rules/names/numbers/why)
- [ ] No scope creep (no components.go wiring — leaf 04; no skill edits)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Prompt texts are contract — tests may assert substrings; keep the
  system prompts in named constants for reviewability.
- Response.Content may need trimming (models add whitespace) — trim
  before capping.
- Do NOT gate registration on SummarizeEnabled inside the tool; the
  nil-chatter error is the tool-level contract, gating is wiring (04).
