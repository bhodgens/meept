# STT docs updates - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Document Whisper server, Deepgram endpoint, and vocabulary knobs
- **Dependencies:** 01,02,03
- **Estimated Context:** 20K
- **Concurrency Group:** C
- **Audit references:** R1,R4

## Goal

Keep user docs and package invariants aligned with the three new opt-in STT knobs.

## Context

Docs: `docs/workflows/voice.md:138` config table; `docs/workflows/voice-sidecar.md`; `internal/voice/AGENTS.md`.

## Verified Anchors

- `docs/workflows/voice.md:138` - config table
- `docs/workflows/voice-sidecar.md` - voice spec
- `internal/voice/AGENTS.md` - full contract

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Documentation only; no code signatures.
```

### What This Leaf Consumes

- C1/C2/C3 - branch orchestrator

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Document three new STT knobs | Modify: `docs/workflows/voice.md`, `docs/workflows/voice-sidecar.md` | ``grep -n "whisper_server_addr\|deepgram_endpoint_ms\|recognition_vocabulary" docs/workflows/voice.md`` | undocumented | PASS | Given docs; When read; Then each knob has default, effect, and paired-fixture note |
| T2 | Update voice AGENTS.md STT contract | Modify: `internal/voice/AGENTS.md` | ``grep -n "WhisperServerAddr" internal/voice/AGENTS.md`` | absent | PASS | Given AGENTS.md; When read; Then server mode, endpoint flag, and vocabulary semantics stated |

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

- Only edit root AGENTS.md if a new cross-boundary contract appears.
