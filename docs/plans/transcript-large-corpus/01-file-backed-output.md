# File-Backed Transcript Output - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** `output_path` parameter on transcript_fetch: write the full
  formatted transcript to disk, return path + bounded preview in the
  tool result. The conversation never carries the bulk.
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A — another agent concurrently edits
  config/skills/learn-from-video/SKILL.md and docs/workflows/*.md. Do
  NOT touch those files.

## Goal

The agent loop truncates any tool result to ~9k chars (3000 tokens,
shrinking toward 600 late in a turn). A 100k-char transcript can never
reach the model in one result — by design. The learn workflow needs the
FULL text, reachable in slices the model can digest: write it to a file,
return the path plus a short preview, and let the agent page through it
with the existing `file_read` tool (which supports offset/limit and
never enters the conversation whole).

## Context

transcript_fetch lives at internal/tools/builtin/transcript_fetch.go:
~470 lines. It already has pagination (`offset`/`max_chars`, sliced on
the formatted text with `total_chars` in the response — commit 62e92b39).
Execute builds `text` via fetchTranscriptText, then normalizes
offset/maxChars, slices `page`, appends the suffix when clipped, and
returns a map: video_id, content, truncated, timestamps, offset,
total_chars.

Working-dir resolution precedent: tools receive the session working dir
via `tools.WorkingDirFromContext(ctx)` (internal/tools/workdir_ctx.go;
see internal/tools/builtin/generate_media_test.go for the test pattern
using tools.ContextWithWorkingDir). NEVER os.Getwd() in daemon code
(AGENTS.md). Relative output_path resolves against the working dir when
one is present; when the context carries no working dir, fall back to
the tool's configured fallback root (new TranscriptConfig field,
default ~/.meept/media — mirror generate_image's media.OutputDir
resolution pattern at internal/config/schema.go EffectiveMediaConfig).

Key files:
- internal/tools/builtin/transcript_fetch.go — the tool
- internal/tools/builtin/transcript_fetch_test.go — fake-runner tests
- internal/tools/workdir_ctx.go — working-dir context helpers
- internal/config/schema.go:150 — TranscriptConfig (you ADD one field
  here: FallbackOutputDir; you do NOT touch other structs)

## Interface Contracts (From Parent)

```
// New Parameters() entries:
//   output_path string, optional — write the full formatted transcript
//     to this path. Relative paths resolve against the session working
//     dir; "" or absent = old in-result behavior. Parent dirs created
//     (0o755); file written 0o644.
//
// New TranscriptConfig field:
//   FallbackOutputDir string — used when the context carries no working
//     dir and output_path is relative. Default "~/.meept/media" when
//     empty (expand ~ like EffectiveMediaConfig does).
//
// Behavior when output_path is set (after text is fully formatted):
//   1. mkdir -p the parent; write the FULL text to the resolved path
//      (0o644). Write failure -> returned error, no partial fallback.
//   2. if totalChars <= maxChars (no pagination was needed): content =
//      first ~1000 chars of the text + "\n...[full transcript at
//      <resolvedPath>]" pointer line; truncated = false
//   3. else: content = the paginated window exactly as today (offset/
//      max_chars honored, suffix when clipped); the path is added to
//      the result map so the agent can page the rest via file_read
//   4. result map gains: path (resolved absolute path, omitted when
//      output_path not given)
//
// When output_path is NOT set: byte-identical behavior to today.
```

## Tasks

### Task 1: Config field + path resolution

**Objective:** FallbackOutputDir on TranscriptConfig; resolve
output_path against working-dir context with the fallback root.

**Files:**
- Modify: internal/config/schema.go (TranscriptConfig ONLY — add
  FallbackOutputDir with a doc comment; freeze the default in
  DefaultTranscriptConfig and DefaultConfig per the existing pattern)
- Modify: internal/tools/builtin/transcript_fetch.go
- Test: internal/tools/builtin/transcript_fetch_test.go
- Test: internal/config/schema_test.go (extend the existing
  TestTranscriptToolConfigDefaults with the new field)

**Step 1: Write failing tests**
- schema: DefaultTranscriptConfig().FallbackOutputDir == "~/.meept/media".
- resolution table: absolute path passes through; relative path + working-dir
  context -> filepath.Join(wd, rel); relative + no working-dir context ->
  fallback root joined; empty output_path -> no resolution, no file.

**Step 2:** run to verify FAIL.

**Step 3: Implement** `resolveOutputPath(ctx, raw) (string, error)` —
unexported, using tools.WorkingDirFromContext (grep workdir_ctx.go for
the exact getter name first) and t.fallbackOutputDir. Expand `~` the way
schema.go does for media.output_dir (find the helper; reuse, don't copy).

**Step 4:** run to PASS.

### Task 2: File write + bounded preview result

**Objective:** Write the full text; shape the result per the contract.

**Files:**
- Modify: internal/tools/builtin/transcript_fetch.go
- Test: internal/tools/builtin/transcript_fetch_test.go

**Step 1: Write failing tests** (fake runner, small canned transcript):
- output_path set, text fits: file exists on disk with the FULL text;
  content is a head preview (<= ~1000 chars + pointer line naming the
  path); result has path; truncated == false; total_chars correct.
- output_path set + offset/max_chars: content is the paginated window
  (existing semantics), result has path, file still has the FULL text.
- output_path set + unwritable parent (point it at a path under an
  existing FILE, e.g. <tmp>/not-a-dir/x.txt): Execute returns an error
  mentioning the path; no result map.
- output_path empty: result has NO path key; behavior identical to
  current tests (the existing suite must stay green unchanged).

**Step 2:** run to verify FAIL.

**Step 3: Implement** in Execute after `text` is built and BEFORE the
offset normalization block: if output_path != "" -> resolve, MkdirAll
parent 0o755, os.WriteFile 0o644 (errors wrapped with %w + context).
Then the existing pagination logic runs unchanged; afterwards, when
path != "" AND nothing was clipped AND totalChars <= maxChars: replace
content with the bounded preview + pointer line. Add `path` to the map
only when output_path was given.

**Step 4:** run to PASS.

### Task 3: gofmt + package green

gofmt -l on touched files (fix if listed). `go build
./internal/config/ ./internal/tools/builtin/ && go test
./internal/tools/builtin/ -run TestTranscriptFetch -count=1` and
`go test ./internal/config/ -run TestTranscriptToolConfig -count=1` —
all pass.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contract satisfied exactly (result keys, write mode,
      resolution order: working dir -> fallback root)
- [ ] No os.Getwd anywhere
- [ ] FallbackOutputDir default frozen in both default sites
- [ ] Existing pagination tests green WITHOUT modification
- [ ] gofmt clean; no ignored errors; no debug artifacts

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract match: resolution order, write modes, result keys
- [ ] Full text on disk byte-identical to formatted text (no slicing
      before write)
- [ ] Write failure surfaces as error (no silent in-result fallback)
- [ ] No scope creep (no summarize, no config wiring, no skill edits)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The preview constant (~1000 chars) is fixed; the pointer line may
  push total content a bit past it — acceptable, pin the constant.
- The existing `offset`/`max_chars` tests MUST stay green untouched —
  this leaf is strictly additive around them.
- file_read's own offset/limit paging is the consumer side; do not
  modify file_read.
