# Rumble streaming tree - E2E Test Gate Leaf

> **For the implementing agent:** Write TESTS ONLY for F1, F2, F3, F5. Do NOT
> write implementation. Do NOT commit. Tests must fail RED for
> missing-implementation reasons or be honestly skipped with a documented
> reason. Explore with search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** E2E gate tests for request shape, pseudo-stream partials, kwargs, keys selectors
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Audit references:** F1, F2, F3, F5

## Goal

Provide hermetic e2e coverage for the corrected Rumble request shape, generic
partial streaming, kwargs propagation, and keys-frame selectors. No real vLLM.

## Context

- `e2e/suites/voice-lifecycle/rumble_stt_test.go` - prior tree's gate (DO NOT EDIT)
- `e2e/manifest.json` - scenario registry; add new scenario ids
- Contracts C1-C6 live in ../master.md

## Verified Anchors

- `e2e/manifest.json` - voice-keys-rumble-01 scenario template
- `internal/daemon/voice_keys.go:35-55` - keysWire producer
- `internal/voicechild/providers_wiring.go:100-135` - keysWire decoder

## Interface Contracts (From Parent)

### What This Leaf Exposes

e2e scenario ids: rumble-chat-completions-01, rumble-partial-stream-01,
rumble-template-kwargs-01, rumble-keys-selectors-01.

### What This Leaf Consumes

- C1 STTProvider, C2 ProviderConfig, C3 request shape, C4 keys frame, C5 decorator

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | chat.completions e2e | Create: `e2e/suites/voice-lifecycle/rumble_streaming_test.go` | `go test -tags e2e ./e2e/suites/voice-lifecycle/ -run TestRumbleChatCompletionsE2E -count=1` | wrong path/body | PASS | Given fake vLLM chat server + keys stt_vendor=rumble; When call opens and PCM sent; Then server sees audio_url body and a Final transcript arrives |
| T2 | partial stream e2e | Modify same file | `go test -tags e2e ./e2e/suites/voice-lifecycle/ -run TestRumblePartialStreamE2E -count=1` | undefined decorator | PASS | Given stt_partial_ms=300 + fake server; When PCM arrives over time; Then partial transcripts arrive before close and one final arrives after |
| T3 | kwargs e2e | Modify same file | `go test -tags e2e ./e2e/suites/voice-lifecycle/ -run TestRumbleTemplateKwargsE2E -count=1` | undefined C2/C4 fields | PASS | Given keys selectors; When POST; Then chat_template_kwargs matches |
| T4 | Register scenarios | Modify: `e2e/manifest.json` | `python3 -c 'import json;json.load(open("e2e/manifest.json"))'` | n/a | PASS | Given manifest; When parsed; Then all 4 ids present |

T1-T3 detail: mirror the prior tree's compact `TestVoiceKeysRumble01` shape -
fake loopback HTTP server, construct via `providers.NewSTT` with C2 selectors,
feed PCM, assert wire body and transcripts. Keep hermetic (no real vLLM, no
network beyond loopback, no credentials).

## Self-Verification Checklist

- F1/F2/F3/F5 covered; manifest parses
- No implementation files touched; no line-number corruption; no commit

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- Scenarios registered; build tag e2e; manifest path_map valid
- Tests hermetic; no implementation code; no gate weakening

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not touch the prior tree's `TestRumbleSTTStream` or `TestVoiceKeysRumble01`.
The streaming probe gate (`TestRumbleStreamingProbe`) stays skipped: this tree
adds client-side partials, not server-side incremental input.
