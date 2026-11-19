# TTS branch contract tests - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** RED tests for F3,F6,F8,F9,F10
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** F3,F6,F8,F9,F10,R4

## Goal

Cover the five TTS features as RED tests before implementation.

## Context

Daemon planner, child render worker, transports, resampler, and qwen3 request body each need one behavior test.

## Verified Anchors

- `internal/daemon/voice_clip_planner.go:162` - planner Run
- `internal/voicechild/clips.go:217` - clipRenderWorker
- `internal/voicechild/providers/tts_openai.go:59` - ttsTransport
- `internal/voicechild/providers/resample.go:106` - Read
- `internal/voicechild/providers/tts_openai.go:252` - ChunkSize

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
TestClipPrewarmDeferred, TestRenderYieldsToLive, TestCloudKeepAlive, TestQwen3NoKeepAlive, TestResamplerReuseByteIdentical, TestQwen3ChunkSizeOverride
```

### What This Leaf Consumes

- C1 ClipPrewarmDelayMs - 01-clip-deferral.md
- C2 Qwen3ChunkSize - 05-chunk-size-knob.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Test clip deferral gate | Test: `internal/voicechild/clips_defer_test.go` | ``go test -p 2 ./internal/voicechild/ -run TestClipPrewarmDeferred`` | gate absent | PASS | Given delay>0; When render frame arrives before played; Then queue holds; Given played; Then worker starts |
| T2 | Test render yield + transports + resampler + chunk | Test: `internal/voicechild/providers/tts_contracts_test.go` | ``go test -p 2 ./internal/voicechild/providers/ -run "TestRenderYields|TestCloudKeepAlive|TestResampler|TestQwen3Chunk"`` | symbols absent | PASS | Given live synth; When render checks; Then defer; Given cloud vendor; Then keep-alive on; Given qwen3; Then off; Given chunk override; Then body carries it |

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

- Tests only; no implementation files. Mask check required.
