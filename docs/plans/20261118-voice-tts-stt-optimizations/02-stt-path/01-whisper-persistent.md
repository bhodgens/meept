# Persistent Whisper server mode (F2) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add opt-in persistent whisper.cpp server transport behind WhisperServerAddr
- **Dependencies:** 00T-stt-tests.md, foundation C1
- **Estimated Context:** 60K
- **Concurrency Group:** B
- **Audit references:** F2

## Goal

Remove the ~350 ms cold whisper-cli subprocess per utterance by keeping the model resident behind an opt-in server address. Exec mode remains the default.

## Context

`WhisperSTT.recognizeOne` execs `whisper-cli -m model -f wav -nt` per segment (`stt_whisper.go:374-396`). FINALS only by design (`:81-83`). FlushNow kick also pays the cold start. Server mode must preserve segmentation and FlushNow semantics.

## Verified Anchors

- `internal/voicechild/providers/stt_whisper.go:87` - WhisperSTT struct
- `internal/voicechild/providers/stt_whisper.go:374-396` - recognizeOne
- `internal/voicechild/providers/stt_whisper.go:81-83` - finals-only contract
- `internal/voicechild/keys.go` - WhisperServerAddr selector

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
WhisperServerAddr=="" -> current exec path byte-identical; non-empty -> persistent server client with same recognizeOne signature and context timeout.
```

### What This Leaf Consumes

- C1 WhisperServerAddr - foundation 01-keys-contract.md
- ProviderConfig alias - `internal/voicechild/providers/providers.go:46`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Define server client seam with same recognize signature | Modify: `internal/voicechild/providers/stt_whisper.go`; Create: `internal/voicechild/providers/stt_whisper_server.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestWhisperServerMode`` | no seam | PASS | Given server addr; When recognizeOne called; Then request sent over persistent transport, transcript returned identically |
| T2 | Preserve exec fallback + timeout + WAV write | Modify: `internal/voicechild/providers/stt_whisper.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestWhisperExecFallback`` | fallback changed | PASS | Given empty addr; When recognizeOne; Then exec.CommandContext with -nt and same timeout, byte-identical |
| T3 | Guard FlushNow and teardown in server mode | Modify: `internal/voicechild/providers/stt_whisper_server.go` | ``go test -p 2 -race ./internal/voicechild/providers/...`` | flush or close race | PASS | Given active call; When FlushNow/close; Then no stale request outlives call, no goroutine leak |

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

- If installed whisper-cli lacks `--server`, mark Q2 BLOCKED and ship exec fallback only.
- No new module dependencies.
