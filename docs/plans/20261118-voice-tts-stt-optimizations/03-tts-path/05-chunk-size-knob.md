# qwen3 chunk_size knob (F9) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Expose opt-in qwen3 chunk_size; default 4
- **Dependencies:** 00T-tts-tests.md, foundation C1; SERIALIZE AFTER 03
- **Estimated Context:** 30K
- **Concurrency Group:** C
- **Audit references:** F9

## Goal

Let operators tune first-audio on their server without a code change. Local measurements at chunk 1/2 were noisy, so keep default 4.

## Context

`qwen3PCMChunkSize = 4` (`tts_openai.go:43`), used in request body (`:252`). Keys frame has no selector. Plumb config -> keys -> ProviderConfig -> request.

## Verified Anchors

- `internal/voicechild/providers/tts_openai.go:40-43` - constant
- `internal/voicechild/providers/tts_openai.go:248-253` - request body
- `internal/voicechild/keys.go` - Qwen3ChunkSize selector
- `internal/voicechild/providers_wiring.go:17` - ProviderConfig

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Qwen3ChunkSize=0 -> body chunk_size 4 (legacy); positive -> override.
```

### What This Leaf Consumes

- C2 Qwen3ChunkSize - branch orchestrator
- ProviderConfig - `internal/voicechild/providers_wiring.go:17`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Plumb chunk size to provider config | Modify: `internal/voicechild/keys.go`, `internal/voicechild/providers_wiring.go` | ``go test -p 2 ./internal/voicechild/ -run TestQwen3ChunkSizeOverride`` | field absent | PASS | Given keys value 1; When provider built; Then cfg carries 1; Given absent; Then 0 |
| T2 | Use override in request body | Modify: `internal/voicechild/providers/tts_openai.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestQwen3ChunkSizeOverride`` | constant used | PASS | Given override 1; When request built; Then chunk_size=1; Given 0; Then 4 |

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

- Shares `tts_openai.go` with leaf 03; dispatch only after 03 COMPLETE.
