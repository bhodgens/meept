# Codemode Sandbox Core - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate e2e
> cases exist; make the sandbox-level ones pass. Do NOT commit. Write once,
> never read back. Explore with search_files/terminal, not read_file.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** internal/codemode package per contract C3 with the spike-chosen runtime
- **Dependencies:** 00S verdict (runtime pinned in orchestrator), 00T gate
- **Estimated Context:** 100K
- **Concurrency Group:** solo

## Goal

Ship `internal/codemode`: New/Execute/Close per C3, tool bridges as JSON-in/
JSON-out functions with ctx cancellation, hard timeout, memory cap, no direct
fs/net, Output and Value clamped to 50KB.

## Context

- Runtime + bridge idiom: as recorded in ./orchestrator.md Completion
  Tracking Table notes (spike verdict). Follow the spike's scratch module
  for the embedding pattern.
- internal/codemode imports NOTHING from internal/tools or builtin (generic).
- pkg/id for any generated names; errors wrapped %w; nil guards on setters.

## Verified Anchors

- Spike report: /tmp/pi-eval/codemode-spike-report.md (BRIDGE IDIOM section)
- go.mod - add the chosen runtime dep (pure-Go if verdict said cgo-free)

## Interface Contracts (From Parent)

### What This Leaf Exposes

C3 verbatim (../master.md): `New(Options) (*Sandbox, error)`,
`(*Sandbox) Execute(ctx, script) (Result, error)`, `(*Sandbox) Close() error`,
`Options{Timeout, MemoryLimit, Tools []ToolBridge}`,
`Result{OK, Output, Value, Err}`. Script env: `tools.<name>(argsJSON) -> Promise<string>`,
`ALL_TOOLS`, `text(...)`, `return <value>`.

### What This Leaf Consumes

- none (leaf 02 consumes this API)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Package skeleton + New/Close | Create: internal/codemode/sandbox.go | `go build ./internal/codemode/` | n/a | compiles; Close idempotent | Given New with 1 bridge; When Close twice; Then no panic |
| T2 | Execute happy path + Value clamp | Create: internal/codemode/exec.go | `go test -p 2 ./internal/codemode/ -run TestExecute` | RED | return value JSON-serialized into Result.Value; >50KB clamped with marker | Given script returning a 60KB string; When executed; Then Value length <= 50KB+marker |
| T3 | Tool bridge round-trip + errors | Create: internal/codemode/bridge.go | `go test -p 2 ./internal/codemode/ -run TestBridge` | RED | args JSON -> Call -> result JSON back; bridge error rejects with same message | Given bridge echoing args; When script calls it; Then round-trip equality |
| T4 | Timeout kill | Modify: exec.go | `go test -p 2 ./internal/codemode/ -run TestTimeout` | RED | infinite loop terminated near Timeout; Result.Err names timeout | Given loop script + 200ms timeout; When executed; Then wall < 2s, Err set |
| T5 | Memory cap + no-fs negative | Modify: exec.go | `go test -p 2 ./internal/codemode/ -run TestMemoryCap` | RED | allocation-heavy script aborts; fs/net identifiers undefined | Given array-doubling script + 64MB cap; When executed; Then Err set, host RSS bounded |
| T6 | Output clamp + text() | Modify: exec.go | `go test -p 2 ./internal/codemode/ -run TestOutputClamp` | RED | text() accumulates into Output, clamped 50KB | Given 60KB of text() calls; When executed; Then Output <= 50KB+marker |

NOTE: unit tests inside the NEW package are an accepted exception only if the
orchestrator records the policy deviation; otherwise fold T2-T6 assertions
into the e2e suite (leaf 03). Orchestrator decides at dispatch and writes the
decision into the dispatch context. Default: e2e-folded (repo policy).

## Self-Verification Checklist

- [ ] C3 signatures byte-exact; no builtin/tools imports
- [ ] Timeout kill verified with wall-clock bound, not just Err
- [ ] Deterministic: same script + same bridges -> same Result (run -count=5)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. verify tails; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Spike verdict runtime matches go.mod addition
- [ ] Negative tests present (fs, net)
- [ ] No goroutine leak (Execute after Close errors cleanly)
