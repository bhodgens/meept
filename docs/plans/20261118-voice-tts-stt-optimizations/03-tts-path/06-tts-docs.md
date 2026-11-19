# TTS docs updates - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Document clip delay, cloud transport, resampler, chunk knob
- **Dependencies:** 01,02,03,04,05
- **Estimated Context:** 20K
- **Concurrency Group:** D
- **Audit references:** R1,R4

## Goal

Keep user docs and package invariants aligned with the new TTS knobs.

## Context

Docs: `docs/workflows/voice.md` config table, `docs/workflows/voice-sidecar.md`, `internal/voice/AGENTS.md`, root AGENTS.md summary if needed.

## Verified Anchors

- `docs/workflows/voice.md:138` - config table
- `docs/workflows/voice-sidecar.md` - spec
- `internal/voice/AGENTS.md` - contract

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Documentation only; no code signatures.
```

### What This Leaf Consumes

- C1/C2 - branch orchestrator

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Document new TTS knobs | Modify: `docs/workflows/voice.md`, `docs/workflows/voice-sidecar.md` | ``grep -n "clip_prewarm_delay_ms\|qwen3_chunk_size" docs/workflows/voice.md`` | undocumented | PASS | Given docs; When read; Then both knobs have defaults and A/B note |
| T2 | Update AGENTS.md TTS contract | Modify: `internal/voice/AGENTS.md` | ``grep -n "ClipPrewarmDelayMs" internal/voice/AGENTS.md`` | absent | PASS | Given AGENTS.md; When read; Then deferral policy and transport split stated |

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

- None.
