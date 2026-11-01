# Shell Tail Truncation + Spill Pointer - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks using TDD; the gate tests already exist and are
> committed - make them pass. Do NOT delete, skip, weaken, or rewrite gate
> tests. Do NOT commit - the orchestrator handles git. Do NOT use read_file
> on source files - explore with search_files/terminal. Write once, do not
> read back.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** shell tool truncation keeps the TAIL and spills full output to a file whose path rides in the result (contracts C1/C2)
- **Dependencies:** 00T-gate VALIDATED; consumes SpillService (exists, internal/llm/spill.go)
- **Estimated Context:** 85K
- **Concurrency Group:** parallel with 02-spill-sweep (disjoint files)

## Goal

Close GAP-1: build/test output over 50KB currently keeps the first 50KB and
discards the rest. After this leaf: keep the last 50KB (line-boundary cut),
state omitted bytes, and write the full raw output to
$MEEPT_HOME/spill/tools/<session_id>/<ulid>.txt with the path in the result.

## Context

- internal/tools/builtin/shell.go:31 (MaxOutputSize), :510 and :719 (two
  truncation sites - BOTH must change; fix the class, not one site)
- internal/llm/spill.go:46 (SpillService; root $MEEPT_HOME/spill)
- Import direction: builtin -> internal/llm may already exist (check
  `go list -deps`); if it creates a cycle, define
  `type SpillWriter interface { WriteToolSpill(sessionID string, data []byte) (path string, err error) }`
  in builtin, set via existing setter pattern (nil guard required,
  setters_test.go), wire llm.SpillService from internal/daemon.
- Session ID: shell tool already receives execution context; use it. Empty
  session -> subdirectory "nosession".

## Verified Anchors

- internal/tools/builtin/shell.go:31 - `MaxOutputSize = 50000`
- internal/tools/builtin/shell.go:510 - head truncation site 1
- internal/tools/builtin/shell.go:719 - head truncation site 2
- internal/tools/builtin/shell.go:305 - `Truncated bool json:"truncated,omitempty"`
- internal/llm/spill.go:46-61 - SpillService

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// ShellResult gains:
FullOutputPath string `json:"full_output_path,omitempty"`
// Truncation note (both stdout and stderr sites):
// "... (truncated, <N> bytes total; first <M> bytes omitted; full output: <path>)"
// when spill succeeded; without "; full output: <path>" when it failed.
```

### What This Leaf Consumes

- C2 spill path: `$MEEPT_HOME/spill/tools/<session_id>/<ulid>.txt`
- pkg/id.Generate() for filenames

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Tail-keep helper | Modify: internal/tools/builtin/shell.go; Test: gate tests | `go test -p 2 -run TestShellTruncation ./internal/tools/builtin/` | gate RED | tail kept, line-boundary cut, valid UTF-8 | Given >50KB output; When truncated; Then last <=50KB on a newline boundary is returned |
| T2 | Spill write + path in result | Modify: internal/tools/builtin/shell.go (+ wiring file if interface needed); Test: new test in shell_test.go (bug-fix class) | `go test -p 2 -run TestShellSpill ./internal/tools/builtin/` | no spill today | full raw output on disk; path in note + FullOutputPath field | Given truncated output and a working spiller; When tool returns; Then the file contains the complete output byte-for-byte |
| T3 | Best-effort semantics | Test: shell_test.go | `go test -p 2 -run TestShellSpillFailure ./internal/tools/builtin/` | n/a | spilling error -> warn log, no path, tool still succeeds with tail truncation | Given a failing spill writer; When tool returns; Then exit code/output unaffected and no path in note |
| T4 | Under-cap regression | gate test T3 | `go test -p 2 -run TestShellTruncationUnderCap ./internal/tools/builtin/` | gate | byte-identical, Truncated=false, no spill file | Given <50KB; When returned; Then unchanged |

## Self-Verification Checklist

- [ ] Gate tests pass untouched; both truncation sites fixed
- [ ] Note format matches C1 exactly; JSON tag `full_output_path,omitempty`
- [ ] No rune-splitting (utf8 audit clean); no I/O under mutex
- [ ] Spill dir created 0700; file 0600

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. verify commands + last 20 lines; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Sites :510 and :719 both tail-keep (grep for `[:MaxOutputSize]` returns 0)
- [ ] Best-effort spill (no new failure mode for the shell tool)
- [ ] Import cycle resolved cleanly (interface + daemon wiring, not a hack)
- [ ] Gate tests unmodified (git diff on test files shows only additions from this leaf's new tests)
