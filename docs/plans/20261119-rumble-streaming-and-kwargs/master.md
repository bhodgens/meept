# Rumble streaming + chat_template_kwargs - Implementation Orchestrator

> **For the executing agent:** You are the root orchestrator. Dispatch leaf
> implementations, review in-session, re-dispatch on gaps, and track completion.
> Do NOT implement code. Do NOT commit until a child passes review.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 6 documents (2 test gates, 4 implementation leaves)
- **Scope:** Fix the Rumble vLLM request-format bug, add generic provider-agnostic
  client-side pseudo-streaming, and expose `chat_template_kwargs` through
  `ProviderConfig`.

## Goal

The Rumble adapter shipped in `20261118-rumble-stt-provider` posts a JSON body
`{"model","file"}` to `/v1/audio/transcriptions`. vLLM's transcriptions endpoint
requires multipart/form-data, and the Rumble model card documents
`chat.completions` with an `audio_url` data URI instead. The fake gate server
accepted the JSON body, so the bug passed the gate and is only visible against
a real vLLM server. Fix the transport first.

Then add a PROVIDER-AGNOSTIC client-side pseudo-stream decorator that snapshots
a growing PCM buffer into successive complete utterances and emits
`Transcript{Final:false}` partials followed by exactly one `Final:true`. The
decorator depends only on the `STTProvider` interface and a base factory, so it
works for whisper, rumble, or any future batch vendor. It is strictly opt-in
(`PartialIntervalMs > 0`); the default path stays byte-identical.

Finally expose the model card's `chat_template_kwargs` controls (`language`,
`digits`, `filler`, `verbatim`) through `ProviderConfig`, the C1 keys frame, and
operator config, so they reach the vLLM request body.

## Architecture

`NewSTT` becomes a two-stage factory: `newBaseSTT` performs the original vendor
switch, and `NewSTT` optionally wraps the result in `PseudoStreamSTT`. The
decorator calls `newBaseSTT` afresh per snapshot, because vendor `Stream`
sessions are stateful. No new wire frame, bus topic, or config enum is added;
new keys-frame fields are additive and absent-on-wire means legacy behavior.
The transport change stays inside `stt_rumble.go`; config plumbing stays in
`providers_wiring.go`, `voice_keys.go`, and `internal/config/schema.go`.

## Interface Contracts

### C1: STTProvider / Transcript (existing, unchanged)

```
// internal/voicechild/pipeline.go:24-26,45-49
type STTProvider interface {
    Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error)
}
type Transcript struct { Text string; Final bool }
// Owner: existing code. Consumers: 01, 02.
```

### C2: ProviderConfig additive fields (owner 01)

```
// internal/voicechild/providers_wiring.go - ProviderConfig gains ONLY these:
ChatTemplateLanguage string  // chat_template_kwargs.language (language NAME, e.g. "English"); empty = omit
ChatTemplateDigits   *bool   // chat_template_kwargs.digits; nil = omit (server default true)
ChatTemplateFiller   *bool   // chat_template_kwargs.filler; nil = omit (server default false)
ChatTemplateVerbatim *bool   // chat_template_kwargs.verbatim; nil = omit (server default true)
PartialIntervalMs    int     // >0 = wrap the base STT provider in PseudoStreamSTT; 0 = legacy
// Consumers: 01 (wire body), 02 (decorator), 03 (config), gates.
```

### C3: Rumble request/response (owner 01)

```
POST <BaseURL>/v1/chat/completions
Content-Type: application/json ; NO Authorization header
{
  "model": "<cfg.Model or Reson8/Rumble-2-Flash>",
  "messages": [{"role":"user","content":[
    {"type":"audio_url","audio_url":{"url":"data:audio/wav;base64,<WAV>"}}
  ]}],
  "temperature": 0,
  "chat_template_kwargs": { ...only fields explicitly set in C2... }
}
Response: {"choices":[{"message":{"content":"<transcript>"}}]}
// Source: https://huggingface.co/Reson8/Rumble-2-Flash (model card quickstart)
// Consumers: 01 (implement), 00T/00Te (gate tests).
```

### C4: keys-frame additive fields (owner 01)

```
// BOTH internal/daemon/voice_keys.go keysWire AND
// internal/voicechild/providers_wiring.go keysWire / KeysFrame gain:
ChatTemplateLanguage string `json:"chat_template_language,omitempty"`
ChatTemplateDigits   *bool  `json:"chat_template_digits,omitempty"`
ChatTemplateFiller   *bool  `json:"chat_template_filler,omitempty"`
ChatTemplateVerbatim *bool  `json:"chat_template_verbatim,omitempty"`
STTPartialMs         *int   `json:"stt_partial_ms,omitempty"`
// Absent on wire -> zero/nil = legacy. Consumers: 01, 03.
```

### C5: pseudo-stream decorator (owner 02)

```
type STTBaseFactory func(cfg ProviderConfig, key string) (voicechild.STTProvider, error)
func newPseudoStreamSTT(cfg ProviderConfig, key string, base STTBaseFactory) (voicechild.STTProvider, error)
// NewSTT: base, err := newBaseSTT(cfg,key); if cfg.PartialIntervalMs>0 wrap, else return base.
// Intermediate snapshots: Transcript{Text:text, Final:false}
// Final (pcm close or FlushNow kick): exactly one Transcript{Final:true}
// Fresh base provider per snapshot; fail-soft; no I/O under mutex.
// Consumers: 02 (implement), 00T/00Te (gate tests).
```

### C6: operator config (owner 03)

```
// internal/config/schema.go VoiceConfig gains:
ChatTemplateLanguage string `json:"chat_template_language"`
ChatTemplateDigits   *bool  `json:"chat_template_digits,omitempty"`
ChatTemplateFiller   *bool  `json:"chat_template_filler,omitempty"`
ChatTemplateVerbatim *bool  `json:"chat_template_verbatim,omitempty"`
STTPartialMs         int    `json:"stt_partial_ms"`
// config/meept.json5 gains a commented additive block. Consumers: 03, 01 (producer).
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-unit-tests.md | leaf (test gate) | none | 40K | A |
| 00Te | 00Te-e2e-tests.md | leaf (test gate) | none | 40K | A |
| 01 | 01-request-and-wiring.md | leaf | gates validated | 70K | B |
| 02 | 02-pseudostream.md | leaf | 01 | 65K | C |
| 03 | 03-config.md | leaf | gates validated | 40K | C |
| 04 | 04-docs.md | leaf | gates validated | 35K | C |

Same-letter children are file-disjoint and dispatch together. 02 depends on
01 because it consumes C2/C5 field names and the `newBaseSTT` seam; 03 and 04
are file-disjoint from 01 and 02 and may dispatch with either wave.

## Dispatch Protocol

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T + 00Te together. Brief: tests ONLY, RED for
missing-implementation reasons, no implementation, no commit. Validate
in-session (never delegate review) against the Test Gate matrix; apply the
delta protocol; commit gate tests; mark gates TESTS_VALIDATED.

### Phase 1: Fix the bug first (child 01 alone)

Dispatch 01 by itself. It owns `stt_rumble.go`, `providers_wiring.go`, and
`voice_keys.go`. Verify the request-shape gate goes GREEN against a fake
chat.completions server. Commit before any other implementation leaf.

### Phase 2: Parallel children 02, 03, 04

Dispatch 02 (decorator + `NewSTT` refactor), 03 (config), 04 (docs/AGENTS)
together. Their write sets do not intersect. 02 reuses C2/C5 names pinned above.

### Phase 3: Review and commit each child

Review in-session (main model, never a delegated reviewer). Check leaf spec,
contracts, Review Checklist; run tests; gate test files must show no diff.
Pass: commit + status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles.

### Phase 4: Integration Review

After ALL children REVIEWED: verify contracts, run full suite and voice e2e
areas, check no import cycles. Pass: gofmt, drift checks, commit integration,
set children COMPLETE.

## Review Checklist

- [ ] All tasks from the leaf document implemented
- [ ] Interface contracts satisfied (signatures, types, keys, paths)
- [ ] Files at exact specified paths; gate tests untouched
- [ ] No JSON/base64/audio body logged; no Authorization header
- [ ] Default path byte-identical when all new selectors are zero/nil
- [ ] No debug artifacts or line-number corruption
- [ ] gofmt clean; no new dependency

## Coding Conventions

- **Language/Framework:** Go 1.27, stdlib `net/http`/`encoding/json`; no new dependency.
- **Naming:** `PseudoStreamSTT`, `newPseudoStreamSTT`, `newBaseSTT`, `STTBaseFactory`.
- **Error handling:** wrap `fmt.Errorf("...: %w", err)`; constructor errors fail only the call.
- **Testing:** table-driven; gate tests in `internal/voicechild/providers/` and `e2e/suites/voice-lifecycle/`.
- **Secrets:** keyless vendor; never log request body, base64 audio, or keys.
- **Mutex scope:** no I/O under mutex (`mutexio` analyzer).
- **Wire:** additive keys fields use `omitempty`; absent means legacy.

## Requirement Traceability

| Req | Source (user words) | Plan element | Verified by |
|-----|---------------------|--------------|-------------|
| R1 | "fix the bug first" | Phase 1 child 01 / C3 | TestRumbleRequestShape |
| R2 | "client-side psuedo-streaming so that it's provider agnostic" | 02 / C5 | TestPseudoStreamEmitsPartialsThenFinal |
| R3 | "Expose chat_template_kwargs (language, digits, filler, verbatim) through ProviderConfig (model card)" | 01 C2/C3 + 03 C6 | TestRumbleTemplateKwargs |
| R4 | "Do this via a hierarchial plan - create the plan then execute" | this tree | structural compliance scan + execution |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-unit-tests | TESTS_VALIDATED | 1 | Commit 0f3cfd69; RED transport/symbols, F4 guardrail PASS. |
| 00Te-e2e-tests | TESTS_VALIDATED | 1 | Commit 0f3cfd69; compile-RED on C2 fields, 4 scenarios registered. |
| 01-request-and-wiring | REVIEWED | 1 | Commit abbcd934; chat.completions body + kwargs + keys wiring; all gates GREEN, hooks green. |
| 02-pseudostream | REVIEWED | 1 | Commit 7637b300; generic decorator, all gates GREEN. |
| 03-config | REVIEWED | 1 | Commit 627ab169; C6 fields + daemon emit, package tests GREEN. |
| 04-docs | REVIEWED | 1 | Commit 2edf7e8e; docs + AGENTS invariants updated. |

Gate rule: implementation children stay PENDING until 00T + 00Te reach TESTS_VALIDATED.

## Integration Test Plan

- `go build ./cmd/meept-voice ./cmd/meept-daemon`
- `go test -p 2 -count=1 ./internal/voicechild/... ./internal/voice/... ./internal/daemon/... ./internal/config/...`
- `make e2e-fast-area AREA=voice-lifecycle`
- `make e2e-fast-area AREA=voice-local`
- `go test -tags e2e ./e2e/suites/voice-lifecycle/ -run 'Rumble|Partial' -v`
- Manual (deferred live pilot): real vLLM Rumble server; partials arrive before
  the utterance ends; `chat_template_kwargs` affect output; whisper default
  unchanged when selectors absent.

## Test Gate

Test leaves: `00T-unit-tests.md`, `00Te-e2e-tests.md` - Phase 0, validated
in-session.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | Rumble posts chat.completions audio_url, parses choices | 01 Task 1 | TestRumbleRequestShape | rumble-chat-completions-01 | PENDING |
| F2 | Generic pseudo-stream emits partials then one final | 02 Task 1 | TestPseudoStreamEmitsPartialsThenFinal | rumble-partial-stream-01 | PENDING |
| F3 | chat_template_kwargs reach request body | 01 Task 2 / 03 | TestRumbleTemplateKwargs | rumble-template-kwargs-01 | PENDING |
| F4 | Default path byte-identical (selectors absent) | 02 Task 2 | TestNewSTTDefaultNoDecorator | existing keys-providers-01 | PENDING |
| F5 | keys frame carries additive selectors | 01 Task 3 | TestKeysFrameRumbleSelectors | rumble-keys-selectors-01 | PENDING |

Statuses: PENDING | DELTA | VALIDATED. Delta log: none yet.

## Frozen Documents

Leaves freeze at dispatch. Corrections ride dispatch context as dated addenda.

## Open Questions

- Live vLLM verification of `chat_template_kwargs` remains a manual pilot; the
  gate uses a fake chat.completions server with the exact model-card body shape.
- Native streaming vendors (deepgram) already emit partials; enabling
  `stt_partial_ms` on them double-partials. Document: opt in only for batch vendors.

## Completion Report (2026-11-19)

All six children land COMPLETE. Commits: c490eaf3 (plan), 0f3cfd69
(Phase 0 gates), abbcd934 (01 bug fix), 7637b300 (02 decorator),
627ab169 (03 config), 2edf7e8e (04 docs).

Integration gates run in the worktree at final HEAD:
- `go build ./cmd/meept-voice ./cmd/meept-daemon` - PASS.
- `go test -p 2 -count=1 ./internal/voicechild/... ./internal/voice/...
  ./internal/daemon/... ./internal/config/...` - all 8 packages PASS.
- Rumble e2e gates: chat-completions / partial-stream / kwargs / keys
  PASS. The prior tree's `TestVoiceKeysRumble01` fake server was updated
  from the old transcriptions shape to the corrected chat.completions shape
  (integration fix; same stale-test class as the unit gate).
- `make e2e-fast-area AREA=voice-local` - PASS (40.6 s).
- `make e2e-fast-area AREA=voice-lifecycle` - the Rumble tests all PASS,
  but the suite is RED on PRE-EXISTING `TestVoiceKeysToProviders` ("fake
  qwen3 endpoint never served a synthesis request within 15s"). This test
  was proven to fail identically at base commit 9157700f in the prior tree;
  it is unrelated to this change and belongs to another lane. Not acted on.

Resolved bugs and observations during execution:
- ORIGINAL BUG FIXED: the shipped adapter posted JSON to
  `/v1/audio/transcriptions`; vLLM needs multipart there and the model card
  documents chat.completions with audio_url. The transport now matches the
  model card; the prior gate test that encoded the bug was updated.
- Leaf 01 placed C4 fields in `internal/voicechild/keys.go` (KeysFrame +
  applyKeys), which the plan mislocated in providers_wiring.go. Accepted as
  a necessary correction; kwargs now reach the adapter in production.
- A mutexio gate finding in a gate-test helper (Header.Get under lock) was
  fixed by moving the read outside the critical section.
- Leaf 02 buffered the pseudo-stream out-channel (cap 16) to avoid a
  deadlock when a downstream consumer pauses reading during paced PCM feed.
- Pseudo-streaming is CLIENT-SIDE snapshots, not server incremental input;
  it does not reduce time-to-first-word on a batch model. Do not enable on
  native-partial vendors (deepgram). Live vLLM pilot remains deferred.

## Notes

- The prior tree's F4 skip (`TestRumbleStreamingProbe`) stays intact and is NOT
  edited by this tree. This tree adds client-side partials, not server streaming.
- Companion tree `20261118-rumble-stt-provider/` is already merged to main;
  this tree is additive on top of it.
- Evaluation record: `docs/research/rumble2flash-evaluation.md`.
