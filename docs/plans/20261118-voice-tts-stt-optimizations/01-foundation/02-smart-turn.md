# Smart Turn verdict consumption (F1) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Consume Smart Turn verdict behind opt-in mode 2; add kill-switch mode 1
- **Dependencies:** 01-keys-contract.md
- **Estimated Context:** 55K
- **Concurrency Group:** C
- **Audit references:** F1

## Goal

Close the dead-seam finding: the Smart Turn ONNX model is loaded per local
call and fed every 20 ms chunk, but its `AnalyzeEndOfTurn` verdict has no
production caller (`pipeline.go:220-223`). Consume the verdict as an
opt-in semantic endpointing signal (mode 2). Provide an immediate kill-switch
(mode 1) that installs no analyzer for operators who prefer zero cost.
Mode 0 preserves today's behavior.

## Context

`handleOpen` builds analyzers asynchronously (`open.go:54-84`). The pump
feeds `AppendAudio` under `turnMu` (`pipeline.go:1506-1510`). Dispatch
currently waits on `turnConfirm` (900 ms) plus mic-hold re-arms
(`pipeline.go:940-1101`). `Clear()` already runs at dispatch and teardown.
jargo `smartTurnBase` has no internal lock; all calls stay under `turnMu`.

## Verified Anchors

- `internal/voicechild/pipeline.go:215-225` - `turn`, `turnMu`, dropped-verdict comment
- `internal/voicechild/pipeline.go:1500-1511` - per-chunk `AppendAudio` under turnMu
- `internal/voicechild/pipeline.go:992-1027` - `accumulateFinal`
- `internal/voicechild/pipeline.go:1077-1101` - `finalConfirmFired`
- `internal/voicechild/turn.go:22-44` - `TurnAnalyzer` interface
- `internal/voicechild/open.go:44-84` - async analyzer init
- `internal/voicechild/pipeline.go:227-233` - `analyzerInit` barrier

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Smart Turn mode behavior:
  mode 0 (default): current behavior, byte-identical
  mode 1: no Smart Turn analyzer built or fed; VAD stays intact
  mode 2: AnalyzeEndOfTurn consulted at confirm-window expiry;
          complete=true permits immediate dispatch;
          complete=false defers one bounded re-arm.
```

### What This Leaf Consumes

- C1 `SmartTurnMode` - `internal/voicechild/keys.go` - 01-keys-contract.md
- C3 `TurnAnalyzer` - `internal/voicechild/turn.go:22` - orchestrator
- C4 lock rule - root master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Wire mode into analyzer construction | Modify: `internal/voicechild/open.go`, `internal/voicechild/child.go` | `go test -p 2 ./internal/voicechild/ -run TestSmartTurnModes` | mode ignored | PASS | Given mode=1; When open handled; Then no turn factory installed; mode=0/2 installs as today |
| T2 | Gate per-chunk AppendAudio by mode | Modify: `internal/voicechild/pipeline.go:1506-1510` | `go test -p 2 ./internal/voicechild/ -run TestSmartTurnModes` | analyzer still fed in mode 1 | PASS | Given mode=1; When chunk pumped; Then AppendAudio not called; mode 0/2 unchanged |
| T3 | Consult verdict in confirm path (mode 2) | Modify: `internal/voicechild/pipeline.go:1077-1101` | `go test -p 2 ./internal/voicechild/ -run TestAnalyzeEndOfTurnConsumed` | verdict unused | PASS | Given mode=2 and complete=true; When confirm fires; Then dispatch immediate; Given complete=false; Then one bounded re-arm; verdict read under turnMu, never under playMu |
| T4 | Emit telemetry for verdict decisions | Modify: `internal/voicechild/pipeline.go` | `go test -p 2 ./internal/voicechild/ -run TestSmartTurnTelemetry` | no log | PASS | Given mode=2; When verdict applied; Then INFO log with call_id, verdict, probability, decision; no key material |
| T5 | Race + full child suite | - | `go test -p 2 -race ./internal/voicechild/...` | - | clean | Given all changes; When race suite runs; Then green, no lock nesting |

## Self-Verification Checklist

- [ ] All tasks implemented; verify commands pass
- [ ] `AnalyzeEndOfTurn` has a production caller in mode 2
- [ ] No call to `AnalyzeEndOfTurn` while holding playMu
- [ ] Bounded re-arms; no infinite defer
- [ ] Mode 0 byte-identical; mode 1 installs no analyzer
- [ ] Telemetry carries no secrets

**DO NOT COMMIT.** Orchestrator handles git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + output
3. Deviations with rationale

## Review Checklist (For Review Agent)

- [ ] Verdict actually consumed (grep for production AnalyzeEndOfTurn caller)
- [ ] Turn analyzer calls under turnMu only; no nested playMu
- [ ] Re-arms bounded; no caller cutoff regression
- [ ] Gate tests untouched; no scope creep

## Notes

- False-cutoff risk requires paired hesitation fixtures before mode 2 is
  recommended for production. Default stays 0.
- Subagent duration cap: keep this leaf within ~25 minutes; if it times out,
  audit disk for partial work and re-dispatch only the missing half.
