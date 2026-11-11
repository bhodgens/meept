# Telnyx Call Status + Ignored Events — Implementation Leaf (01)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks using TDD. The failing gate tests ALREADY EXIST and are
> committed — make them pass; do NOT delete, skip, weaken, or rewrite any gate
> test (report suspected-wrong gate tests to the orchestrator). Do NOT commit —
> the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files — explore with search_files or terminal
> cat. After writing a file, do NOT read it back. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ./master.md
- **Scope:** C1 GetCallStatus on the telnyx client + C2 IgnoredEventError classification in ParseCallEvent.
- **Dependencies:** 00T (gate tests committed)
- **Estimated Context:** 70K
- **Concurrency Group:** A (file-disjoint with 03, 08)

## Goal

Give meept's telnyx package the is_alive probe the reaper needs, and stop
non-lifecycle Telnyx call.* webhook events from logging as errors or causing
Telnyx retry storms. Ported from phone/core/internal/telnyx/{callstatus.go,
telnyx_webhook_test.go} on branch fix/coordinator-missed-hangup-watchdog in
~/git/uptractor-jobfile — cat those files; the shapes are frozen (master C1, C2).

## Context

- internal/voice/telnyx/client.go — Client{baseURL, apiKey}, postCall helper; add a GET counterpart
- internal/voice/telnyx/callcontrol.go:77 ParseCallEvent — the webhook switch (call.initiated/answered/hangup today)
- Webhook HTTP handler that consumes ParseCallEvent — must ACK ignored events (2xx) and log at debug; hard errors still refuse (non-2xx). Locate via grep ParseCallEvent.
- Source port: uptractor phone/core/internal/telnyx/callstatus.go (verbatim Go, rename package)

## Verified Anchors

- internal/voice/telnyx/client.go:53 — `func NewClient` — client shape
- internal/voice/telnyx/client.go:70 — `func (c *Client) postCall` — request/auth pattern to mirror for GET
- internal/voice/telnyx/callcontrol.go:77 — `func ParseCallEvent` — classification point

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/voice/telnyx
type CallStatus struct{ Known, Alive bool }
func (c *Client) GetCallStatus(ctx context.Context, callControlID string) (CallStatus, error)
type IgnoredEventError struct{ EventType string }
func IsIgnoredEvent(err error) bool
```

Semantics frozen: GET /calls/{call_control_id}, read data.is_alive; HTTP 404 =>
CallStatus{Known:false, Alive:false}, nil error; empty id => error; other HTTP
errors wrapped. IgnoredEventError only for event types with the "call." prefix
that are not in the lifecycle set; anything else stays a plain error.

### What This Leaf Consumes

- C1, C2 (master.md). Gate tests: internal/voice/telnyx/callstatus_test.go, ignored_event_test.go.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---|---|---|---|---|---|---|
| T1 | GetCallStatus + CallStatus | Create/modify: internal/voice/telnyx/callstatus.go | `go test ./internal/voice/telnyx -run CallStatus` | undefined | all green | Given httptest returns {"data":{"is_alive":false}}; When GetCallStatus; Then Known=true Alive=false nil err |
| T2 | IgnoredEventError + IsIgnoredEvent + ParseCallEvent classification | Modify: internal/voice/telnyx/callcontrol.go; Create: internal/voice/telnyx/ignored_event.go | `go test ./internal/voice/telnyx -run Ignored` | undefined | all green | Given body event "call.analyzed"; When ParseCallEvent; Then IsIgnoredEvent(err)==true |
| T3 | Webhook handler ACKs ignored events (2xx, debug log), keeps refusing non-call.* | Modify: the handler file found via grep | `go test ./internal/voice/... -run Webhook` + full telnyx suite | existing behavior | green, no regressions | Given handler receives call.analyzed; When request; Then 200 and debug-level log only |

## Self-Verification Checklist

- [ ] Gate tests pass unmodified; `go test ./internal/voice/telnyx` fully green
- [ ] Signatures match C1/C2 exactly
- [ ] No scope creep (no reaper code — that is leaf 02)
- [ ] gofmt + staticcheck clean on touched files

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] 404 semantics: Known=false, Alive=false, err=nil (not an error return)
- [ ] Non-call.* events still hard errors (misrouted webhook refused)
- [ ] Handler ack prevents Telnyx retries; log at Debug not Error
- [ ] No debug artifacts, no TODOs

Output: APPROVED or specific gaps with file + line references.

## Notes

- Keep httpClient override seam if present (tests inject transports).
- The uptractor source logs a `snippet(body)` on HTTP errors — mirror for debuggability.
