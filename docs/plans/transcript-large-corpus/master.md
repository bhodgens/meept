# Transcript Large-Corpus Handling - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 leaf documents under this node
- **Scope:** Architectural fix for the tool-result ceiling on large
  transcripts: file-backed ingest as the default for "learn" workflows,
  local model summarization for "describe the shape" requests, and docs
  that describe the real workflow.

## Goal

The agent loop caps every tool result at `ToolResultMaxTokens = 3000`
(~9k chars; internal/agent/loop.go:3216), shrinks it toward 600 tokens as
the turn budget drains (loop.go:4027), windows history at 30k tokens per
iteration with oldest-first eviction, and stops the turn at 50k tokens.
A 100k-char transcript (~33k tokens) therefore cannot flow through the
conversation — and should not. Today `transcript_fetch` dumps text into
that funnel; the learn-from-video skill's "summarize in 40k chunks" step
promises something the architecture cannot deliver.

This tree re-shapes the flow around the constraint instead of fighting it:

| # | Capability | Leaf |
|---|-----------|------|
| 1 | `output_path` on transcript_fetch — full transcript to disk, bounded preview in-conversation; learn workflow reads pages via `file_read` | 01 |
| 2 | `summarize` mode: local summarizer model chunks + map-reduces inside the tool; result is a dense digest + pointer to the full text | 02 |
| 3 | learn-from-video skill rewrite + workflow docs to match reality | 03 |
| 4 | Integration wiring, config registration, e2e greps | 04 |

## Architecture

`transcript_fetch` gains two orthogonal knobs.

**output_path (leaf 01):** when the caller passes `output_path`, the tool
writes the FULL formatted transcript to that path (resolved against the
session working dir via `tools.WorkingDirFromContext`, falling back to
`media.output_dir` semantics — never the daemon CWD, per AGENTS.md) and
returns a bounded result: `path`, `total_chars`, `offset`/`max_chars`
echoes (still honored for the preview), and the first ~1000 chars as
`content`. The conversation never sees the bulk. The agent processes the
file with the existing `file_read` tool (offset/limit paging, 5MB cap),
taking notes per slice — distill-as-you-go instead of hold-everything.

**summarize (leaf 02):** when `summarize: true`, the tool runs a
map-reduce INSIDE the tool process: slice the formatted transcript into
~12k-char windows with 500-char overlap, send each to the injected
`llm.Chatter` (the existing `c.SummarizerClient`, built from
`summarizer_model` falling back to `small_model` — components.go:843),
and roll the per-chunk summaries up with a second call. The tool returns
the final digest (bounded to ~4k chars) plus `path` when output_path was
also given. The parent model reasons over the digest; raw pages remain
reachable via output_path + file_read when it needs verbatim detail.
Nil chatter -> clear error telling the user to configure `summarizer_model`
or `small_model` (degrades like the transcript dependency itself).

**Config (leaf 02):** `[transcript] summarize_enabled` (default FALSE —
it spends LLM tokens per call) and `summarize_model` (default "" ->
`summarizer_model` -> `small_model` fallback chain, mirroring the
classifier_model precedent at schema.go:1722). The chatter injected is
`c.SummarizerClient`; when `summarize_model` is set and differs from the
chain default, leaf 04 wires a dedicated client via the existing
`createAuxiliaryLLMClientWithResolver` helper.

**Skill + docs (leaf 03):** learn-from-video step 2 changes from
"summarize in 40k chunks" (impossible) to the real workflow: fetch with
output_path, then file_read slices of ~400 lines taking structured notes
per slice, then synthesize from notes. "Describe the shape" requests ->
`summarize: true`. Docs updated in both workflow files.

Wiring is centralized in leaf 04 (single-writer for components.go and
schema.go), consistent with the skill-authoring tree's discipline.

## Interface Contracts

### Contract 1: output_path (C1)

```
// File: internal/tools/builtin/transcript_fetch.go
// New Parameters() entries:
//   output_path string, optional — write the full formatted transcript
//     to this path. Relative paths resolve against the session working
//     dir (tools.WorkingDirFromContext); "" or absent = old in-result
//     behavior. Parent dirs are created (0o755); file written 0o644.
//
// Behavior when set:
//   - full text written to disk BEFORE any pagination slicing
//   - result map: path, total_chars, offset (clamped), truncated,
//     timestamps, video_id (unchanged keys) + content = the paginated
//     window as today (default full-in-result only when totalChars
//     fits max_chars, same as now — callers with small videos see no
//     difference unless they opt in)
//   - content ALWAYS capped at max_chars as today; when output_path is
//     set AND the text fits, content is a ~1000-char head preview plus
//     a "...full transcript at <path>" pointer line
//   - write failure -> error (do not silently fall back)
//
// Owner: 01-file-backed-output.md
// Consumers: 03-skill-and-docs.md (workflow), 04-wiring.md (docs)
```

### Contract 2: summarize mode (C2)

```
// File: internal/tools/builtin/transcript_fetch.go
// New Parameters() entry:
//   summarize bool, optional (default false)
//
// New unexported dependency:
//   summarizer llm.Chatter — injected via SetSummarizer (nil-guarded).
//   Nil summarizer + summarize=true -> error:
//   "transcript_fetch: summarization not configured (set
//   [transcript] summarize_enabled = true and a summarizer_model or
//   small_model in models.json5)"
//
// Algorithm (all inside the tool, testable with a fake Chatter):
//   1. slice formatted text into ~12k-char windows, 500-char overlap
//   2. per window: one Chat call, system prompt = "Summarize this
//      transcript segment. Preserve: steps, decision rules, tool/API
//      names, numbers, and the WHY. Drop filler. Max 300 words."
//   3. reduce: one Chat call over the joined per-chunk summaries,
//      prompt = "Merge into one coherent summary. Keep all steps,
//      rules, names, numbers. Max 800 words."
//   4. result map: content = digest (capped 4000 chars),
//      summarized = true, chunk_count, total_chars, path (when
//      output_path also given), video_id
//   - window/reduce Chatter failures -> error naming the failed stage
//   - per-call context timeout: reuse the tool's existing
//     timeoutSeconds for the WHOLE summarize operation; a deadline hit
//     mid-way returns "transcript_fetch: summarization timed out"
//
// Owner: 02-summarize-mode.md
// Consumers: 04-wiring.md (config + chatter injection)
```

### Contract 3: config (C3)

```
// File: internal/config/schema.go — extend TranscriptConfig:
//   SummarizeEnabled bool   `json:"summarize_enabled" toml:"summarize_enabled"` // default false
//   SummarizeModel   string `json:"summarize_model"   toml:"summarize_model"`   // default ""; "" -> summarizer_model -> small_model
// Defaults frozen in DefaultTranscriptConfig + DefaultConfig (existing
// pattern). gendoc comment tags per the struct's existing style.
//
// File: internal/daemon/components.go — registerBuiltinTools transcript
// block: construct with SummarizeEnabled gate; when enabled, inject
// SetSummarizer(c.SummarizerClient) (or a dedicated auxiliary client
// when SummarizeModel names a different model than the chain default).
//
// Owner: 02 owns schema.go fields; 04 owns components.go wiring.
// (schema.go single-writer: leaf 02. components.go single-writer: leaf 04.)
```

### Contract 4: skill + docs (C4)

```
// File: config/skills/learn-from-video/SKILL.md
//   step 1 gains: "for learn workflows, pass output_path=<session path>"
//   step 2 REPLACED: read the file via file_read in ~400-line slices,
//   take structured notes per slice (steps/rules/names/numbers/why),
//   synthesize from notes. "Describe the shape of this video" requests
//   -> summarize: true instead.
// File: docs/workflows/external-integrations.md — output_path +
//   summarize sections (config keys, behavior tables)
// File: docs/workflows/skills.md — composition-flow section updated to
//   match the new step 2
//
// Owner: 03-skill-and-docs.md
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-file-backed-output.md | leaf | none | 35K | A |
| 02 | 02-summarize-mode.md | leaf | 01 (same file — SEQUENTIAL after 01 commits) | 40K | B |
| 03 | 03-skill-and-docs.md | leaf | none (docs describe contracted behavior) | 25K | A |
| 04 | 04-wiring.md | leaf | 01, 02 COMMITTED | 40K | C |

**Concurrency groups:** group A = 01 + 03 (disjoint files: 01 owns
transcript_fetch.go, 03 owns SKILL.md + docs/*.md). Group B = 02 alone
(same file as 01 — dispatched only after 01 is COMMITTED). Group C = 04
alone, after 01+02 are committed. schema.go: leaf 02 is the single
writer; components.go: leaf 04 is the single writer.

## Dispatch Protocol

Same discipline as the skill-authoring tree: dispatch group A
simultaneously with the "another agent is concurrently editing
<sibling files> — do not touch or be surprised" constraints; review each
return in-session; commit per leaf (explicit paths only — the shared
index collects sibling files); then group B, then group C; then the
integration gate.

Mandatory dispatch-context lines (in addition to each leaf's own):
- "Do NOT commit. Do NOT run git add. Write code, run tests, report."
- "Do NOT use read_file on existing source files — search_files or
  terminal cat. Write each file once; do not read it back."
- "gofmt every touched file. go build + scoped go test -count=1 for
  your packages. Never unbounded go test ./... (-p 2 if you must)."
- "Run gofmt on every file you touch."

## Review Checklist

Orchestrator verifies per leaf, in-session:

- [ ] All tasks from the leaf implemented; contracts above satisfied
- [ ] Exact file paths; tests written and passing (TDD)
- [ ] SetSummarizer nil-guarded; no `_ =` errors; two-value map asserts
- [ ] No daemon-CWD fallbacks for output_path (working-dir context or
      explicit config only)
- [ ] No debug artifacts; no line-number corruption; gofmt clean
- [ ] No scope creep (no new tools, no loop changes, no evolver changes)

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-file-backed-output | PENDING | 0 | |
| 02-summarize-mode | PENDING | 0 | |
| 03-skill-and-docs | PENDING | 0 | |
| 04-wiring | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./internal/... && go vet` on touched packages — clean.
2. `go test ./internal/tools/builtin/ ./internal/config/ ./internal/daemon/ -count=1` — pass.
3. Wiring grep: SetSummarizer called exactly once in components.go;
   SummarizeEnabled/SummarizeModel present in schema.go with false/""
   defaults.
4. Skill parse: learn-from-video catalog test still passes with the
   rewritten step 2 (contract test in internal/skills may need its
   section assertions updated by leaf 03 — coordinate via that test's
   owner).
5. Behavior matrix (unit-level, fake runner + fake chatter): no params
   -> unchanged; output_path only -> file + preview; summarize only ->
   digest; both -> digest + file; nil chatter + summarize -> config
   error; chatter failure -> stage-naming error.
6. Docs grep: "40k chunks" no longer appears anywhere (the impossible
   promise is gone).

## Structural Completeness Check (Before Dispatch)

Required sections verified present in this master: Dispatch Protocol,
Interface Contracts, Child Index, Review Checklist, Completion Tracking
Table, Integration Test Plan. Each leaf carries: DISPATCH header,
Parent/scope/dependencies/estimated context, TDD tasks, interface
contract section, Self-Verification Checklist, Review Checklist, and a
"Do NOT commit" instruction.

## Open Questions

- **Preview size for output_path mode** (leaf 01): contract fixes ~1000
  chars; if the fakes show the pointer line crowding it, 800 is
  acceptable — pin the constant, don't tune per-call.
- **Overlap window edge** (leaf 02): transcripts shorter than one 12k
  window skip the map step and run reduce only — covered by a test.
- **Concurrent summarizer calls** (leaf 02): sequential map calls only;
  no fan-out. Bounded and predictable beats fast and racy inside a tool.

## Notes

- The agent-loop caps (ToolResultMaxTokens, 30k iteration window, 50k
  turn budget) are CORRECT and out of scope. This tree routes around
  them; nothing in internal/agent/ changes.
- Summarizer chain precedent: classifier_model -> small_model fallback
  lives at schema.go:1722 and components.go:830; the summarizer client
  construction is components.go:843 (createAuxiliaryLLMClientWithResolver).
  Reuse, do not reinvent.
- media.output_dir (~/.meept/media) is the fallback root when the
  session working dir is unavailable — mirror generate_media's
  resolution, never os.Getwd() in daemon code.
- Scope guard: NO changes to internal/agent/loop.go, NO new tools, NO
  evolver changes, NO RPC handlers. AGENTS.md: no new packages — the
  no-change verification applies (leaf 04 states it in the report).
