# Built-in Filters (json_format, language_en, lint_go) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Implement the three first-party filters - JSON format/validate, language detection, Go lint/autofix - against the OutputFilter interface.
- **Dependencies:** 01-filter-interface.md (interface + chain)
- **Estimated Context:** 65K (exploration 12K + generation 22K + iteration 18K + overhead 13K)
- **Concurrency Group:** B

## Goal

Ship the concrete filters that justify the mechanism: `json_format` (validate +
canonical reformat), `language_en` (script/stopword detection, fail-only), and
`lint_go` (gofmt rewrite + vet signal). All deterministic, all local, all
idempotent, all emitting machine-readable Reasons.

## Context

The `OutputFilter` interface and `FilterChain` come from leaf 01
(`internal/validator/filter.go`). These builtins live in separate files in the
same package, one per filter, mirroring the sub-validator layout of
`internal/validator` (`filesystem.go`, `shell.go`, etc.).

Key files to understand before implementing:
- `internal/validator/filter.go` - the interface this leaf implements (from leaf 01)
- `internal/validator/filesystem.go` - layout/doc style for a concrete validator file
- `internal/task/step.go` - how to read step metadata (ToolHint, Claims) for Applies decisions - read-only

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/validator/filter_json.go
func NewJSONFormatFilter() *JSONFormatFilter
// Name() = "json_format"
// Applies: step metadata/output_filters declares it AND the step expects JSON
//          (step metadata key "expects_json" true, or ToolHint in a small
//          known set). When not applicable, skip.
// Process: empty/whitespace output -> pass (nothing to validate).
//          invalid JSON -> FilterFail, Reason = encoding/json error, prefixed
//          with the byte offset: "invalid JSON at offset N: <err>".
//          valid JSON -> canonical reformat (2-space indent, sorted map keys,
//          trailing newline) via rewrite; byte-identical -> pass.
// Idempotency: formatting canonical output twice is byte-identical.

// File: internal/validator/filter_language.go
func NewLanguageFilter(expected string) *LanguageFilter   // expected: "en" default
// Name() = "language_en" (or "language_<code>")
// Applies: declared in step metadata/output_filters.
// Process: script-range scan (Latin vs Cyrillic vs CJK vs Arabic via
//          unicode ranges) + bundled English stopword hit-rate.
//          Detection is pure stdlib + a ~200-word stopword list embedded as
//          a Go string constant. NO network, NO model call.
//          Confidence = stopword hit-rate in [0,1].
//          Detected != expected AND confidence >= 0.5 -> FilterFail with
//          Reason "lang=<code> confidence=<f> expected=<code>".
//          Otherwise pass. NEVER rewrites (fail-only filter).
// Idempotency: pure function of input.

// File: internal/validator/filter_lint.go
func NewGoLintFilter(gofmtBin, goBin string) *GoLintFilter // empty = PATH lookup
// Name() = "lint_go"
// Applies: declared AND output contains at least one fenced or bare Go code
//          block (heuristic: "package " + "func " co-occurrence, or ```go fence).
// Process: extract code blocks -> write temp dir -> run gofmt -l (10s timeout).
//          Files needing format -> gofmt -w, re-read -> FilterRewrite with the
//          reformatted blocks substituted back into the output.
//          go vet (10s timeout) findings -> informational Warnings on the
//          ChainResult are NOT available via tri-state; encode vet findings in
//          the REWRITE diff context or pass unchanged if clean. vet failure
//          does NOT fail the chain (vet is advisory; gofmt is mechanical).
//          Subprocess timeout, missing binary, or nonzero gofmt error ->
//          FilterFail with Reason "lint_go: <err>".
// Idempotency: gofmt -w on gofmt-clean code is a no-op -> second run passes.
```

### What This Leaf Consumes

```
// From 01-filter-interface.md:
type OutputFilter interface { Name(); Applies; Process }
type FilterResult struct { Outcome; Filter; Output; Reason }
const FilterPass, FilterRewrite, FilterFail
```

## Tasks

### Task 1: json_format filter

**Objective:** Validate + canonically reformat JSON outputs.

**Files:**
- Create: `internal/validator/filter_json.go`
- Test: `internal/validator/filter_json_test.go`

**Step 1: Write failing test** - table-driven:
- pretty-printed valid JSON with unsorted keys -> rewrite, output is
  canonical (sorted keys, 2-space indent); run Process on its own output ->
  pass (idempotency proof)
- `{"a":` -> fail with Reason containing "invalid JSON at offset"
- empty output -> pass
- non-JSON step (Applies false) -> never processed

**Step 2:** `go test -p 2 ./internal/validator/ -run TestJSONFormat -v` -> FAIL
**Step 3:** implement `filter_json.go`
**Step 4:** PASS

### Task 2: language_en filter

**Objective:** Fail-only language detection, stdlib-only.

**Files:**
- Create: `internal/validator/filter_language.go`
- Test: `internal/validator/filter_language_test.go`

**Step 1: Write failing test** - table-driven:
- English text ("The quick brown fox...") -> pass
- German text ("Die Ergebnisse der Untersuchung sind...") -> fail with
  Reason matching `lang=de confidence=0\.\d+ expected=en`
- mixed with >50% English -> pass
- CJK text -> fail (script-based detection)
- empty -> pass
- idempotency: pure function; run twice, identical results

**Step 2-4:** failing -> implement -> pass (`-run TestLanguageFilter`)

Test fixture strings: use REAL German/CJK sentences, not transliterations
(detection depends on script ranges). Embed fixtures in the test file.

### Task 3: lint_go filter

**Objective:** gofmt autofix rewrite on Go code blocks.

**Files:**
- Create: `internal/validator/filter_lint.go`
- Test: `internal/validator/filter_lint_test.go`

**Step 1: Write failing test** - table-driven:
- output with a ```go block containing badly formatted code -> rewrite,
  output contains the gofmt-ed block, rest of output byte-identical
- already-formatted block -> pass
- no Go content -> Applies false, never processed
- gofmt binary not found (fake bin path) -> fail with Reason containing "lint_go:"
- subprocess timeout respected (use a bin that sleeps; keep test under 5s)

**Step 2-4:** failing -> implement -> pass (`-run TestGoLintFilter`)

Guard rails required by the repo: every exec.Cmd error checked (no `_ =`),
context timeout on both subprocesses, temp dir via t.TempDir() in tests,
os.MkdirTemp + defer RemoveAll in prod code.

### Task 4: Package wiring - builtin registry constructor

**Objective:** One constructor producing the default filter set by name.

**Files:**
- Create: `internal/validator/filter_builtin.go`
- Test: `internal/validator/filter_builtin_test.go`

**Step 1: Write failing test:** `NewBuiltinFilter(name string, cfg BuiltinConfig) (OutputFilter, error)`
returns the right type for "json_format", "language_en", "lint_go"; unknown
name returns an error listing the known names (never a nil filter + nil error).

**Step 2-4:** failing -> implement -> pass (`-run TestBuiltinRegistry`)

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing (`go test -p 2 ./internal/validator/ -v`)
- [ ] Interface contracts satisfied exactly (names, Reason formats)
- [ ] All three filters idempotent (each has an explicit idempotency test)
- [ ] No network calls, no model calls anywhere in the filters
- [ ] All subprocess errors handled (no `_ =` sites - pre-commit gate)
- [ ] All files at exact specified paths
- [ ] gofmt clean
- [ ] No deviations from spec (or documented below)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Reason formats match the contracts verbatim
- [ ] Every filter is idempotent with a dedicated test proving it
- [ ] No network/model calls; subprocesses context-bounded
- [ ] No ignored errors (`_ =`), no bare panics
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- language detection is heuristic by design. Its job is catching WHOLE-OUTPUT
  wrong-language responses cheaply, not detecting one borrowed foreign word.
  The 0.5 confidence floor encodes that; do not tune it upward.
- gofmt availability: the daemon may run where Go is absent. That is exactly
  the fake-bin fail path - fail with reason, never block the step on a
  missing tool silently.
