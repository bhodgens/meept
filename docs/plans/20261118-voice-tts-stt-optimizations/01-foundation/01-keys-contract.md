# Additive config + keys contract - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add new opt-in config fields and carry them over C1 keys line
- **Dependencies:** 00T-foundation-tests.md
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **Audit references:** R4, C1, C2

## Goal

Define every new knob once in `VoiceConfig` and extend `KeysFrame` with
omitempty selectors so all later branches consume a single seam. Zero/empty
values must reproduce the current wire and behavior byte-identically.

## Context

The daemon emits one extended C1 keys line per call
(`internal/daemon/voice_keys.go:134-227`, raw `SendRaw` at :227). The child
decodes it (`internal/voicechild/keys.go:31-91`). ConfigUI reflects knobs
in `internal/configui/sections_voice.go`. Config template lives in
`config/meept.json5` around line 1378.

## Verified Anchors

- `internal/config/schema.go:5622` - `type VoiceConfig struct`
- `internal/config/schema.go:5214-5243` - `DefaultConfig` voice defaults
- `internal/daemon/voice_keys.go:134-227` - `sendKeysFrame`
- `internal/daemon/voice_keys.go:180-188` - selection fields emitted
- `internal/voicechild/keys.go:31-91` - `KeysFrame` fields
- `internal/configui/sections_voice.go:37` - turn_confirm_ms field pattern
- `config/meept.json5:1378` - voice config template

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go VoiceConfig additions
SmartTurnMode         int      `json:"smart_turn_mode"`         // 0
WhisperServerAddr     string   `json:"whisper_server_addr"`     // ""
DeepgramEndpointMs    int      `json:"deepgram_endpoint_ms"`    // 0
RecognitionVocabulary []string `json:"recognition_vocabulary"`  // nil
Qwen3ChunkSize        int      `json:"qwen3_chunk_size"`        // 0 => 4
ClipPrewarmDelayMs    int      `json:"clip_prewarm_delay_ms"`   // 0
ContextWarmWindowMs   int      `json:"context_warm_window_ms"`  // 0 => turn_confirm_ms

// internal/voicechild/keys.go KeysFrame additions
SmartTurnMode int; WhisperServerAddr string; DeepgramEndpointMs int
Vocabulary []string; Qwen3ChunkSize int; ClipPrewarmDelayMs int
ContextWarmWindowMs int   // all omitempty on wire
```

### What This Leaf Consumes

- Root C1 - KeysFrame contract - root master.md
- Root C2 - VoiceConfig contract - root master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add VoiceConfig fields with zero defaults | Modify: `internal/config/schema.go:5622` | `go test -p 2 ./internal/config/ -run TestVoiceConfigSmartTurnDefault` | fields absent | PASS | Given DefaultConfig(); When read; Then all new fields zero/empty |
| T2 | Extend KeysFrame with omitempty selectors | Modify: `internal/voicechild/keys.go:31` | `go test -p 2 ./internal/voicechild/ -run TestKeysFrameSmartTurn` | fields absent | PASS | Given zero-value KeysFrame; When serialized; Then no new keys on wire |
| T3 | Emit new selectors from sendKeysFrame | Modify: `internal/daemon/voice_keys.go:134-227` | `go test -p 2 ./internal/daemon/ -run TestKeysFrameNewSelectors` | fields not emitted | PASS | Given config with mode=2 and vocabulary; When keys emitted; Then child receives exact values |
| T4 | Reflect knobs in ConfigUI + template | Modify: `internal/configui/sections_voice.go`, `config/meept.json5` | `go build ./internal/configui/` | fields not visible | PASS | Given config editor; When voice section shown; Then new knobs present and documented |
| T5 | Full config/daemon/child tests | - | `go test -p 2 ./internal/config/... ./internal/daemon/... ./internal/voicechild/...` | - | clean | Given all changes; When suite runs; Then green |

## Self-Verification Checklist

- [ ] All tasks implemented; verify commands pass
- [ ] Every new field omitempty on wire; zero = legacy
- [ ] ConfigUI and template updated
- [ ] No I/O under locks; no secret logging
- [ ] No scope creep or debug artifacts

**DO NOT COMMIT.** Orchestrator handles git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + output
3. Deviations with rationale

## Review Checklist (For Review Agent)

- [ ] Field names/types/defaults match C1/C2 exactly
- [ ] Absent selectors produce byte-identical wire
- [ ] Config template parses
- [ ] No gate-test modification

## Notes

- This leaf owns the shared seam. Other branches consume but must not edit
  `schema.go`, `voice_keys.go`, or `keys.go`.
