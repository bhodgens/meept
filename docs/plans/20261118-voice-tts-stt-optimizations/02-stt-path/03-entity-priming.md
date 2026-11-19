# Critical-entity priming (F5) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Forward opt-in vocabulary as Deepgram keyterm params and Whisper -p prompt
- **Dependencies:** 00T-stt-tests.md, foundation C1; SERIALIZE AFTER 01 and 02
- **Estimated Context:** 35K
- **Concurrency Group:** B
- **Audit references:** F5

## Goal

Improve accuracy on names, addresses, and identifiers without changing broad WER behavior. Empty vocabulary keeps both wire requests byte-identical.

## Context

Deepgram listenPath sets model/encoding/keywords flags but no keyterm (`stt_deepgram.go:250-264`). Whisper exec args are `-m model -f wav -nt` with no prompt (`stt_whisper.go:385`). Vocabulary rides the keys frame from foundation.

## Verified Anchors

- `internal/voicechild/providers/stt_deepgram.go:250-264` - listenPath
- `internal/voicechild/providers/stt_whisper.go:374-396` - recognizeOne
- `internal/voicechild/keys.go` - Vocabulary selector
- `internal/voicechild/providers/providers.go:46` - ProviderConfig alias

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Vocabulary nil/empty -> current wire; non-empty -> Deepgram keyterm params and Whisper -p prompt argument.
```

### What This Leaf Consumes

- C3 Vocabulary - branch orchestrator
- C1 keys frame - foundation 01-keys-contract.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add Deepgram keyterm params when vocabulary non-empty | Modify: `internal/voicechild/providers/stt_deepgram.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestDeepgramKeytermParams`` | params absent | PASS | Given vocabulary [name,id]; When listenPath built; Then keyterm params present; Given empty; Then URL identical to legacy |
| T2 | Add Whisper -p prompt when vocabulary non-empty | Modify: `internal/voicechild/providers/stt_whisper.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestWhisperPromptArg`` | arg absent | PASS | Given vocabulary; When recognizeOne execs; Then -p prompt included; Given empty; Then args identical to legacy |
| T3 | No over-bias guard and paired fixture note | Modify: provider files + docs note | ``go test -p 2 ./internal/voicechild/providers/...`` | - | clean | PASS | Given >N terms; When configured; Then warning logged; docs name paired critical-entity fixture requirement |

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

- Shares provider files with leaves 01 and 02. Dispatch only after both COMPLETE.
- Over-biasing risk: keep vocabulary short; document paired fixtures.
