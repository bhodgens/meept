# Transcript Fetch Tool - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** A `transcript_fetch` builtin tool that turns any YouTube
  URL form into transcript text via the `youtube-transcript-api` Python
  subprocess.
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Meept agents cannot read video content today. This leaf adds the ingest
half of the learn-from-media chain: a tool that fetches a YouTube
transcript (auto-generated or human) and returns plain text (optionally
timestamped) as a tool result. It follows the WebFetchTool pattern
exactly: injectable dependencies, actionable error strings, no security
orchestrator required (it performs no arbitrary fetches — the only
network side effect is YouTube's transcript endpoint, reached via the
Python library).

## Context

Meept is a Go agent platform. Builtin tools live in
`internal/tools/builtin/` and implement the `tools.Tool` interface
(Name, Description, Parameters, Execute — `internal/tools/interface.go:24`).
`web_fetch.go` is the canonical read-only tool: constructor takes
(t, cap), optional `Set*` setters with nil guards, schema in
`Parameters()`, execution in `Execute(ctx, args json.RawMessage)`.

Subprocess precedent: STT runs whisper.cpp as a subprocess
(docs/workflows/speech-to-text.md); this leaf uses the same shape for
`youtube-transcript-api` (a small pip package). The tool must degrade
gracefully when Python or the package is missing — actionable error,
no daemon impact.

Key files to understand before implementing:
- internal/tools/builtin/web_fetch.go — tool pattern to mirror
  (constructor, setters, schema, Execute, truncation)
- internal/tools/interface.go:24 — the Tool interface
- internal/tools/builtin/setters_test.go — nil-guard test pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/tools/builtin/transcript_fetch.go
package builtin

type TranscriptConfig struct {
    PythonPath     string // default "python3" when empty
    ModuleName     string // default "youtube-transcript-api" when empty
    TimeoutSeconds int    // default 60 when <= 0
}

func NewTranscriptFetchTool(cfg TranscriptConfig, logger *slog.Logger) *TranscriptFetchTool

// Name() == "transcript_fetch"
// Parameters schema:
//   url        string, required
//   timestamps bool, optional (default false)
//   language   string, optional
```

### What This Leaf Consumes

Nothing from sibling leaves. Standard library + exec only.

## Tasks

### Task 1: Tool skeleton + schema

**Objective:** Create the tool struct, constructor, and Parameter
schema per the contract.

**Files:**
- Create: `internal/tools/builtin/transcript_fetch.go`
- Test: `internal/tools/builtin/transcript_fetch_test.go`

**Step 1: Write failing test**

```go
func TestTranscriptFetch_NameAndSchema(t *testing.T) {
    tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
    if tool.Name() != "transcript_fetch" {
        t.Fatalf("name = %q", tool.Name())
    }
    // Assert schema: url required string; timestamps bool; language string.
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/tools/builtin/ -run TestTranscriptFetch_NameAndSchema -count=1`
Expected: FAIL (undefined)

**Step 3: Write minimal implementation**

Struct + `NewTranscriptFetchTool` applying config defaults (empty
PythonPath -> "python3", empty ModuleName, TimeoutSeconds <= 0 -> 60).
Logger nil-safe: store as-is, skip logging when nil (match how sibling
tools treat optional logger).

**Step 4: Run test to verify pass**

Run: `go test ./internal/tools/builtin/ -run TestTranscriptFetch_NameAndSchema -count=1`
Expected: PASS

### Task 2: URL parsing to video ID

**Objective:** Accept every YouTube URL form and extract the 11-char
video ID; reject non-YouTube hosts.

**Files:**
- Modify: `internal/tools/builtin/transcript_fetch.go`
- Test: `internal/tools/builtin/transcript_fetch_test.go`

**Step 1: Write failing test**

Table-driven: `https://www.youtube.com/watch?v=DWoJZs6TuVs`,
`https://youtu.be/DWoJZs6TuVs`,
`https://www.youtube.com/shorts/DWoJZs6TuVs`,
`https://www.youtube.com/embed/DWoJZs6TuVs`,
`https://www.youtube.com/live/DWoJZs6TuVs`,
bare `DWoJZs6TuVs`. Negative: `https://example.com/x`, empty string,
11-char ID with invalid chars.

**Step 2: Run test to verify failure**

Run: `go test ./internal/tools/builtin/ -run TestTranscriptFetch_ParseVideoID -count=1`
Expected: FAIL

**Step 3: Write minimal implementation**

`func (t *TranscriptFetchTool) parseVideoID(raw string) (string, error)`.
Hosts accepted: www.youtube.com, youtube.com, m.youtube.com,
youtu.be, music.youtube.com (path segments: /watch?v=, /shorts/,
/embed/, /live/, bare for youtu.be and raw IDs). Raw ID: exactly 11
chars of [A-Za-z0-9_-]. Anything else: error
`"not a youtube video url or video id"`.

**Step 4: Run test to verify pass**

Expected: PASS

### Task 3: Subprocess execution with injectable runner

**Objective:** Run the Python module as a subprocess; make the command
runner injectable so tests never execute real Python.

**Files:**
- Modify: `internal/tools/builtin/transcript_fetch.go`
- Test: `internal/tools/builtin/transcript_fetch_test.go`

**Step 1: Write failing test**

```go
func TestTranscriptFetch_Execute_FakeRunner(t *testing.T) {
    // Inject a runner func that asserts the argv contains:
    //   <pythonPath> -m youtube_transcript_api ...
    // and returns canned JSON lines: [{"text": "hello", "start": 0.0}, ...]
    // Assert output text == "hello" (no timestamps) and
    // "[00:00] hello" with timestamps=true.
}
func TestTranscriptFetch_Execute_RunnerMissingDep(t *testing.T) {
    // Runner returns stderr "No module named youtube_transcript_api".
    // Assert the error mentions installation guidance:
    // "youtube-transcript-api not installed" + pip install hint.
}
func TestTranscriptFetch_Execute_Timeout(t *testing.T) {
    // Runner blocks past TimeoutSeconds (simulate via ctx deadline in
    // the fake). Assert error mentions timeout.
}
```

**Step 2: Run test to verify failure**

Expected: FAIL

**Step 3: Write minimal implementation**

- Unexported `runner` field: `func(ctx context.Context, name string,
  args []string) (stdout, stderr []byte, err error)`, defaulting to
  exec.CommandContext. Constructor accepts an optional runner via
  `WithTranscriptRunner(fn)` functional option on the constructor OR a
  `SetTranscriptRunner` (pick ONE style matching web_fetch.go's
  conventions — web_fetch uses Set* setters; use Set* with nil guard).
- Invoke: `exec.CommandContext(ctx, pythonPath, "-m",
  "youtube_transcript_api", videoID, "--languages", lang...)` — check
  the module's actual CLI surface at implementation time with
  `python3 -m youtube_transcript_api --help` if available; if the CLI
  is awkward, prefer the documented Python-API-via-heredoc fallback
  (`python3 -c "..."` script that imports the module and prints JSON
  lines of {text,start}). Choose whichever is stable and testable;
  record the choice in a comment.
- Language fallback: pass requested language first; on
  "No transcripts found"/no-matching-language stderr, retry once with
  the default transcript.
- Error mapping (return as errors, prefix context):
  - module missing -> `"transcript_fetch: youtube-transcript-api not
    installed (install: <python> -m pip install
    youtube-transcript-api)"` — wrap %w the exec error.
  - transcript disabled -> `"transcript_fetch: transcripts are disabled
    for this video"`.
  - video unavailable/private -> pass through with
    `"transcript_fetch: video unavailable"` context.
  - ctx deadline -> `"transcript_fetch: timed out after Ns"`.
- Output size: truncate final text at 100_000 chars with a
  `"...[truncated]"` suffix (match web_fetch's cap discipline).

**Step 4: Run test to verify pass**

Expected: PASS

### Task 4: Execute wiring of args + timestamps format

**Objective:** Full `Execute(ctx, args json.RawMessage)` path: parse
args, validate url, run, format, return.

**Files:**
- Modify: `internal/tools/builtin/transcript_fetch.go`
- Test: `internal/tools/builtin/transcript_fetch_test.go`

**Step 1: Write failing test**

End-to-end Execute with fake runner: valid url -> success text; bad
url -> error; timestamps=true -> `[MM:SS] text` lines; language unset
-> runner receives no --languages restriction.

**Step 2: Run test to verify failure**

Expected: FAIL

**Step 3: Write minimal implementation**

Args parse via encoding/json into a params struct (two-value map
assertions are NOT needed here — this is a typed struct). Format:
timestamps false -> paragraphs of text joined by "\n"; true ->
"[MM:SS] text" per segment (MM:SS from start seconds; hours become
HH:MM:SS).

**Step 4: Run test to verify pass**

Expected: PASS

### Task 5: gofmt + package green

**Objective:** Format and confirm the package (not repo) builds and
tests clean.

**Step 1:** gofmt -l on the two files; fix if listed.
**Step 2:** `go build ./internal/tools/... && go test
./internal/tools/builtin/ -count=1` — pass.
(Do NOT run unbounded repo-wide tests.)

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] Every Set* method nil-guarded; no ignored errors; no panics
- [ ] gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths)
- [ ] Code follows project conventions (naming, error handling, structure)
- [ ] No bugs, no security issues (no shell injection: video ID is
      validated charset before reaching argv; no user input into a
      shell string)
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The Python CLI surface of youtube-transcript-api has changed across
  versions (v1.x moved to the Python API). Prefer the `python3 -c`
  JSON-lines approach if the CLI proves unstable; the fake runner
  tests are CLI-shape-agnostic either way. Document the exact argv in
  a comment so the review agent can verify it against the installed
  package if present.
- SSRF does not apply: the tool never fetches user-supplied URLs
  directly; the Python library only contacts YouTube transcript
  endpoints for the parsed video ID.
- If python3 is absent entirely, exec fails fast — ensure the error
  still carries the install guidance (same message path as
  module-missing).
