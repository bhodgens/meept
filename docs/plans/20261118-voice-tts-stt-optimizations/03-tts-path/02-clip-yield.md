# Clip render yield to live synthesis (F10) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Background clip renders yield to live synthesis and barge-in
- **Dependencies:** 00T-tts-tests.md, foundation C1
- **Estimated Context:** 35K
- **Concurrency Group:** B
- **Audit references:** F10

## Goal

Ensure a caller interruption and its next answer never queue behind background filler renders. Renders are idempotent and re-requested, so deferral loses nothing.

## Context

`clipRenderWorker` renders serially on pipeline ctx (`clips.go:217-346`). `clear` cancels live synths via playGen (`pipeline.go:2202-2239`) but never background renders.

## Verified Anchors

- `internal/voicechild/clips.go:217-346` - render worker/renderClip
- `internal/voicechild/pipeline.go:2202-2239` - clear path
- `internal/voicechild/pipeline.go:1849` - tryLaunchPrefetch
- `internal/voicechild/keys.go` - ClipPrewarmDelayMs selector

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
When pipeline has live synth jobs or is speaking, renderClip skips or defers the render; next pass re-requests it.
```

### What This Leaf Consumes

- C1 ClipPrewarmDelayMs - branch orchestrator
- C3 lock rule - branch orchestrator

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add yield check in renderClip | Modify: `internal/voicechild/clips.go` | ``go test -p 2 ./internal/voicechild/ -run TestRenderYieldsToLive`` | no check | PASS | Given live synth active; When renderClip called; Then Speak deferred; Given idle; Then render proceeds |
| T2 | Barge-in re-request guarantee | Modify: `internal/voicechild/clips.go` | ``go test -p 2 ./internal/voicechild/ -run TestRenderReRequest`` | drops render | PASS | Given deferral; When clear; Then spec remains queued for next pass; no lost clip |
| T3 | No new lock; race clean | - | ``go test -p 2 -race ./internal/voicechild/...`` | - | clean | Given all changes; When race suite; Then green; yield check uses existing playMu fields |

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

- If leaf 01 lands, this leaf is largely satisfied; still implement the barge-in yield check.
