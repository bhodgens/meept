# Rumble native streaming adapter - Implementation Orchestrator

> **For the executing agent:** You are the root orchestrator. Dispatch leaf
> implementations, review in-session, re-dispatch on gaps, and track completion.
> Do NOT implement code. Do NOT commit until a child passes review.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 5 documents (2 test gates, 3 implementation leaves)
- **Scope:** Replace the batch/vLLM Rumble adapter with a native WebSocket
  streaming adapter that consumes the live server's partial/final/end protocol,
  and switch the installed operator config from whisper to rumble.

## Goal

The prior tree shipped a batch `RumbleSTT` that POSTs a vLLM-style
`chat.completions` body. The live server on socrates:8000 is NOT vLLM: it is a
FastAPI + transformers server (`/home/caimlas/rumble-server/rumble_server.py`,
version 0.2.0) exposing:

- `POST /v1/audio/transcriptions` (multipart, one shot)
- `WS   /v1/audio/transcriptions/stream` (native incremental streaming)

Live behavior verified by the operator and by an in-session WS probe: the
client streams int16 mono 16 kHz PCM as binary frames; the server decodes the
growing window on a cadence (>=1.0 s of signal and >=0.8 s since growth),
emits self-segmented `final` events for leading words stable across consecutive
decodes, a `partial` for the changing tail, and an `end` event on flush. Silence
produces only `end` with empty text (no hallucination).

This tree makes meept consume that native protocol directly. It removes the
need for the previous client-side pseudo-stream workaround on the Rumble leg,
switches `~/.meept/meept.json5` to `stt_provider: "rumble"` with
`stt_base_url: "http://socrates:8000"`, and documents the behavior change.

## Architecture

`RumbleSTT.Stream` opens ONE WebSocket per call, pumps incoming PCM chunks as
binary frames, and reads JSON events in a separate goroutine. The pump goroutine
is the SOLE writer: `FlushNow` signals it via a buffered channel, so no mutex is
held across network I/O (mutexio invariant). Transcript mapping:
`partial` -> `Transcript{Final:false}`, `final` -> `Transcript{Final:true}`,
`end` -> emit any uncovered remainder as one final then close. Native vendors
must never be wrapped in `PseudoStreamSTT`; `NewSTT` gains a small guard.

## Interface Contracts

### C1: STTProvider / Transcript (existing, unchanged)

```
// internal/voicechild/pipeline.go:24-26,45-49
type STTProvider interface {
    Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error)
}
type Transcript struct { Text string; Final bool }
// Owner: existing code. Consumers: 01, 02, gates.
```

### C2: Native Rumble WS wire protocol (owner 01)

```
Dial:  ws://<host>/v1/audio/transcriptions/stream
       (derived from cfg.BaseURL via existing dialURL helper in stt_deepgram.go)
Send:  binary WebSocket frames, raw s16le mono 16 kHz PCM (forward the
       pipeline's 640-byte/20 ms chunks unchanged; no base64, no WAV header)
Flush: empty binary frame (or text "end"); server replies final(s) then end
Recv JSON text frames:
  {"type":"partial","text":"Yesterday it was 30."}   -> Final:false
  {"type":"final","text":"Yesterday it was"}         -> Final:true
  {"type":"end","text":"<full transcript>"}          -> terminal
Server invariants (read from live source, verified 2026-11-19):
  - decodes only after >=1.0 s audio and >=0.8 s since last growth
  - self-segmented finals = leading words stable across two decodes
  - trailing changing words are emitted as partial
  - silence returns only {"type":"end","text":""}
  - hard cap 45 s of buffered audio; language pinned "English" server-side
  - NO Authorization header, NO chat_template_kwargs on this endpoint
// Source: ssh caimlas@socrates /home/caimlas/rumble-server/rumble_server.py:105-170
// Consumers: 01 (implement), 00T/00Te (gates).
```

### C3: Transcript mapping and lifecycle (owner 01)

```
partial        -> emit Transcript{Text: text, Final: false} (skip empty)
final          -> emit Transcript{Text: text, Final: true}  (skip empty)
end            -> if text has a remainder not covered by prior finals,
                  emit one final for it; then close the out channel
pcm closed     -> send empty binary flush frame; keep reading until end/error
FlushNow()     -> signal the pump goroutine (buffered(1), non-blocking)
ctx cancelled  -> close WS, close out, no final
dial failure   -> Stream returns a named error (fails only that call)
mid-stream err -> close out silently (fail-soft, never fails the call)
out channel    -> buffered (cap 16) so a paused consumer cannot stall the pump
// Consumers: 01, gates.
```

### C4: Factory posture (owner 02)

```
// NewSTT in internal/voicechild/providers/stt_deepgram.go
base, err := newBaseSTT(cfg, key)
if err != nil { return nil, err }
if cfg.PartialIntervalMs <= 0 || nativeStreamingVendor(cfg.Vendor) {
    return base, nil
}
return newPseudoStreamSTT(cfg, key, newBaseSTT)
// nativeStreamingVendor(v) == true for "rumble" (and "deepgram", which
// already emits genuine partials). This prevents double-partialing.
// Consumers: 02, gates.
```

### C5: Operator config (owner 03; orchestrator applies to ~/.meept)

```
voice.stt_provider  = "rumble"
voice.stt_base_url  = "http://socrates:8000"
voice.stt_partial_ms = 0        (native streaming; no client-side proxy)
voice.local.stt_provider = ""   (inherits global rumble)
// chat_template_* selectors are INERT on this server (transformers path,
// not vLLM chat templates). Document, do not delete the fields.
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-unit-tests.md | leaf (test gate) | none | 45K | A |
| 00Te | 00Te-e2e-tests.md | leaf (test gate) | none | 45K | A |
| 01 | 01-native-ws-adapter.md | leaf | gates validated | 75K | B |
| 02 | 02-factory-posture.md | leaf | gates validated | 35K | C |
| 03 | 03-config-docs.md | leaf | gates validated | 40K | C |

Same-letter children are file-disjoint. 01 owns `stt_rumble.go`; 02 owns
`stt_deepgram.go` (NewSTT wrapper only); 03 owns config template, docs, and
AGENTS. Gate leaves own all test files.

## Dispatch Protocol

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T + 00Te together. Brief: tests ONLY, RED for
missing-implementation reasons. Gates MUST rewrite the stale chat.completions
tests to the native WS protocol. Validate in-session (never delegate review)
against the Test Gate matrix; commit gate tests; mark TESTS_VALIDATED.

### Phase 1: Implementation wave 1

Dispatch 01 (native WS adapter) alone. It is the core behavior change and owns
one file. Verify gates GREEN, commit.

### Phase 2: Implementation wave 2

Dispatch 02 (factory posture) and 03 (config/docs) together: disjoint write sets.

### Phase 3: Review and commit each child

Review in-session (main model, never a delegated reviewer). Check leaf spec,
contracts, Review Checklist; run tests; gate test diff must be empty.
Pass: commit + status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles.

### Phase 4: Integration and operator config

After ALL children REVIEWED: run build, bounded unit sweep, voice e2e areas.
The orchestrator then edits `~/.meept/meept.json5` directly (operator file,
outside the worktree), verifies the effective config, and merges the worktree
branch to main only after gates are green.

## Review Checklist

- [ ] All tasks from the leaf document implemented
- [ ] Interface contracts satisfied (signatures, wire keys, paths)
- [ ] Native WS protocol exact: binary PCM in, partial/final/end out
- [ ] No base64/WAV body on the WS path; no Authorization header
- [ ] No I/O under mutex; single writer goroutine; buffered out channel
- [ ] Native vendors never wrapped in PseudoStreamSTT
- [ ] Whisper default unchanged; no new dependency
- [ ] No debug artifacts or line-number corruption; gofmt clean

## Coding Conventions

- **Language/Framework:** Go 1.27, `github.com/coder/websocket` (already a dependency), no new module.
- **Reuse:** `dialURL`, `wsSession`, `wsutilSession` from `stt_deepgram.go` are in the same package; reuse, do not copy.
- **Naming:** `RumbleSTT`, `newRumbleSTT`, `rumbleStreamPath`, `nativeStreamingVendor`.
- **Error handling:** wrap `fmt.Errorf("rumble stt: ...: %w", err)`; mid-stream errors fail soft.
- **Testing:** table-driven; httptest WS server with `websocket.Accept`.
- **Secrets:** keyless; never log audio bytes or transcript bodies at Info.
- **Mutex scope:** no I/O under mutex (mutexio gate is strict).

## Requirement Traceability

| Req | Source (user words) | Plan element | Verified by |
|-----|---------------------|--------------|-------------|
| R1 | "rumble is now live ... native streaming" | 01 / C2 | TestRumbleNativeStreamEvents |
| R2 | "Configure the environment to use this instead of whisper (~/.meept)" | Phase 4 operator config | config get + live probe |
| R3 | "Make sure our code supports this capability fully" | 01 + 02 + gates | unit + e2e gates |
| R4 | "Explain how this changes the local streaming proxy workaround behavior" | 03 docs + final answer | docs/workflows/voicechild.md |
| R5 | "implement the plan as well, using subagents" | this tree | execution + commits |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-unit-tests | TESTS_VALIDATED | 1 | Commit 26790fa9; native WS RED gates, whisper guardrail PASS. |
| 00Te-e2e-tests | TESTS_VALIDATED | 1 | Commit 26790fa9; fake WS e2e RED, 3 scenarios registered. |
| 01-native-ws-adapter | REVIEWED | 1 | Commit e335ea15; native WS protocol, all gates GREEN, hooks green. |
| 02-factory-posture | REVIEWED | 1 | Commit f5e384a2; native vendors bypass proxy, gate GREEN. |
| 03-config-docs | REVIEWED | 1 | Commit f6e8d826; docs + AGENTS + template, stale comments reconciled. |

Gate rule: implementation children stay PENDING until 00T + 00Te reach TESTS_VALIDATED.

## Integration Test Plan

- `go build ./cmd/meept-voice ./cmd/meept-daemon`
- `go test -p 2 -count=1 ./internal/voicechild/... ./internal/voice/... ./internal/daemon/... ./internal/config/...`
- `make e2e-fast-area AREA=voice-lifecycle`
- `make e2e-fast-area AREA=voice-local`
- Live probe (already performed during research): WS partial/final/end sequence
  on socrates:8000; operator may re-run after config switch.

## Test Gate

Test leaves: `00T-unit-tests.md`, `00Te-e2e-tests.md` - Phase 0, validated in-session.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | Native WS binary PCM in, partial/final/end mapped | 01 / C2,C3 | TestRumbleNativeStreamEvents | rumble-native-stream-01 | PENDING |
| F2 | Flush on pcm close and FlushNow sends empty frame | 01 / C3 | TestRumbleNativeFlush | rumble-native-flush-01 | PENDING |
| F3 | Native vendor not wrapped in pseudo-stream | 02 / C4 | TestNewSTTNativeNoDecorator | rumble-native-factory-01 | PENDING |
| F4 | Silence yields only end with empty text | 01 / C2 | TestRumbleNativeSilence | n/a (fixture) | PENDING |
| F5 | Whisper default unchanged | existing | TestNewSTT_DefaultUnchanged | existing keys-providers-01 | PENDING |

Statuses: PENDING | DELTA | VALIDATED. Delta log: none yet.

## Frozen Documents

Leaves freeze at dispatch. Corrections ride dispatch context as dated addenda.

## Open Questions

- `chat_template_language/digits/filler/verbatim` are inert on the transformers
  server; they remain in config for a future vLLM deployment. Documented in 03.
- The 45 s server cap and 0.8 s decode cadence are operator-side server settings,
  not repo config. No new config fields for them in this tree.

## Completion Report (2026-11-19)

All five children COMPLETE. Commits: eae9a115 (plan), 26790fa9
(gates), e335ea15 (01 native WS adapter), f5e384a2 (02 factory guard),
f6e8d826 (03 docs/config).

Integration gates in the worktree at final HEAD:
- `go build ./cmd/meept-voice ./cmd/meept-daemon` - PASS.
- `go test -p 2 -count=1 ./internal/voicechild/... ./internal/voice/...
  ./internal/daemon/... ./internal/config/...` - all 8 packages PASS.
- Rumble native e2e gates: stream / flush / factory / keys - all PASS.
- `make e2e-fast-area AREA=voice-local` - PASS.
- `make e2e-fast-area AREA=voice-lifecycle` - Red ONLY on the PRE-EXISTING
  `TestVoiceKeysToProviders` (fake qwen3 synthesis timeout), proven at
  base commit 9157700f in an earlier tree; unrelated, left untouched.

Operator config (~/.meept/meept.json5, orchestrator-owned, outside tree):
stt_provider=rumble, stt_base_url=http://socrates:8000, stt_partial_ms=0.
Verified via rebuilt `meept config get` and live /health (model loaded,
both transcription endpoints live). Binaries rebuilt from current main.

Resolved during execution:
- Hook fixes: mutexio (header/path reads outside lock in gate fake),
  error handling (two ignored flush writes now DEBUG-logged).
- Production const named `rumbleNativeStreamPath` to avoid collision with
  the gate file's package-level `rumbleStreamPath`.
- Stale config comments about chat_template keys reconciled: they are
  inert on the transformers server; native vendors bypass the proxy.

## Notes

## Notes

- The prior tree's `TestRumbleStreamingProbe` skip cites batch-only behavior;
  with native streaming now live, that skip text is stale. Gate leaves update
  it to record the NEW decision (native streaming supported by the live server)
  instead of deleting coverage.
- The "local streaming proxy workaround" is `PseudoStreamSTT` (client-side
  snapshots). With native streaming it is bypassed for rumble: no re-POST of a
  growing buffer, no O(n^2) recompute, genuine server-stable finals for
  barge-in/endpointing, and `stt_partial_ms` set to 0.
