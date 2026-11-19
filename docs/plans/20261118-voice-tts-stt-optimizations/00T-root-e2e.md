# Root contract e2e gate - Test Leaf

> Do NOT commit. Tests only. Do NOT write or modify implementation files. Tests must fail RED for missing-implementation reasons. Run them once to confirm failure is missing-implementation, not syntax error. Do not use read_file on existing source. Do not read files back after writing.

## Meta

- **Parent:** ../master.md
- **Scope:** Cross-branch additive-wire contract tests for C1/C2
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** R1, R2, R4

## Goal

Validate that every new keys-frame selector and config field is additive.
Absent fields must produce the legacy wire shape and legacy defaults.

## Context

The daemon emits one extended C1 keys line per call through
`TextServer.SendRaw` (`internal/daemon/voice_keys.go:227`). The child decodes
it in `internal/voicechild/keys.go:31`. New optional fields must use
`omitempty` so an unset knob leaves the line byte-identical.

## Verified Anchors

- `internal/daemon/voice_keys.go:134-227` - `sendKeysFrame`; raw-line emission
- `internal/voicechild/keys.go:31-91` - `KeysFrame` definition
- `internal/config/schema.go:5622` - `VoiceConfig` definition

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e tests (build tag e2e) asserting legacy wire shape with no new fields
```

### What This Leaf Consumes

- C1 KeysFrame - `internal/voicechild/keys.go:31` - foundation branch
- C2 VoiceConfig - `internal/config/schema.go:5622` - foundation branch

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Test legacy keys line has no new selector keys | Create: `e2e/suites/voice_tts_stt/keys_additive_test.go`; Modify: `e2e/manifest.json` | `go test -tags e2e -p 2 ./e2e/suites/voice_tts_stt/...` | package absent / selectors absent | PASS with no new keys serialized | Given default VoiceConfig; When sendKeysFrame emits; Then raw line contains no `smart_turn_mode`, `whisper_server_addr`, `deepgram_endpoint_ms`, `vocabulary`, `qwen3_chunk_size`, `clip_prewarm_delay_ms` |
| T2 | Test legacy config defaults unchanged | Create: `e2e/suites/voice_tts_stt/config_defaults_test.go` | `go test -tags e2e -p 2 ./e2e/suites/voice_tts_stt/...` | new fields absent | PASS with defaults equal to pre-change values | Given `DefaultConfig()`; When Voice read; Then `TurnConfirmMs=900`, `FragmentConfirmMs=3000`, new fields zero values |
| T3 | Register suite in manifest | Modify: `e2e/manifest.json` | `go test -tags e2e -p 2 ./e2e/suites/voice_tts_stt/...` | path_map missing | PASS | Given suite dir; When manifest parsed; Then path_map includes `internal/voicechild/`, `internal/config/`, `internal/daemon/` |

## Self-Verification Checklist

- [ ] All tasks implemented; verify commands pass
- [ ] Tests fail RED for missing-implementation reasons before implementation
- [ ] No implementation files written
- [ ] No mask corruption: `grep -oE '\*4' <new-file> | wc -l` is 0
- [ ] `e2e/manifest.json` updated with suite entry

**DO NOT COMMIT.** Orchestrator commits gate tests after validation.

## Completion Report Format

1. Files touched
2. Verify command + output (last 20 lines)
3. Deviations with rationale

## Review Checklist (For Review Agent)

- [ ] Tests assert behavior (wire shape, default values), not implementation details
- [ ] No tautologies
- [ ] Suite registered in manifest
- [ ] No implementation files touched

## Notes

- Gate commit will fail hooks by design (symbols not yet present).
  Orchestrator runs manual gates and commits with `--no-verify` after
  recording commands, per tests-first-gate.md.
