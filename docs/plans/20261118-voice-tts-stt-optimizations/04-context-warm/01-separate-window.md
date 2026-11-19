# Separate context warm window (F7) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add opt-in ContextWarmWindowMs
- **Dependencies:** 00T-warm-tests.md, foundation C1
- **Estimated Context:** 30K
- **Concurrency Group:** B
- **Audit references:** F7

## Goal

Let the warm request land before the confirm-window dispatch. Keep 0 = legacy behavior.

## Context

`contextwarm.go:225` uses `cw.cfg.Window`, set from resolved `turn_confirm_ms` in `voice_wiring.go:705-716`. The Whisper leg emits no partials (`stt_whisper.go:81-83`), so warm remains phone-only; that gap is documented, not fixed here.

## Verified Anchors

- `internal/voice/contextwarm.go:124-160` - ContextWarmer struct/config
- `internal/voice/contextwarm.go:218-232` - window use
- `internal/daemon/voice_wiring.go:705-716` - warmer construction
- `internal/voice/contextwarm.go:428-436` - RoutePartial
- `internal/voicechild/keys.go` - ContextWarmWindowMs selector

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
ContextWarmWindowMs=0 -> resolved turn_confirm_ms (legacy); >0 -> override debounce window. OnFinal cancellation unchanged.
```

### What This Leaf Consumes

- C1 ContextWarmWindowMs - branch orchestrator
- C2 VoiceConfig ContextWarmWindowMs - foundation 01-keys-contract.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Wire override into warmer config | Modify: `internal/daemon/voice_wiring.go`, `internal/voice/contextwarm.go` | ``go test -p 2 ./internal/voice/ -run TestContextWarmWindowOverride`` | no override | PASS | Given override>0; When warmer built; Then cfg.Window uses it; Given 0; Then turn_confirm_ms |
| T2 | Preserve OnFinal cancellation and hit-rate telemetry | Modify: `internal/voice/contextwarm.go` | ``make e2e-fast-area AREA=context_warm`` | cancel broken | PASS | Given pending warm; When OnFinal; Then cancelled; metrics unchanged; no warm for finals-only Whisper leg |

## Self-Verification Checklist

- [ ] All tasks implemented; verify commands pass
- [ ] Interface contracts satisfied exactly (field names, defaults, paths)
- [ ] All files at exact specified paths
- [ ] No scope creep; no debug artifacts, no TODOs, no line-number corruption
- [ ] Default values preserve pre-change behavior byte-identically when the new knob is unset

**DO NOT COMMIT.** Orchestrator handles git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + output (last 20 lines)
3. Deviations with rationale

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched
- [ ] Contracts match exactly (signature, type, path, default)
- [ ] No scope creep; no bugs; no secrets in logs

## Notes

- None.
