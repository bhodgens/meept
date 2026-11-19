# Rumble native streaming - E2E Test Gate Leaf

> **For the implementing agent:** Write TESTS ONLY for F1-F3. Do NOT write
> implementation. Do NOT commit. Tests must fail RED for
> missing-implementation reasons. Explore with search_files/terminal.
> Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Hermetic e2e gates for native Rumble WS streaming
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A
- **Audit references:** F1, F2, F3

## Goal

Provide hermetic e2e coverage proving the keys-frame vendor=rumble path
constructs the native streaming adapter and that partial/final events reach
the pipeline. No real vLLM or socrates dependency.

## Context

- `e2e/suites/voice-lifecycle/rumble_streaming_test.go` - prior tree's gates
  (stale chat.completions shape; rewrite to native WS)
- `e2e/suites/voice-lifecycle/rumble_stt_test.go` - prior keys gate (stale)
- `e2e/manifest.json` - scenario registry
- Contracts C1-C5 in ../master.md

## Verified Anchors

- `e2e/manifest.json` - voice-keys-rumble-01 and rumble-* scenarios
- `internal/voicechild/providers/stt_deepgram.go:85-90` - wsutil dial seam

## Interface Contracts (From Parent)

### What This Leaf Exposes

e2e scenario ids: rumble-native-stream-01, rumble-native-flush-01,
rumble-native-factory-01.

### What This Leaf Consumes

- C1 STTProvider, C2 WS protocol, C4 factory, C5 config

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Native stream e2e | Rewrite: `e2e/suites/voice-lifecycle/rumble_streaming_test.go` | `go test -tags e2e ./e2e/suites/voice-lifecycle/ -run TestRumbleNativeStreamE2E -count=1` | stale chat shape | PASS | Given fake WS server + keys stt_vendor=rumble; When PCM fed; Then partial+final transcripts arrive via NewSTT |
| T2 | Flush e2e | Modify same file | `go test -tags e2e ./e2e/suites/voice-lifecycle/ -run TestRumbleNativeFlushE2E -count=1` | no flush frame | PASS | Given pcm close; Then one empty frame and end event honored |
| T3 | Register scenarios | Modify: `e2e/manifest.json` | `python3 -c 'import json;json.load(open("e2e/manifest.json"))'` | n/a | PASS | Given manifest; When parsed; Then the 3 ids present |

T1-T2 detail: use a fake WS server via `websocket.Accept`, construct via
`providers.NewSTT(ProviderConfig{Vendor:"rumble", BaseURL: srv.URL, SampleRate:16000}, "")`,
feed int16 PCM chunks, assert event mapping and flush. Hermetic: loopback only,
no real socrates, no credentials. Replace the stale chat.completions fake server
in the existing file; remove stale tests that assert the old transport.

The prior `TestRumbleStreamingProbe` skip must be updated: it now records that
the live server supports native streaming (cite the WS endpoint and the
verified partial/final sequence), not the old "batch-only" claim. Keep it as a
decision record; do not delete coverage.

## Self-Verification Checklist

- F1-F3 covered; manifest parses; no implementation files touched
- No line-number corruption; no commit

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- Scenarios registered; build tag e2e; hermetic fake WS server
- Streaming probe decision updated honestly; no gate weakening

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not touch code files. The prior tree's `TestVoiceKeysRumble01` in
`rumble_stt_test.go` is also stale (chat.completions fake); update its fake
server to the native WS protocol in this gate leaf.
