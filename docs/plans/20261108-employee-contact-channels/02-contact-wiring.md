# Contact wiring (employee tool + channel adapters) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing tests ALREADY EXIST and
> are committed (test gate 00T) - your job is to make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> gate tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** The employee-facing `contact` tool (C6) + the telnyx_voice ChannelAttempt adapter over the existing dial path + full wiring of Registry -> Store -> ChainRunner -> channel attempts.
- **Dependencies:** 01 (REVIEWED), 11 branch (REVIEWED), 12 (REVIEWED)
- **Estimated Context:** 40K
- **Concurrency Group:** C (last)

## Goal

One employee action: `contact(person, message)` resolves the person, walks the configured channel chain, and reports per-channel outcomes. The telnyx_voice adapter closes the chain over the EXISTING voice_call dial path (no-answer advances the chain; an answered call STOPS the chain and proceeds as a live call).

## Context

The existing employee voice tool is internal/tools/builtin/voice_call.go (leg/LocalFunc/LegDefaultFunc seam pattern - policy stays in the leg, the tool stays thin). The contact tool follows that pattern: it resolves + chains, and each channel's policy lives in its adapter. Voice-call semantics (root master C2): connected = chain terminal "answered"; no-answer/busy/unavailable/failed = advance.

## Verified Anchors

- internal/tools/builtin/voice_call.go:46-91 - the seam pattern (LocalFunc/LegDefaultFunc/SetX nil guards) to mirror for ContactFunc
- internal/contact/chain.go - ChainRunner (REVIEWED in leaf 01)
- internal/voice/dial.go:209 - the dial path the telnyx_voice adapter wraps (no-answer timer -> FailOutcome(OutcomeNoAnswer))
- internal/daemon/voice_wiring.go - where tools get their seams armed
- internal/contact/types.go - Record resolution helpers to add (GetByPhone/GetBySignalUUID exist; add Resolve(idOrPhoneOrUUID))

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/contact/resolve.go (NEW):
func (s *SQLiteStore) Resolve(ctx context.Context, key string) (Record, error)
// Tries: exact ID -> normalized E.164 -> Signal UUID. ErrUnknownContact otherwise.

// internal/tools/builtin/contact.go (NEW tool, name "contact"):
// Args json: {"person": string, "message": string, "channels": []string optional}
// Behavior: Resolve -> ChainRunner.Run(record, message, override) ->
// report rendered as lowercase per-channel outcome lines:
//   telnyx voice: no answer
//   signal voice: busy
//   signal message: delivered
// SetContactFunc seam (nil-guarded, voice_call.go pattern) arms the tool with
// a runner closure built in daemon wiring from: Store, ChainRunner, attempts map.

// internal/daileg or internal/contact adapters - telnyx_voice attempt:
// internal/voice/voiceattempt.go (NEW):
func NewTelnyxVoiceAttempt(dial func(ctx, target, firstLine string) (ccid string, err error),
    wait func(ctx, ccid) (outcome string)) contact.ChannelAttempt
// dial = the existing outbound dial path; wait = terminal outcome
// (answered|no_answer|busy|failed) from Manager outcome tracking. A CONNECTED
// call is chain-terminal: outcome "answered", the call continues on its own
// session (goal parked by existing machinery).
```

### What This Leaf Consumes

- C1 Store + C2 ChainRunner (leaf 01), C3/C4 signal attempts (leaf 11), C5 SMS attempt (leaf 12)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Resolve helper | Create: internal/contact/resolve.go; Test: internal/contact/resolve_test.go (NEW) | `go test ./internal/contact/ -run Resolve` | method absent | id/phone/uuid resolution with E.164 normalize; ErrUnknownContact | Given key "+13055551234"; Then record by phone returned |
| T2 | telnyx_voice adapter | Create: internal/voice/voiceattempt.go; Test: NEW internal/voice/voiceattempt_test.go | `go test ./internal/voice/ -run VoiceAttempt` | adapter absent | no_answer advances chain; answered terminal; busy/failed mapped; allowlist via existing dial gates | Given dial func returning no-answer outcome; Then Attempt returns "no_answer" |
| T3 | contact tool | Create: internal/tools/builtin/contact.go; Test: internal/tools/builtin/contact_test.go (exists, gate RED) | `go test ./internal/tools/builtin/ -run Contact` | tool undefined | arg shape + nil-guarded SetContactFunc + lowercase report lines; unknown person = named error text, not crash | Given person="sam" resolved to 2-channel chain; Then output lists both attempts with outcomes |
| T4 | daemon wiring | Modify: internal/daemon/voice_wiring.go or components wiring (build attempts map: telnyx_voice, signal_voice, signal_msg, telnyx_sms; ChainRunner with [contact] delays; arm SetContactFunc) | `go build ./... && go test ./internal/daemon/ -run Contact` | wiring absent | disabled channels simply absent from attempts map (chain skips them with an "unconfigured" report line, never an error) | Given signal disabled; Then chain report shows signal lines as unconfigured, chain continues |
| T5 | e2e greening | Make gate e2e suites pass: e2e/suites/contact-chain (full chain), signal-sidecar, telnyx-sms | `go test -tags e2e ./e2e/suites/contact-chain/ ./e2e/suites/signal-sidecar/ ./e2e/suites/telnyx-sms/` | RED from gate | all GREEN; one chain id across attempts; no double-notification | per scenario |

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests passing (contact_test GREEN, e2e suites GREEN)
- [ ] Interface contracts satisfied exactly (C6)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Do NOT COMMIT.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests passing, gate tests untouched
- [ ] Interface contracts match exactly (signatures, types, paths)
- [ ] Tool text lowercase (UI convention); no recipient message bodies in logs
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The tool does NOT expose raw channel tools to employees; `voice_call` stays for direct control, `contact` is the policy-driven path. Keep both.
- Report lines are lowercase per the UI conventions (root AGENTS.md).
