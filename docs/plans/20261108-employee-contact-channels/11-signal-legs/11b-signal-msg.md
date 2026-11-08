# Signal messaging channel - Implementation Leaf

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

- **Parent:** ../orchestrator.md
- **Scope:** The signal_msg contact.ChannelAttempt (outbound) + inbound envelope routing into employee sessions. File-partitioned: ONLY internal/signalleg/msgchannel.go + inbound router + tests.
- **Dependencies:** 10-signal-sidecar (REVIEWED), 01-contact-foundation (REVIEWED), 11a-signal-voice-leg creates the package (dispatch 11a first OR same wave with this file partition - msgchannel.go is a NEW file, no shared-file race)
- **Estimated Context:** 30K
- **Concurrency Group:** B

## Goal

Messages an employee sends reach a Signal contact (with throttle + allowlist + backoff), and inbound Signal messages append to the same (number=signal:<uuid>, employee) session machinery other legs use - so conversation context carries across voice and text.

## Context

Same signal-cli subprocess as the voice leg (leaf 11a's package). The contact chain (#118) consumes the ChannelAttempt. Research archive: .research/signal-telnyx-channels.md (json-rpc send/envelope shapes; abuse posture: throttle outbound, treat privacy-setting rejections as channel-unavailable).

## Verified Anchors

- internal/signalleg/leg.go (from 11a) - package Deps type (reuse Client/Binding seams; do not re-declare)
- internal/voice/binding.go:199-231 - OnCallStart session-bind pattern (inbound routing reuses the same binding for messages)
- internal/contact/chain.go - ChannelAttempt seam this leaf implements
- .research/signal-telnyx-channels.md - envelope payload + rate-limit notes

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/signalleg/msgchannel.go (NEW file):
func (l *Leg) MessageAttempt() contact.ChannelAttempt
// Attempt(ctx, rec contact.Record, message string) (outcome string, err error)
// Order: allowlist check -> token-bucket throttle (default: 3/min per
// account, config) -> Client.Send(rec=rec.SignalUUID fallback rec.Phone,
// message) -> ts>0 = "delivered".
// Send error mapping: rate-limit/4xx-style = "unavailable" (chain falls
// through); hard failure = "failed". Privacy-setting rejections arrive as
// send failures => "unavailable", NEVER a daemon error.
// Attempts append an audit-style attempt row keyed by chain id (C2 posture).

// internal/signalleg/inbound.go (NEW file):
func (l *Leg) RouteInbound(msg signal.DataMessage)
// Dedupe (sourceUuid, timestamp); bind (number="signal:"+sourceUuid,
// employee) session via Binding; inject the message body into the session
// as prompt material (the same path other legs feed transcripts);
// attachments logged by path only.
```

### What This Leaf Consumes

- C3 Client (Send/Typing/RegisterMessages)
- contact.ChannelAttempt seam (C2)
- Binding session machinery

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | MessageAttempt | Create: internal/signalleg/msgchannel.go; Test: internal/signal/msg_test.go extensions in gate file internal/tools/builtin/contact_test.go (chain-level) + NEW internal/signalleg/msgchannel_test.go | `go test ./internal/signalleg/ -run Message` | method absent | allowlist -> throttle -> send -> outcome mapping green | Given rate-limited send; Then Attempt returns "unavailable", nil err (chain falls through) |
| T2 | throttle | Modify: internal/signalleg/msgchannel.go (token bucket; [signal].msg_per_minute config, default 3) | `go test ./internal/signalleg/ -run Message` | no throttle | 4th send within a minute with fake clock blocks (or defers per config) until bucket refills | Given 3 sends + 4th immediate; Then 4th waits (fake clock advances) |
| T3 | inbound routing | Create: internal/signalleg/inbound.go | `go test ./internal/signalleg/ -run Inbound` | router absent | dedupe works; session bound via Binding with signal:<uuid> number; body injected as prompt material | Given same envelope twice; Then Binding.OnCallStart-equivalent fired once, message present once |
| T4 | config | Modify: internal/config/schema.go ([signal].msg_per_minute, allowed_uuids consumed by both channels) | `go test ./internal/config/` | fields absent | defaults: msg_per_minute=3, allowed_uuids empty | Given DefaultConfig(); Then cfg.Signal.MsgPerMinute==3 |

## Self-Verification Checklist

- [ ] All tasks implemented; tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths (msgchannel.go and inbound.go are NEW files - no edits to leg.go)
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Do NOT COMMIT.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing, gate tests untouched
- [ ] Interface contracts match exactly (signatures, types, paths)
- [ ] No account identifiers or message bodies in logs; no scope creep
- [ ] Conventions followed; no bugs, no security issues
- [ ] No debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Groups (joinGroup/updateGroup) are OUT of scope - 1:1 only this tree.
- Typing indicators: send typing on chain attempts is OPTIONAL polish; do not implement unless trivial (one Client.Typing call before Send).
