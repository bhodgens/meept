# Call Recording + Consent Gate — Implementation Leaf (06)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks using TDD. The failing gate tests ALREADY EXIST — make
> them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ./master.md
- **Scope:** Telnyx dual-channel call recording on answer + recording.saved URL capture, default-off, hard-gated on a consent-announcement pin.
- **Dependencies:** leaf 01 COMPLETE (shares telnyx client + manager answer path; SERIALIZED after leaf 02 — both touch internal/voice/manager.go)
- **Estimated Context:** 70K
- **Concurrency Group:** B (after 02)

## Goal

Port the recording capability from ~/git/uptractor-jobfile branch
fix/coordinator-missed-hangup-watchdog (phone/core/internal/telnyx/recording.go
+ README-callbrief.md switches) into meept: on call.answered, when
`voice.record_calls` is enabled AND a consent announcement pin is configured,
start a dual-channel mp3 recording; capture the URL from the
`call.recording.saved` webhook onto the call record. Consent gate semantics
are frozen: without the pin, enabling recording is a startup REFUSAL (Error
log + feature forced off), never a silent record-without-consent. Rationale
carried verbatim from source: recording needs caller consent (all-party-consent
states); enable only once the call announces recording and AI.

## Context

- internal/voice/telnyx/ — client (postCall), webhook parse (ParseCallEvent; recording.saved arrives as a call.* lifecycle-adjacent event — classify explicitly, do NOT let leaf 01's IgnoredEventError swallow it)
- internal/voice/manager.go — answer path (call.answered handling)
- internal/voice/callstore.go — voice_calls; recording URL stored in an additive `recording_url TEXT NOT NULL DEFAULT ''` column (guarded ALTER, same pattern as leaf 04's brief column — COORDINATE: if leaf 04 landed first, extend its migration; the migration must stay idempotent)
- Source: cat phone/core/internal/telnyx/recording.go + recording_test.go

## Verified Anchors

- internal/voice/telnyx/client.go:70 — postCall — request pattern for POST /calls/{id}/actions/record_start
- internal/voice/manager.go:321 — HandleCallEvent — answer + recording.saved routing

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/voice/telnyx
func (c *Client) StartRecording(ctx context.Context, callControlID string, opts RecordOptions) error
type RecordOptions struct{ Format string; Channels string } // frozen default: {"mp3","dual"}

// internal/voice (config surface)
// voice.record_calls bool (default false)
// voice.record_consent_pin string — REQUIRED non-empty when record_calls true;
//   startup validation refuses (logs Error, disables recording) when missing.
func (s *CallStore) SetRecordingURL(ctx context.Context, ccid, url string) error
```

Webhook: `call.recording.saved` payload's recording URL (data.download_url or
source-shape equivalent — verify against recording_test.go fixtures in the
source repo) is stored via SetRecordingURL keyed by call_control_id.

### What This Leaf Consumes

- C1/C2 from leaf 01 (recording.saved must NOT be classified as IgnoredEventError). Gate tests: internal/voice/recording_test.go.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---|---|---|---|---|---|---|
| T1 | StartRecording on telnyx client + RecordOptions defaults | Create internal/voice/telnyx/recording.go | `go test ./internal/voice/telnyx -run Record` | gate RED | green | Given httptest server; When StartRecording; Then POST /calls/{id}/actions/record_start with mp3+dual payload |
| T2 | recording.saved parse + URL capture; explicit event classification so IgnoredEventError never swallows it | Modify internal/voice/telnyx/callcontrol.go, internal/voice/manager.go | `go test ./internal/voice/telnyx && go test ./internal/voice -run Recording` | gate RED | green | Given recording.saved webhook; When handled; Then call row recording_url set |
| T3 | Answer-path start + consent gate: record_calls on + pin set => StartRecording at answer; pin missing => startup refusal, recording off, Error log; default off => no call | Modify internal/voice/manager.go, internal/daemon/voice_wiring.go, internal/config | `go test ./internal/voice -run Recording && go build ./...` | gate RED | green | Given record_calls=true, pin empty; When daemon boots; Then recording disabled + Error logged |
| T4 | callstore additive column (idempotent, coexists with leaf 04's migration) | Modify internal/voice/callstore.go | `go test ./internal/voice` full | gate RED | green | Given store opened twice; When second open; Then no migration error |

## Self-Verification Checklist

- [ ] Default-off proven by test (no StartRecording HTTP call observed when disabled)
- [ ] Consent pin refusal happens at STARTUP, not lazily at first call
- [ ] recording.saved survives leaf 01's ignored-event classification (explicit test)
- [ ] `go test -p 2 ./internal/voice ./internal/voice/telnyx` green
- [ ] Migration idempotent alongside brief/ext columns

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** 2. **Verify command + output:** last 20 lines 3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] No path records without the pin (grep every StartRecording call site)
- [ ] URL stored server-side; never rendered into prompts unredacted
- [ ] Webhook classification change doesn't reopen leaf 01's retry-storm fix
- [ ] No debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- Dual-channel keeps caller/agent separable for eval + brief accuracy; keep the option struct even though only one default ships.
