# Clip prewarm deferral (F3) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Defer daemon clip prewarm until first played edge behind opt-in
- **Dependencies:** 00T-tts-tests.md, foundation C1
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **Audit references:** F3

## Goal

Move the setup-time clip render burst off the first-audio critical path. Delay=0 keeps today behavior.

## Context

`planClipsForCall` fires at EventSetup (`voice_wiring.go:1232`). Planner Run ships full pool (`voice_clip_planner.go:162`). Child starts worker at first clip frame (`clips.go:208`). Child emits played edge (`pipeline.go:1997`).

## Verified Anchors

- `internal/daemon/voice_wiring.go:1232` - setup planner call
- `internal/daemon/voice_clip_planner.go:162-207` - Run
- `internal/voicechild/clips.go:178-210` - enqueue/worker start
- `internal/voicechild/pipeline.go:1997` - played edge
- `internal/voicechild/keys.go` - ClipPrewarmDelayMs selector

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
ClipPrewarmDelayMs=0 -> immediate render (legacy); >0 -> planner waits for played edge plus delay before sending clip_render frames.
```

### What This Leaf Consumes

- C1 ClipPrewarmDelayMs - branch orchestrator
- played edge - existing C1 frame

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add delay gating to planner Run | Modify: `internal/daemon/voice_clip_planner.go` | ``go test -p 2 ./internal/daemon/ -run TestClipPrewarmDeferred`` | no gate | PASS | Given delay=0; When Run; Then specs sent immediately; Given delay>0; When Run; Then send waits for played signal |
| T2 | Route played edge to planner gate | Modify: `internal/daemon/voice_wiring.go`, `internal/voicechild/pipeline.go` if needed | ``go test -p 2 ./internal/daemon/ ./internal/voicechild/ -run TestClipDeferralSignal`` | signal absent | PASS | Given first played frame; When routed; Then pending specs released after configured delay |
| T3 | Preserve clip identity and fallback | Modify: planner + child render path | ``make e2e-fast-area AREA=voice_clips`` | clip key drift | PASS | Given deferred render; When served; Then ClipKey unchanged; Given cache miss; Then live synth fallback unchanged |

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
- [ ] No I/O under playMu; no nested locks
- [ ] No scope creep; no bugs; no secrets in logs

## Notes

- Shares no files with 02 (child clips.go) except dispatch coordination.
- Benefit unmeasured; A/B on target box required.
