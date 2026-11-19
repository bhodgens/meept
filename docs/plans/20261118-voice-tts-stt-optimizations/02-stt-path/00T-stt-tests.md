# STT branch contract tests - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** RED tests for F2, F4, F5
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** F2,F4,F5,R4

## Goal

Cover the three STT features as RED tests before implementation.

## Context

Vendor adapters emit `Transcript` values. New keys selectors flow from foundation. Tests target provider packages and the pipeline consumer.

## Verified Anchors

- `internal/voicechild/providers/stt_whisper.go:374` - recognizeOne
- `internal/voicechild/providers/stt_deepgram.go:327` - emit
- `internal/voicechild/pipeline.go:46` - Transcript
- `internal/voicechild/pipeline.go:1205` - consumeTranscripts

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
TestWhisperServerMode, TestWhisperExecFallback, TestDeepgramEndpointFlag, TestEndpointFastDispatch, TestDeepgramKeytermParams, TestWhisperPromptArg
```

### What This Leaf Consumes

- C1 WhisperServerAddr - 01-whisper-persistent.md
- C2 Transcript.Endpoint - 02-deepgram-endpoint.md
- C3 Vocabulary - 03-entity-priming.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Test persistent vs exec Whisper mode selection | Test: `internal/voicechild/providers/stt_whisper_server_test.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestWhisper`` | server mode absent | PASS | Given empty WhisperServerAddr; When constructed; Then exec path selected; Given addr set; Then server client selected |
| T2 | Test Deepgram endpoint flag parse + fast dispatch | Test: `internal/voicechild/providers/stt_deepgram_endpoint_test.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestDeepgramEndpointFlag`` | Endpoint field absent | PASS | Given speech_final JSON; When emit; Then Transcript.Endpoint=true; Given DeepgramEndpointMs=0; When final arrives; Then timer path unchanged |
| T3 | Test vocabulary wiring to vendor params | Test: `internal/voicechild/providers/stt_vocab_test.go` | ``go test -p 2 ./internal/voicechild/providers/ -run TestVocab`` | vocabulary unused | PASS | Given vocabulary list; When Deepgram listenPath built; Then keyterm params present; Given Whisper; Then -p prompt arg present; Given empty list; Then wire unchanged |

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

- Tests only; no implementation files.
- Mask check: `grep -oE '\*{4}' <file> | wc -l` = 0.
