# Telnyx SMS channel - Implementation Leaf

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
- **Scope:** Telnyx messaging: send client, signature-verified inbound/DLR webhook receiver, the telnyx_sms ChannelAttempt (contract C5).
- **Dependencies:** 01-contact-foundation (ChannelAttempt seam, REVIEWED)
- **Estimated Context:** 45K
- **Concurrency Group:** B

## Goal

SMS as the universal contact floor: every contact with a phone number is reachable; delivery outcomes (message.finalized) feed the chain's failure detection. Inbound SMS reaches the employee's session like other channels.

## Context

Same Telnyx account as Call Control (one API key - `Broker.Value("telnyx_api_key")` already resolves it); messaging needs a separate Messaging Profile bound to the number (ops setup, documented not coded). Webhooks: same Ed25519 `telnyx-signature` header scheme as the existing Call Control verifier - REUSE that verifier, never write a second one. Event types: `message.received` (inbound text), `message.finalized` (terminal DLR: delivered|failed). US 10DLC: application-to-person traffic from local numbers requires brand+campaign registration (days-weeks) - for this use case the toll-free-verified path is recommended; document it, do not code around it. Research archive: .research/signal-telnyx-channels.md.

## Verified Anchors

- internal/telnyx/ - existing Call Control client package (messaging client lives beside it; same auth)
- internal/comm/http/ - the voice webhook routes (signature verification + route registration pattern to mirror)
- scripts/audit-secrets-value-callers.py - Broker.Value caller allowlist (only if a NEW Value call is needed; the telnyx key caller likely exists - reuse)
- internal/voice/binding.go:199-231 - session-bind pattern for inbound routing
- .research/signal-telnyx-channels.md - endpoint/payload shapes + 10DLC notes

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/telnyx/messaging.go (NEW file in existing package):
type Message struct {
    ID         string
    From       string // E.164
    To         string // E.164
    Text       string
    Direction  string  // "inbound"|"outbound"
    Status     string  // "queued"|"sent"|"delivered"|"failed"
    ReceivedAt time.Time
}
func NewMessagingClient(apiKey string, profileID string, httpClient *http.Client, log *slog.Logger) *MessagingClient
func (m *MessagingClient) SendSMS(ctx context.Context, from, to, text string) (id string, err error)
// to gated by strict ^\+[1-9]\d{7,15}$ (ErrInvalidE164); api: POST /v2/messages
// {from, to, text, messaging_profile_id}; parse data.id.

// ParseWebhook(eventType string, payload []byte) (Message, error)
// Handles "message.received" and "message.finalized".

// Webhook receiver: internal/comm/http route (mirror the voice webhook route
// pattern; SAME Ed25519 signature verifier as Call Control):
type MessagingWebhookDeps struct {
    Verify  func(payload []byte, headers http.Header) error // existing verifier seam
    OnInbound func(Message) error  // bind session + inject prompt material
    OnFinalized func(Message) error // DLR -> chain attempt outcome resolution
}

// contact attempt (internal/contact or internal/telnyx - put the adapter in
// telnyx to avoid a telnyx->contact import):
type SMSAttempt struct{ /* client + DLR wait */ }
func (s *SMSAttempt) Channel() contact.Channel // "telnyx_sms"
func (s *SMSAttempt) Attempt(ctx context.Context, rec contact.Record, message string) (outcome string, err error)
// Send then WAIT (bounded, config, default 30s) for message.finalized for
// the returned id: delivered -> "delivered"; failed -> "failed"; timeout ->
// "unavailable" (chain falls through; DLR continues async and updates state).
```

### What This Leaf Consumes

- Existing telnyx auth + webhook verifier seams
- contact.ChannelAttempt (C2)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | messaging client | Create: internal/telnyx/messaging.go; Test: internal/telnyx/messaging_test.go (exists, gate RED) | `go test ./internal/telnyx/ -run SMS` | client absent | SendSMS parses data.id; E.164 gate rejects malformed (named ErrInvalidE164); profile id included | Given to="+1305555123" (too short); Then ErrInvalidE164, no HTTP call |
| T2 | webhook parse | Modify: internal/telnyx/messaging.go | `go test ./internal/telnyx/ -run SMS` | parse absent | message.received and message.finalized payloads -> Message (status failed preserved) | Given finalized payload status=failed; Then Message.Status=="failed" |
| T3 | webhook route | Create: receiver in internal/comm/http (route + handler; reuse verifier); Test: NEW internal/comm/http/messaging_webhook_test.go | `go test ./internal/comm/http/ -run Messaging` | route absent | bad signature -> 401, never parsed; good signature message.received -> OnInbound fired; finalized -> OnFinalized fired | Given tampered payload; Then 401 and zero dep calls |
| T4 | SMSAttempt adapter | Create: internal/telnyx/sms_attempt.go; Test: NEW internal/telnyx/sms_attempt_test.go | `go test ./internal/telnyx/ -run Attempt` | adapter absent | delivered/failed/timeout outcomes per DLR; async DLR after timeout updates state for observability | Given finalized(delivered) in 100ms; Then outcome "delivered" fast |
| T5 | config | Modify: internal/config/schema.go ([messaging] under [voice] or sibling [telnyx.messaging]: from_number, profile_id, dlr_wait_seconds default 30, enabled false) + DefaultConfig + template | `go build ./... && go test ./internal/config/` | section absent | defaults present; enabled=false keeps everything refused (named ErrMessagingDisabled) | Given enabled=false; Then Attempt returns ErrMessagingDisabled before any HTTP |

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests passing (messaging_test GREEN)
- [ ] Interface contracts satisfied exactly (C5)
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
- [ ] Signature verifier REUSED (no second Ed25519 implementation); no key material in logs
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The Messaging Profile + toll-free verification setup is documented in the leaf report (ops checklist), NOT automated.
- If the existing telnyx package has no reusable HTTP error shape, follow its Call Control error handling rather than inventing a parallel one.
