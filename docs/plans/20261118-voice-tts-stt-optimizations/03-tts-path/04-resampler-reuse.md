# Resampler buffer reuse (F8) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Reuse 640-byte scratch and carry buffers; byte-identical output
- **Dependencies:** 00T-tts-tests.md
- **Estimated Context:** 25K
- **Concurrency Group:** C
- **Audit references:** F8

## Goal

Remove ~270 KB of avoidable allocation churn per 3 s utterance from the child decode path.

## Context

`resamplingReader.Read` allocates `make([]byte, 640)` each pass (`resample.go:111`). `convertAligned` allocates joined/carry (`resample.go:180,190`). Verify jargo Resampler.Process does not retain input before aliasing.

## Verified Anchors

- `internal/voicechild/providers/resample.go:41-75` - struct/constructor
- `internal/voicechild/providers/resample.go:106-141` - Read
- `internal/voicechild/providers/resample.go:172-197` - convertAligned

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
No exported API change; output bytes identical for aligned and odd-byte reads.
```

### What This Leaf Consumes

- None

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Reuse scratch buffer field | Modify: `internal/voicechild/providers/resample.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestResamplerReuseByteIdentical`` | no reuse | PASS | Given multiple Read calls; When resampling; Then same output bytes; scratch allocated once |
| T2 | Verify jargo Process contract | Modify: `internal/voicechild/providers/resample.go` | ``go test -p 2 ./internal/voicechild/providers/...`` | - | clean | Given Process; When called; Then input not retained (source/docs check noted in report) |

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

- Not sold as first-audio improvement; GC pressure only.
