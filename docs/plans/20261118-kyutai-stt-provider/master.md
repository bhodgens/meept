# Kyutai STT provider - Root Orchestrator

> **For the executing agent:** You are the root orchestrator. Dispatch leaf
> implementations, review in-session, re-dispatch on gaps, track completion.
> Do NOT implement code. Do NOT commit until a child passes review.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 4 documents (1 test gate leaf, 3 implementation leaves)
- **Scope:** Add Kyutai STT 1B as an additive, opt-in STT provider for the
  voice local leg, supplemental to whisper. Whisper stays the default.

## Goal

Reduce the local leg's time-to-`heard` by offering a native streaming STT
provider with semantic VAD and a ~125 ms flush. Kyutai STT 1B streams words
~500 ms after speech and replaces meept's fixed 500 ms silence wait + 900 ms
`turn_confirm_ms` aggregation for calls that opt in. Whisper remains the
default; no existing configuration changes behavior.

Ground truth: `docs/research/realtime-stt-options.md` ranks Kyutai #1 for
latency (native streaming, official MLX on Mac, Rust WebSocket server for
socrates, CC BY 4.0 weights / MIT+Apache-2.0 code, en/fr only).

## Architecture

Kyutai runs as a self-hosted WebSocket server (Rust on socrates, MLX on Mac).
The child-side provider opens one WS connection per call, streams raw PCM,
and emits `Transcript{Text, Final}` deltas through the existing
`STTProvider.Stream` seam. No daemon code changes: the C1 keys line already
carries `stt_vendor`/`stt_model`/`stt_base_url` generically. A new vendor case
in `NewSTT` constructs the adapter. `stt_provider: "kyutai"` in config opts in;
empty/absent keeps the current provider.

## Interface Contracts

### C1: STTProvider (existing, unchanged)

```
// internal/voicechild/pipeline.go:24-26
type STTProvider interface {
    Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error)
}
// Owner: existing code (no change). Consumers: 01-kyutai-adapter.md
```

### C2: Transcript (existing, unchanged)

```
// internal/voicechild/pipeline.go:45-49
type Transcript struct { Text string; Final bool }
// Owner: existing code. Consumers: 01-kyutai-adapter.md
```

### C3: Factory seam (existing, add one case)

```
// internal/voicechild/providers/stt_deepgram.go:154-172
func NewSTT(cfg ProviderConfig, key string) (voicechild.STTProvider, error)
// Owner: existing code. Consumers: 01-kyutai-adapter.md (adds case "kyutai")
// ProviderConfig: internal/voicechild/providers_wiring.go:17-24
//   { Vendor, Model, BaseURL, SampleRate, ... }
```

### C4: Keys frame (existing, no change)

```
stt_vendor / stt_model / stt_base_url ride the C1 keys line:
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
// Owner: existing code. Consumers: 03-docs-config.md (document "kyutai" value)
// No new config fields. No enum validation to extend.
```

## Open Questions

- **Kyutai wire protocol** — exact WS path and JSON frame shape must be
  confirmed from official docs before 00T authors the fake server. If it
  differs from `{type:partial|final,text}`, the control agent updates the
  gate test via the delta protocol with the source URL.
- **WebSocket dependency** — resolved: `github.com/gorilla/websocket` is
  already in `go.mod:30`. Leaf 01 uses it; no new dependency needed.
- **Live pilot** — socrates Kyutai server install is a manual step outside
  this tree; CI stays hermetic with a fake WS server.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-tests.md | leaf (test gate) | none | 45K | A |
| 01 | 01-kyutai-adapter.md | leaf | 00T validated | 80K | B |
| 02 | 02-registration-docs.md | leaf | 00T validated | 45K | B |
| 03 | 03-config-e2e.md | leaf | 00T validated; 01 for run | 55K | B |

Same-letter children are file-disjoint and dispatch together. 01 owns
`stt_kyutai.go`; 02 owns `stt_deepgram.go` (switch), `providers.go` (pin),
`main.go` comment, docs, and AGENTS.md; 03 owns e2e scenario + manifest.
Note: 01 and 02 both touch `stt_kyutai_test.go`? No — 00T owns all test
files; 01/02 must not create or edit gate tests.

## Dispatch Protocol

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T-tests.md first. Brief: write tests ONLY, fail RED for
missing-implementation reasons, do NOT write implementation, do NOT commit.
Validate in-session (never delegate review) against the Test Gate matrix;
apply the delta protocol; commit the gate tests; mark 00T TESTS_VALIDATED.
Only then dispatch implementation leaves 01/02/03.

### Phase 1: Dispatch Concurrency Group B

Dispatch 01, 02, 03 simultaneously via `delegate_task`: goal = "Implement all
tasks from [leaf]"; context = full leaf text + contracts + conventions +
relevant source INLINED; include no-commit, no-git-add, no-read_file,
write-once-and-stop rules. Each leaf stays inside its declared write set.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run tests; `git diff` on gate test files
must be empty. Pass: commit + status REVIEWED. Gaps: re-dispatch with
findings, max 3 cycles, then escalate.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts, run the full suite and the
voice e2e area, check no import cycles. Gaps: re-dispatch the failing child.
Pass: gofmt, run drift checks, commit integration changes, set children
COMPLETE, report COMPLETE.

## Review Checklist

- [ ] All tasks from the leaf document implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; gate test files show no diff
- [ ] New tests written and passing (TDD followed)
- [ ] Code follows project conventions; no scope creep
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go 1.27, stdlib + `gorilla/websocket` if already
  a module dependency; do not add a new dependency without orchestrator OK.
- **Naming:** Exported types/functions PascalCase; vendor const
  `STTVendorKyutai`; unexported helpers camelCase.
- **Error handling:** wrap with `fmt.Errorf("kyutai stt: ...: %w", err)`;
  constructor errors fail only the call, never the child (C3 posture).
- **Testing:** table-driven where natural; tests in
  `internal/voicechild/providers/`; new-feature e2e in `e2e/suites/`.
- **Secrets:** no keys in logs; `KeysFrame.String/LogValue` redacts.
- **Formatting:** gofmt before reporting.
- **Mutex scope:** no I/O under mutex (repo `mutexio` analyzer).

## Requirement Traceability

| Req | Source (user words) | Plan element | Verified by |
|-----|---------------------|--------------|-------------|
| R1 | "create a hierarchial plan to implement kyutai as another frontend option" | master + 01 + 02 | `NewSTT` case + e2e |
| R2 | "supplemental to whisper, and default" | 02 config docs; C5 | whisper default unchanged test |
| R3 | "near-realtime audio" / time-to-`heard` | 01 streaming adapter | WS delta e2e timing |
| R4 | English focus | 03 e2e English prompt | WER/smoke scenario |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests | PENDING | 0 | |
| 01-kyutai-adapter | PENDING | 0 | |
| 02-registration-docs | PENDING | 0 | |
| 03-config-e2e | PENDING | 0 | |

Gate rule: implementation children stay PENDING until 00T reaches TESTS_VALIDATED.

## Integration Test Plan

- `go test -p 2 -count=1 ./internal/voicechild/... ./internal/voice/... ./internal/daemon/...`
- `make e2e-fast-area AREA=voice-lifecycle`
- `make e2e-fast-area AREA=voice-local`
- `go build ./cmd/meept-voice`
- Manual: `stt_provider: "kyutai"` + `stt_base_url` -> local call prints
  `heard` within ~500 ms of speech; unset -> whisper unchanged.

## Test Gate

Test leaf: `00T-tests.md` — dispatched in Phase 0, validated by the control
agent in-session.

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Kyutai adapter streams partial + final transcripts | 01 Task 1 | TestKyutaiSTTStream | kyutai-ws-01 | PENDING |
| F2 | `NewSTT` constructs kyutai from keys frame | 02 Task 1 | TestNewSTT_Kyutai | voice-keys-kyutai-01 | PENDING |
| F3 | Whisper remains default when selector unset | 02 Task 2 | TestNewSTT_DefaultUnchanged | existing voice-keys-providers-01 | PENDING |
| F4 | BaseURL required, keyless construction | 01 Task 2 | TestNewKyutai_BaseURLRequired | n/a - constructor guard | PENDING |
| F5 | Clear/cancel closes WS (barge-in) | 01 Task 3 | TestKyutaiCancelClosesWS | voice-bargein kyutai variant | PENDING |

Statuses: PENDING | DELTA | VALIDATED.

Delta log: none yet.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf mid-execution.

## Notes

- Kyutai weights are CC BY 4.0 (en/fr only); document this limitation.
- No daemon code changes: the keys seam is generic.
- A sibling tree `docs/plans/20261118-voice-tts-stt-optimizations/` exists;
  this tree is self-contained and does not reference or modify it.
