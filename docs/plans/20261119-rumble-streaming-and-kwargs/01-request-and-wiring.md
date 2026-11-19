# Rumble request-shape fix + factory split - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> exist. Do NOT delete/skip/weaken gate tests. The one documented exception:
> `TestRumbleSTTStream` in `stt_rumble_test.go` asserts the OLD JSON transcriptions
> body and MUST be updated to the chat.completions shape (it is the bug's own
> test). Do NOT commit. Explore with search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Correct vLLM request shape; add C2 kwargs fields; plumbing through keys frame; split NewSTT
- **Dependencies:** 00T + 00Te at TESTS_VALIDATED
- **Estimated Context:** 70K
- **Concurrency Group:** B
- **Audit references:** F1, F3, F5

## Goal

Replace the broken JSON POST to `/v1/audio/transcriptions` with the
model-card chat.completions `audio_url` request. Add the four
`chat_template_kwargs` fields to ProviderConfig and send only the explicitly
set ones. Split `NewSTT` into `newBaseSTT` (original switch) plus a `NewSTT`
wrapper so leaf 02 can add the pseudo-stream decorator without re-editing the
switch. Plumb additive selectors through both keysWire copies.

## Context

- `internal/voicechild/providers/stt_rumble.go` - file to fix
- `internal/voicechild/providers/stt_deepgram.go:154-172` - NewSTT switch
- `internal/voicechild/providers_wiring.go:17-30,100-135,195-235` - ProviderConfig, keysWire, decode
- `internal/daemon/voice_keys.go:35-55,160-215` - daemon keysWire producer + emit
- C3 request shape in ../master.md is authoritative (model card quote)

## Verified Anchors

- `internal/voicechild/providers/stt_rumble.go:27-29` - rumbleTranscriptionsPath const
- `internal/voicechild/providers/stt_rumble.go:257-280` - transcribe JSON POST
- `internal/voicechild/providers/stt_deepgram.go:154-172` - NewSTT switch
- `internal/voicechild/providers_wiring.go:17-24` - ProviderConfig

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func newBaseSTT(cfg ProviderConfig, key string) (voicechild.STTProvider, error)
// The original vendor switch, moved verbatim from NewSTT.
// NewSTT keeps its exact signature and calls newBaseSTT then (later) the decorator.
```

### What This Leaf Consumes

- C1, C2, C3, C4, C5 - see ../master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Rewrite transcribe to chat.completions | Modify: `internal/voicechild/providers/stt_rumble.go`; Update: `internal/voicechild/providers/stt_rumble_test.go` (old gate only) | `go test ./internal/voicechild/providers/ -run 'TestRumbleRequestShape|TestRumbleSTTStream' -count=1 -v` | wrong body | PASS | Given fake chat server; When Stream; Then audio_url POST + choices parse; both tests GREEN |
| T2 | Add C2 kwargs fields and body emission | Modify: `internal/voicechild/providers_wiring.go` (ProviderConfig), `stt_rumble.go` | `go test ./internal/voicechild/providers/ -run TestRumbleTemplateKwargs -count=1 -v` | undefined fields | PASS | Given kwargs set; When POST; Then chat_template_kwargs has exactly set fields |
| T3 | Plumb C4 selectors through keys frame | Modify: `internal/voicechild/providers_wiring.go` (keysWire + KeysFrame + decode), `internal/daemon/voice_keys.go` (keysWire + emit) | `go test ./internal/voicechild/ -run TestKeysFrameRumbleSelectors -count=1 -v` | undefined fields | PASS | Given keys line with selectors; When decode; Then KeysFrame carries them |
| T4 | Split NewSTT into newBaseSTT wrapper | Modify: `internal/voicechild/providers/stt_deepgram.go` | `go test ./internal/voicechild/providers/ -run 'TestNewSTT_Rumble|TestNewSTT_DefaultUnchanged' -count=1 -v` | n/a | PASS | Given any cfg; When NewSTT; Then behavior identical to pre-split (decorator added by leaf 02) |

T1 detail: replace `rumbleTranscriptionsPath` with `rumbleChatCompletionsPath =
"/v1/chat/completions"`. Build JSON with struct types carrying explicit json tags:
messages -> content -> `{type:"audio_url", audio_url:{url:"data:audio/wav;base64,<WAV>"}}`,
`temperature:0`, and `chat_template_kwargs`. Parse
`choices[0].message.content`. Keep: no auth header, 10s timeout, fail-soft empty
Final on error, no body/base64 logging, reuse writeWAV16. Update the OLD gate
test's fake server to the new path/body/response.

T2 detail: C2 fields per master C2. Use `*bool` so nil means "omit". In the body,
build `map[string]any` and add only non-nil/non-empty fields. Language is a NAME
("English"), not a BCP-47 code; pass verbatim.

T3 detail: add the 5 C4 fields to BOTH keysWire structs (child + daemon) with
omitempty, and to child KeysFrame; decode them into KeysFrame through
handleKeysFrame; in daemon emit them from resolved voice config (from C6 fields
that leaf 03 adds - use the struct field names in C6; if leaf 03 has not landed,
guard with build-safe access only after the field exists, otherwise report
RED-BUILD-BLOCKED for the emit line and leave the wire struct ready). Additive
only; absent = zero/nil.

T4 detail: rename the current NewSTT body to `newBaseSTT`, then add a thin
`NewSTT(cfg,key)` that returns `newBaseSTT(cfg,key)` today. Keep all cases
verbatim (deepgram/whisper/rumble/default); no vendor behavior change.

## Self-Verification Checklist

- All tasks implemented; gate tests GREEN; old gate updated only for request shape
- C2/C4 fields exact; chat_template_kwargs omits nil fields
- NewSTT behavior unchanged; gofmt + go vet clean; no new dependency
- No scope creep into 02/03/04 files (pseudostream.go, config/schema.go, docs)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations (exact request/response source URL)

## Review Checklist (For Review Agent)

- Request body matches model card chat.completions shape; choices parse
- kwargs fields exact and omitted when unset; keys fields on BOTH wires
- NewSTT split preserves behavior; gate tests untouched except old bug test

Output: APPROVED or specific gaps with file + line references.

## Notes

This is Phase 1: "fix the bug first". Commit this leaf before leaf 02.
