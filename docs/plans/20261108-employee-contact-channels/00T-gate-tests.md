# Test gate - contact channels - Implementation Leaf

> **For the implementing agent:** You are the TEST-GATE implementer for this
> leaf. Write tests ONLY - no implementation. Tests must fail RED for
> missing-implementation reasons (unresolved symbols are the expected RED).
> Do NOT delete, skip, or weaken any test. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Full unit + e2e test suite for the contact-channels tree, written BEFORE any implementation.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** - (Phase 0)

## Goal

Produce the failing RED test corpus that pins every contract in master.md (C1-C6) and every feature row (F1-F8) of the Test Gate matrix. Implementation leaves then make these tests pass GREEN. This is the tests-first gate: the tests ARE the spec's executable form.

## Context

Repo: /Users/caimlas/git/meept (Go). Voice sidecar contract: internal/voice/AGENTS.md. E2E harness: docs/workflows/e2e-testing.md; suites live in e2e/suites/<name>/ with the `e2e` build tag and a manifest.json entry (see e2e/manifest.json for the existing voice-local suite as the pattern).

## Verified Anchors

- internal/voice/callstore.go - SQLite store pattern to mirror for contacts
- internal/voice/registry_leg_test.go - existing leg-registry test patterns
- e2e/suites/voice-local/voice_local_test.go - hermetic e2e suite pattern (RPC open -> fake client -> assertions)
- e2e/manifest.json - suite registration format
- internal/voice/AGENTS.md - invariants the tests must pin

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Unit test files (RED):
- internal/contact/types_test.go      (C1 Record/Channel shapes)
- internal/contact/store_test.go      (C1 Store CRUD + E.164 normalize + GetByPhone/GetBySignalUUID)
- internal/contact/chain_test.go      (C2 ordered fallback, dedupe, per-attempt outcomes)
- internal/signal/rpc_test.go         (C3 json-rpc client: request-id mutex, notification demux, callEvent states)
- internal/signal/msg_test.go         (send/typing/receipts, envelope receive, dedupe)
- internal/voice/signal_leg_test.go   (C4 registry leg "signal": target shape, no E.164 mangle, shared capacity rules)
- internal/telnyx/messaging_test.go   (C5 send + payload parse + E.164 gate)
- internal/tools/builtin/contact_test.go (C6 tool arg shape + report rendering)

E2E suites (RED until implementation):
- e2e/suites/contact-chain/   (F1,F2,F8: chain across fake voice->signal->sms channels, one chain id)
- e2e/suites/signal-sidecar/  (F3,F4,F5: stub signal-cli json-rpc + callEvent + PCM through real adapters)
- e2e/suites/telnyx-sms/      (F6: signature-verified webhook inbound + DLR correlation)
```

### What This Leaf Consumes

- Contract IDs C1-C6 as defined in ../master.md (tests assert THOSE signatures; implementations do not exist yet).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | C1 unit tests | Create: internal/contact/types_test.go, internal/contact/store_test.go | `go test ./internal/contact/ 2>&1 | grep "no Go files\|undefined"` shows unresolved contact package | package missing | tests compile+pass after leaf 01 | Given a Record with Phone "+13055551234"; When Store.Put; Then GetByPhone returns it normalized and GetBySignalUUID resolves after SignalUUID set |
| T2 | C2 chain tests | Create: internal/contact/chain_test.go | `go test ./internal/contact/` | undefined ChainRunner | table covers: ordered attempts, stop-on-delivered, busy/unavailable/failed fallthrough, one chain id across attempts, delay respected (short test clocks) | Given attempts [voice:no_answer, signal_voice:busy, signal_msg:delivered]; When Run; Then exactly 3 attempts, report marks channel 3 delivered, shared chain id |
| T3 | C3 client tests | Create: internal/signal/rpc_test.go | `go test ./internal/signal/` | package missing | fake json-rpc server over unix socket: concurrent requests serialize (mutex), notification demux routes callEvent by callID, states map RINGING_OUTGOING->CONNECTED->ENDED | Given stub server emitting callEvent CONNECTED then ENDED; When SubscribeCallEvents + StartCall; Then client surfaces both events in order with correct callID |
| T4 | C4 leg tests | Create: internal/voice/signal_leg_test.go | `go test ./internal/voice/` | LegSignal undefined | registry leg "signal": target "signal:<uuid>" never E.164-normalized, one-live-per-target enforced vs phone targets sharing capacity, ccid prefix "signal-" | Given live signal call for uuid U; When AcquireLeg same target; Then ErrDuplicateTarget |
| T5 | C5 msg tests | Create: internal/signal/msg_test.go, internal/telnyx/messaging_test.go | `go test ./internal/signal/ ./internal/telnyx/` | packages missing | signal: Send returns ts, envelope demux dedupes (sourceUuid, timestamp); telnyx: SendSMS parses data.id, rejects non-E.164 input, parses message.received and message.finalized payloads | Given payload message.finalized status=failed; When parse; Then Message.Status=="failed" |
| T6 | C6 tool tests | Create: internal/tools/builtin/contact_test.go | `go test ./internal/tools/builtin/` | contact tool undefined | arg shape {person,message,channels?}, report renders per-channel lines with outcomes | Given ChainReport 2 attempts; Then output lines contain channel names + outcomes |
| T7 | e2e contact-chain suite | Create: e2e/suites/contact-chain/contact_chain_test.go; Modify: e2e/manifest.json (path_map + suite entry) | `go test -tags e2e ./e2e/suites/contact-chain/` | unresolved seams | full chain: contact record seeded, fake channel attempts registered, employee tool invocation walks voice(no-answer)->signal_voice(busy)->signal_msg(delivered), asserts one chain execution id on all effect/audit rows, asserts no second notification per channel | per scenario tables |
| T8 | e2e signal-sidecar suite | Create: e2e/suites/signal-sidecar/signal_sidecar_test.go; Modify: e2e/manifest.json | `go test -tags e2e ./e2e/suites/signal-sidecar/` | unresolved seams | stub signal-cli process (scripted json-rpc responses + callEvent stream + raw PCM socket) -> real Client -> real leg adapter -> PCM at fake child pair paced 640B/20ms; hangup release fan-out empties registry | per scenario tables |
| T9 | e2e telnyx-sms suite | Create: e2e/suites/telnyx-sms/telnyx_sms_test.go; Modify: e2e/manifest.json | `go test -tags e2e ./e2e/suites/telnyx-sms/` | unresolved seams | Ed25519-signed webhook POST message.received -> employee session row; message.finalized failed -> chain attempt outcome failed | per scenario tables |

Worked expansion for T2 (canonical TDD shape - apply the same to all rows):

```go
func TestChainStopsOnDelivered(t *testing.T) {
    // attempts recorder: voice->no_answer, signal_voice->busy, signal_msg->delivered
    // assert: 3 calls happened in policy order; report.Attempts[2].Outcome == "delivered";
    // assert: all attempts share report.ChainID
}
```

Verify RED: `go test ./internal/contact/ 2>&1 | head -5` -> build failure (package absent).
Expected GREEN (post-implementation): all tables pass.

## Self-Verification Checklist

- [ ] All test files created at exact paths; e2e suites registered in e2e/manifest.json
- [ ] Every test asserts a C1-C6 contract signature, not implementation internals
- [ ] All tests fail RED for missing-implementation reasons (run each verify command; build failure = expected RED)
- [ ] No implementation code written (stubs return zero values at most, clearly marked)
- [ ] Do NOT COMMIT.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines showing RED]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Tests exist for every Test Gate matrix row F1-F8
- [ ] No gate test asserts an implementation detail the contracts do not specify
- [ ] e2e suites follow the voice-local pattern (hermetic, build tag, manifest)
- [ ] RED verified per verify command

Output: APPROVED or specific gaps with file + line references.

## Notes

- The contact-chain e2e must use FAKE channel attempts (no network); the signal-sidecar e2e must run a REAL signal-cli-format stub process, not a mock in-process object - that is what makes it survive real-client changes.
- telnyx-sms e2e signs webhook payloads with a test Ed25519 key; the handler verifies against the injected public key (config seam), mirroring how Call Control webhooks verify in tests today.
