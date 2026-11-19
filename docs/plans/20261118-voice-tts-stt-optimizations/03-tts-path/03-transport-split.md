# Cloud OpenAI transport split (F6) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Give cloud OpenAI a keep-alive transport; keep qwen3 no-keepalive
- **Dependencies:** 00T-tts-tests.md, foundation C1
- **Estimated Context:** 30K
- **Concurrency Group:** B
- **Audit references:** F6

## Goal

Cloud OpenAI pays a fresh TCP+TLS handshake per sentence because it shares the qwen3 stale-uvicorn workaround. Split the transports.

## Context

`ttsTransport` has DisableKeepAlives=true for local uvicorn (`tts_openai.go:49-59`). Both `newOpenAITTS` (:191) and `newQwen3TTS` (:206) use it.

## Verified Anchors

- `internal/voicechild/providers/tts_openai.go:46-70` - transport var
- `internal/voicechild/providers/tts_openai.go:178-192` - newOpenAITTS
- `internal/voicechild/providers/tts_openai.go:197-207` - newQwen3TTS

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Cloud OpenAI uses keep-alive transport with bounded IdleConnTimeout. qwen3 keeps no-keepalive transport byte-identically.
```

### What This Leaf Consumes

- ProviderConfig alias - `internal/voicechild/providers/providers.go:46`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Define cloud keep-alive transport | Modify: `internal/voicechild/providers/tts_openai.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestCloudKeepAlive`` | transport absent | PASS | Given cloud vendor; When constructed; Then transport has keep-alive enabled with bounded idle timeout |
| T2 | Keep qwen3 no-keepalive | Modify: `internal/voicechild/providers/tts_openai.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestQwen3NoKeepAlive`` | behavior changed | PASS | Given qwen3 vendor; When constructed; Then DisableKeepAlives=true unchanged |

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
