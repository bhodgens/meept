# Rumble-2-Flash STT provider - Root Orchestrator

> **For the executing agent:** You are the root orchestrator. Dispatch leaf
> implementations, review in-session, re-dispatch on gaps, track completion.
> Do NOT implement code. Do NOT commit until a child passes review.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 5 documents (2 test-gate leaves, 3 implementation leaves)
- **Scope:** Add Rumble-2-Flash as an additive, opt-in STT provider for the
  voice sidecar, with a supervised vLLM sidecar and a batch-first adapter.
  Streaming is investigated as an optional final leaf, not assumed.

## Goal

Offer Rumble-2-Flash (0.5B, Apache-2.0, English WER 6.60) as an accuracy-
focused STT option on the voice local leg, running on socrates' RTX 3060
beside qwen3-tts. Whisper stays the default; no existing config changes
behavior. The primary path is whole-buffer transcription via a warm vLLM
server (removes the cold `whisper-cli` subprocess). Streaming is explicitly
investigated as a research gate: `vllm stream=True` emits output tokens only
after the full audio arrives, so it is NOT incremental input. If no genuine
incremental path exists, the adapter ships batch-only with one `Final:true`
per utterance.

Ground truth: `docs/research/rumble2flash-review.md` (`964fd536`),
`docs/research/qwen3-asr-06b-review.md`, evaluation issue #161.

## Architecture

Rumble runs as a self-hosted vLLM server (`vllm serve Reson8/Rumble-2-Flash
--max-model-len 4096 --gpu-memory-utilization 0.20-0.25`). The child-side
provider posts buffered PCM (one utterance) as base64 `audio_url` to
`/v1/audio/speech` chat completions, or to `/v1/audio/transcriptions`, and
emits one `Transcript{Text, Final:true}` through the existing
`STTProvider.Stream` seam. The daemon spawns/supervises the vLLM sidecar via
the same runtime-lifecycle pattern as local LLM runtimes (spawn guards,
supervisor, no spawn into served endpoint). No new wire frame or bus topic.

## Interface Contracts

### C1: STTProvider (existing, unchanged)

```
// internal/voicechild/pipeline.go:24-26
type STTProvider interface {
    Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error)
}
// Owner: existing code. Consumers: 01-rumble-adapter.md
```

### C2: Transcript (existing, unchanged)

```
// internal/voicechild/pipeline.go:45-49
type Transcript struct { Text string; Final bool }
// Owner: existing code. Consumers: 01-rumble-adapter.md
```

### C3: Factory seam (existing, add one case)

```
// internal/voicechild/providers/stt_deepgram.go:154-172
func NewSTT(cfg ProviderConfig, key string) (voicechild.STTProvider, error)
// ProviderConfig: internal/voicechild/providers_wiring.go:17-24
// Owner: existing code. Consumers: 01-rumble-adapter.md (adds case "rumble")
```

### C4: Keys frame (existing, no change)

```
stt_vendor / stt_model / stt_base_url ride C1 keys line:
  daemon producer: internal/daemon/voice_keys.go:45-47,167-168,206
  child consumer:  internal/voicechild/providers_wiring.go:89-91,177-179
  applyKeys:       internal/voicechild/keys.go:182-234
// Owner: existing code. Consumers: all leaves (reuse, do not redeclare)
```

### C5: Config selector (existing fields only)

```
voice.stt_provider : string  // config/schema.go:5634 (global), 6176 (local)
voice.stt_base_url : string  // config/schema.go:5818 (global only)
voice.stt_model    : string  // config/schema.go:5816
// Owner: existing code. Consumers: 03-registration-docs.md
// No new config fields. No enum validation to extend.
```

### C6: Sidecar runtime lifecycle (existing pattern, new endpoint)

```
vLLM Rumble sidecar: spawn via RuntimeProcess with supervisor;
refuse spawn if endpoint already listening; health /health;
startup orphan sweep only reaper; auto_stop_on_exit.
// Owner: existing runtime code. Consumers: 02-sidecar-wiring.md
```

## Open Questions

- **Endpoint shape** — finalize whether vLLM serves Rumble at
  `/v1/audio/transcriptions` or only `chat.completions` with `audio_url`.
  Gate 00T assumes `/v1/audio/transcriptions`; if wrong, control agent
  updates the test via the delta protocol with the source URL.
- **Streaming (R2)** — documented as batch-only. Gate 00Te records the F4
  decision. If a genuine incremental path is later confirmed, it becomes a
  new leaf; it is not built into leaf 01.
- **Live pilot** — socrates vLLM install and GPU cap tuning are manual,
  outside this hermetic tree.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-unit-tests.md | leaf (test gate) | none | 45K | A |
| 00Te | 00Te-e2e-tests.md | leaf (test gate) | none | 45K | A |
| 01 | 01-rumble-adapter.md | leaf | gates validated | 80K | B |
| 02 | 02-sidecar-wiring.md | leaf | gates validated | 70K | B |
| 03 | 03-registration-docs.md | leaf | gates validated | 40K | B |

Same-letter children are file-disjoint: 01 owns `stt_rumble.go`; 02 owns
runtime wiring/config template; 03 owns `NewSTT` switch, `providers.go` pin,
docs and AGENTS.md. Neither 01/02/03 creates gate tests.

## Dispatch Protocol

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T + 00Te together. Brief: tests ONLY, RED for
missing-implementation reasons, no implementation, no commit. Validate
in-session (never delegate review) against the Test Gate matrix; apply the
delta protocol; commit gate tests; mark gates TESTS_VALIDATED.

### Phase 1: Dispatch Concurrency Group B

Dispatch 01, 02, 03 simultaneously via `delegate_task`: goal = "Implement all
tasks from [leaf]"; context = full leaf text + contracts + conventions +
relevant source INLINED; no-commit, no-git-add, no-read_file,
write-once-and-stop rules. Each leaf stays inside its declared write set.

### Phase 2: Review and Commit Each Child

Review in-session (main model - never delegated reviewer). Check leaf spec,
contracts, Review Checklist; run tests; gate test files must show no diff.
Pass: commit + status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts, run full suite and voice e2e
areas, check no import cycles. Pass: gofmt, drift checks, commit integration,
set children COMPLETE.

## Review Checklist

- [ ] All tasks from the leaf document implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] Files at exact specified paths
- [ ] Gate tests GREEN; gate test files show no diff
- [ ] No debug artifacts or line-number corruption
- [ ] No scope creep; vLLM memory cap explicit (never default 0.92)

## Coding Conventions

- **Language/Framework:** Go 1.27, stdlib net/http; no new dependency.
- **Naming:** `STTVendorRumble`, `RumbleSTT`, `newRumbleSTT`.
- **Error handling:** wrap `fmt.Errorf("rumble stt: ...: %w", err)`;
  constructor errors fail only the call.
- **Testing:** table-driven; tests in `internal/voicechild/providers/`;
  new e2e in `e2e/suites/`.
- **Secrets:** keyless; no keys in logs.
- **Mutex scope:** no I/O under mutex (`mutexio` analyzer).
- **GPU safety:** explicit `--gpu-memory-utilization 0.20-0.25`, never 0.92.

## Requirement Traceability

| Req | Source (user words) | Plan element | Verified by |
|-----|---------------------|--------------|-------------|
| R1 | "implement rumble-2-flash as a voice sidecar stt" | 01 + 02 + 03 | adapter + e2e |
| R2 | "with streaming (if possible)" | 00Te T4 + 05-streaming-probe (deferred research leaf) | gate T4 result |
| R3 | "additive" (implied by evaluation set) | whisper default unchanged | guardrail test |
| R4 | shared 3060 with qwen3-tts | 02 GPU cap | runtime config pin |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-unit-tests | TESTS_VALIDATED | 1 | Authored in-session; RED on undefined newRumbleSTT/DefaultRumbleRuntimeConfig, not syntax. Commit 9d3ef25b. |
| 00Te-e2e-tests | TESTS_VALIDATED | 1 | Authored in-session; keys gate RED on unknown vendor; F4 honest skip cites research:45. Commit 9d3ef25b. |
| 01-rumble-adapter | PENDING | 0 | dispatched |
| 02-sidecar-wiring | PENDING | 0 | dispatched |
| 03-registration-docs | PENDING | 0 | dispatched |

Gate rule: implementation children stay PENDING until 00T + 00Te reach TESTS_VALIDATED.

## Integration Test Plan

- `go test -p 2 -count=1 ./internal/voicechild/... ./internal/voice/... ./internal/daemon/...`
- `make e2e-fast-area AREA=voice-lifecycle`
- `make e2e-fast-area AREA=voice-local`
- `go build ./cmd/meept-voice ./cmd/meept-daemon`
- Manual: `stt_provider: "rumble"` + base URL -> local call prints `heard`
  with WER better than whisper; unset -> whisper byte-identical.

## Test Gate

Test leaves: `00T-unit-tests.md`, `00Te-e2e-tests.md` — Phase 0, validated
in-session. Streaming probe (R2) is a DEFERRED research leaf after batch
ships; the gate records whether genuine incremental input exists.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | Adapter posts buffered PCM, emits Final:true | 01 Task 2 | TestRumbleSTTStream | rumble-stt-01 | VALIDATED |
| F2 | NewSTT constructs rumble from keys frame | 03 Task 1 | TestNewSTT_Rumble | voice-keys-rumble-01 | VALIDATED |
| F3 | Whisper default unchanged | 03 Task 2 | TestNewSTT_DefaultUnchanged | existing keys-providers-01 | VALIDATED |
| F4 | Streaming capability decision recorded | 00Te T4 | n/a - research | TestRumbleStreamingProbe (skip with reason if unsupported) | VALIDATED (batch-only; skip cites research:45) |
| F5 | GPU memory cap explicit in runtime config | 02 Task 1 | TestRumbleRuntimeConfigCap | n/a - config assertion | VALIDATED |

Statuses: PENDING | DELTA | VALIDATED. Delta log: none yet.

## Frozen Documents

Leaves freeze at dispatch. Corrections ride dispatch context as dated addenda.

## Notes

- Rumble is batch as documented; `vllm stream=True` streams output tokens
  only after full audio. Do not claim time-to-first-word improvement.
- Sidecar spawn must follow runtime lifecycle invariants (spawn guards,
  supervisor, orphan sweep) from `internal/daemon/AGENTS.md`.
- Companion tree `20261118-kyutai-stt-provider/` is self-contained; no
  cross-tree references. Evaluation issue #161.
