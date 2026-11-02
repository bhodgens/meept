# Pin: Streaming Calls Are Gated - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit - the orchestrator handles all git operations after review. Do
> NOT use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back. Prefix every go
> command with GOTOOLCHAIN=go1.26.5; run from /Users/caimlas/git/meept.

## Meta

- **Parent:** ../master.md
- **Scope:** A pin test proving streaming tool calls pass the centralized
  gate. This is a PIN, not a wiring change: GateCheck already runs BEFORE
  the streaming branch in executeToolWithRetry (executor.go ~1390), so
  streaming calls (shell, web_fetch) are gated. The test locks that
  ordering so a future refactor cannot silently ungated them.
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A
- **Audit references:** tool-provider-seam closeout; master.md R2

## Goal

If a future edit moves the streaming branch ahead of the GateCheck call,
this test fails loudly. That is the entire deliverable.

## Context

- internal/agent/executor.go:1388-1395 - the `gated.GateCheck(...)` call
  sits before `backoff := NewBackoff(config)` and the retry loop; the
  StreamingTool branch is inside that loop (~1414).
- internal/agent/executor.go:472-484 - GatedExecutionRegistry interface.
- Test style: mirror executor_arggate_test.go (constructs an Executor with
  a fake tool + registry).

## Verified Anchors

- internal/agent/executor.go:1388 - `gated, _ := registry.(GatedExecutionRegistry)`
- internal/agent/executor.go:1414 - `st.ExecuteStreaming(...)` inside the
  loop, after GateCheck
- internal/agent/executor_arggate_test.go - the pattern for executor tests
  with stub tools

## Interface Contracts (From Parent)

### What This Leaf Exposes

One test file: internal/agent/executor_streaming_gate_test.go with
TestStreamingCallPassesGateCheck.

### What This Leaf Consumes

- GatedExecutionRegistry (executor.go:480) - implemented by a recording
  stub in the test.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Pin: streaming path consults the gate | Create: internal/agent/executor_streaming_gate_test.go | `GOTOOLCHAIN=go1.26.5 go test -count=1 ./internal/agent/ -run TestStreamingCallPassesGateCheck` | n/a (pin authored green; the RED proof is the mutation in T2) | Given a streaming stub tool + a recording GatedExecutionRegistry stub; When executeToolWithRetry runs it; Then BeforeExecute was invoked BEFORE the streaming body executed (sequence recorded), and GateComplete observed the streaming result | Given/When/Then in the test table |
| T2 | Mutation check: prove the pin can fail | Same file (temporary mutation, then revert) | same command | Mutation: move the GateCheck call conceptually - in the TEST, assert with a stub whose GateCheck records sequence; then temporarily patch executor.go to call GateCheck AFTER the loop and observe RED; REVERT the patch (git checkout) | RED observed under mutation; GREEN after revert | Given the mutation; When the test runs; Then it fails with a sequence assertion error |

T2 is mandatory (the repo's guard-verification rule: a guard is verified
by its failure mode). The executor.go mutation MUST be reverted
byte-identical (`git diff internal/agent/executor.go` empty afterwards).

## Self-Verification Checklist

- [ ] Pin test green; mutation observed RED; revert verified (diff empty)
- [ ] Full internal/agent suite: only the known pre-existing skills-listing
      failures (4) remain
- [ ] gofmt clean; ASCII-only

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify output (last 20 lines) + the mutation RED output
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Sequence assertion actually orders GateCheck vs streaming body
- [ ] Mutation RED observed and reverted (diff empty)
- [ ] No other tests modified

Output: APPROVED or specific gaps.

## Notes

- The stub streaming tool must implement tools.StreamingTool AND tools.Tool.
- Do not touch the executor's production code except the T2 mutation,
  which is reverted.
