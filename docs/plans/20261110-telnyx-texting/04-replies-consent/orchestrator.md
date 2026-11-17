# 04-replies-consent — Branch Orchestrator

## Goal

Reply handling and consent: keyword classifier (STOP/START/HELP/YES ports), in-memory pending-confirmation registry (the #132 ack hook), daemon glue for opt-out + auto-replies with HELP throttle.

## Architecture Overview

Classifier + registry (leaf 01), daemon glue (leaf 02, edits the
OnInbound body created by branch 03). After this branch, dispatch-tree
consumers only need PendingConfirms.Register from a dispatch send.

## Interface Contracts (FROZEN)
## Interface Contracts (FROZEN)

Provenance: borrows vetted logic from ~/git/strategist-usa (owner's own
repo, no third-party license): go-web-hooks/internal/webhook/
signature.go+normalizer.go+tests, pkg/services/telnyx_provider.go,
go-shard-runner retry/keyword patterns. Adaptations are MANDATORY:
zap->slog, settings-DB/env->meept TOML+Broker, Redis->in-process
(memory nonce store; NO Redis in meept), org-scoped URLs->single tenant,
Postgres->SQLite (house convention modernc/WAL/MaxOpenConns(1)).
DO NOT port: Stripe/billing/campaign/sharding/ClickHouse infra.

Existing meept surfaces (survey 2026-11-10, file:line verified):
- internal/voice/telnyx/signature.go — Ed25519 "{ts}|{body}", unix-only
  ts, ±300s skew, RAW-32-BYTE keys only, no replay protection.
- internal/comm/http/messaging.go — POST
  /api/v1/voice/messaging/webhook (:34), bearer-exempt via HandlesPath,
  deps{Verify,OnInbound,OnFinalized} (:43-47); webhookEnvelope types
  from/to as PLAIN STRINGS (:232-244) — WRONG for real Telnyx payloads
  (from is an OBJECT {phone_number}, to is an ARRAY of objects).
- internal/telnyx/messaging.go — Client.SendSMS POST /v2/messages
  Bearer+profile (:196-228); NO Idempotency-Key, NO per-message
  webhook_url; strict E.164 gate (:57-60).
- internal/telnyx/sms_attempt.go — SMSAttempt with in-memory
  pending-DLR map; currently UNRESOLVABLE because OnFinalized is never
  armed (voice_wiring.go:709-712 sets Verify only; grep shows no
  assignee of OnInbound/OnFinalized anywhere) — CRITICAL GAP this tree
  closes.
- config [voice.messaging] MessagingConfig{Enabled,FromNumber,ProfileID,
  DLRWaitSeconds} (schema.go:5545-5570); API key via Broker
  "telnyx_api_key"; telnyx_sms already in channel vocab (:5108).

Frozen additions:

1) Signature v2 (extend internal/voice/telnyx/signature.go):
```go
// Accepts raw 32-byte OR 44-byte SPKI DER public keys (port
// ED25519_SPKI_HEADER branch logic + detach-copy fix from
// signature.go:28,291-309); timestamp: unix-seconds OR RFC3339
// fallback (:235-245); skew: 300s past / 60s future retained.
func VerifyWebhook(pubKey []byte, sigB64, tsHeader string, body []byte) error
// constant-time compare helper ported (signature.go:445) as
// constantTimeSignatureMatch if not already present.
type NonceStore struct{ ... } // in-process TTL+LRU, NO Redis
func NewNonceStore(ttl time.Duration, max int) *NonceStore // ttl=6m, max=10000
func (n *NonceStore) CheckFresh(eventID string) bool // true=fresh, records
func (n *NonceStore) Release(eventID string) // free for provider redelivery
```
   Handler posture (port strategist's table): signature fail -> 401;
   duplicate event-id (CheckFresh false) -> 200 ack-noop; known-noop
   events (message.created/deleted, non-SMS types) -> 200; unknown
   event type -> 200 (stop redelivery); downstream handler error -> 500
   (redelivery OK, nonce Released first); success -> 202.

2) Payload normalizer (new internal/voice/telnyx/normalizer.go):
```go
type Message struct {
    Event, EventID, Direction, Status string
    IssuedAt                          time.Time
    From, To                          string // normalized E.164 US-biased
    Text                              string
    ProviderMessageID                 string // data.id
    MediaURLs                         []string // scheme-filtered http/https
    Errors                            []struct{ Code, Description string }
}
func ParseMessagingEvent(raw []byte) (Message, error)
// tolerant extractors (port extractStringOrPhoneField / extractToField
// from telnyx_provider.go:674-706): from string|{phone_number};
// to string|[{phone_number}] (first entry); status map (normalizer.go
// :247-293): message.received->inbound; sent/queued/sending->status;
// finalized->delivered|failed|undelivered|expired from payload.status;
// failed/delivery_failed->error.
func NormalizeE164US(s string) string // port normalizeE164 :297-325
```
   comm/http webhookEnvelope is DELETED and replaced by
   ParseMessagingEvent on the buffered raw body (verify-then-parse;
   keep BodyBuffer semantics — the handler must verify against the RAW
   bytes then parse).

3) Send hardening (internal/telnyx/messaging.go):
```go
type SendRequest struct{ To, Text string; MediaURLs []string
                         IdempotencyKey, WebhookURL string } // empty key -> uuid
func (c *Client) SendSMS2(ctx, req SendRequest) (SendResult, error)
type SendResult struct{ ProviderMessageID string; Status string }
type RateLimitError struct{ RetryAfter time.Duration } // parses
// Retry-After secs-or-HTTP-date + X-RateLimit-Remaining (port
// telnyx_rate_limit.go:29-80); >=400 error carries status code +
// body snippet truncated 512B (PII hygiene).
// POST /v2/messages adds: Idempotency-Key header (uuid), webhook_url
// when SendRequest.WebhookURL non-empty; https-only base validation.
```
   contact channel (telnyx_sms SMSAttempt path) switches to SendSMS2
   with WebhookURL = [voice.messaging].webhook_base_url (new key) when
   set; DLR map becomes resolvable via 5).

4) Store (new package internal/sms):
```go
func Open(dbPath string, logger *slog.Logger) (*Store, error) // $MEEPT_HOME/sms/sms.db, WAL, MaxOpenConns(1)
// schema (mirrors strategist sms_events_raw + sms_message_state):
// sms_events_raw(event_id TEXT PRIMARY KEY, event TEXT, direction
//   TEXT, src TEXT, dst TEXT, body TEXT, payload TEXT, provider_ts
//   TEXT, received_at TEXT)
// sms_message_state(provider_message_id TEXT PRIMARY KEY, dst TEXT,
//   status TEXT, err TEXT, updated_at TEXT, event_id TEXT)
func (s *Store) AppendEvent(ctx, ev Event) (bool, error) // INSERT OR
// IGNORE semantics; false = duplicate event id (idempotent)
func (s *Store) UpsertState(ctx, st State) (bool, error) // monotonic:
// applied only when provider_ts >= existing (out-of-order protection,
// port conflict rule from 001_initial_schema.sql:75+ semantics)
func (s *Store) RecentEvents(ctx, src string, since time.Time, limit int) ([]Event, error)
```

5) Bus topics (internal/sms/topics.go, typed-bus pattern):
```go
type InboundMessage struct{ EventID, From, To, Text string; At time.Time }
var InboundTopic = bus.NewTopic[InboundMessage]("sms.inbound.received")
type StateChanged struct{ ProviderMessageID, Status, Err string }
var StateTopic = bus.NewTopic[StateChanged]("sms.state.changed")
```

6) Hook arming (internal/daemon — closes the dead-seam gap):
   WireVoiceDeps/ContactChainWiring gains
   MessagingStore *sms.Store + MessagingNonces *telnyx.NonceStore;
   buildMessagingDeps sets OnInbound/OnFinalized: OnInbound -> store
   append (dup -> ack-ok) + InboundTopic publish + reply classifier (7);
   OnFinalized -> store UpsertState + StateTopic publish +
   attempts.Resolve(providerMessageID,status) (new exported
   resolve method on the DLR pending map, sms_attempt.go).

7) Reply/consent (new internal/sms/reply.go + daemon glue):
```go
type ReplyAction string // ReplyStop|ReplyStart|ReplyHelp|ReplyYes|ReplyOther
func ClassifyReply(text string) ReplyAction // upper(trim); port STOP
// family (stop/unsubscribe/cancel/quit/end/remove/noneed + common
// misspellings/emoji from keyword_rule_engine/consent_service.go:178
// semantics); YES = exact yes/confirm/done/ok (dispatch ack, #132).
```
   Daemon glue on ReplyStop/Start: guard ALTER contacts
   messaging_opt_out INTEGER NOT NULL DEFAULT 0; send confirmation text
   (stop_reply/help_reply config keys, defaults ported wording from
   :395-403); HELP throttled 1/contact/24h via NonceStore-shaped TTL
   map (reuse NonceStore with ttl 24h instance). ReplyYes: if a pending
   confirmation registry has the From number (exported
   sms.PendingConfirms: Register(ref, to, text) /
   Resolve(to) (*pending, bool)) -> resolve + StateTopic-style ack
   publish; v1 registry in-memory (dispatch tree persists later).
```

8) config [voice.messaging] additions: webhook_base_url string,
   stop_reply string, help_reply string, inbound_enabled bool (default
   false). All normalize at load boundary per house; Broker key names
   unchanged ("telnyx_api_key").



## Child Index

| Child | Est. ctx | Deps | Order |
|---|---|---|---|
| 00T | 45K | br-03/02 | 1 |
| 01 | 35K | 00T | 2 |
| 02 | 40K | 01 | 3 |

## Dispatch Protocol

1. Dispatch the `00T` test-gate leaf first ("tests ONLY, RED for missing
   implementation, **Do NOT commit**"). Control agent reviews IN-SESSION
   against the Test Gate matrix; lesser artifact yields to greater;
   commit gate tests; TESTS_VALIDATED.
2. Dispatch implementation leaves per Order; serialize shared-file edits
   per master's serialization table.
3. Review returns IN-SESSION (main model); max 3 loops.
4. Orchestrator-only commits, per-file pathspec.

## Coding Conventions

Per master.md. Ported algorithms keep a source comment ("ported from
strategist-usa <path>:<lines>"). No Redis/Postgres imports; uuid via
pkg/id (verify) or crypto/rand hex.

## Test Gate

| Feature | Unit tests | e2e | Status |
|---|---|---|---|
| STOP-family + YES/HELP classification | 00T §K1 | — | PENDING |
| PendingConfirms one-shot/TTL/normalized | 00T §K2 | — | PENDING |
| glue: opt-out+confirm+throttle+ack flow | 00T §K3 | — | PENDING |

## Completion Tracking Table

| Child | Status | Commit | Notes |
|---|---|---|---|
| 00T | PENDING | | |
| 01 | PENDING | | |
| 02 | PENDING | | |

## Integration Test Plan

Master e2e step-3 STOP/YES subcases run here; final gate: full master Integration Test Plan 1-5 executed by master orchestrator after branch COMPLETE.

## Review Checklist

- [ ] Branch suite GREEN together; contract strings grep-verified.
- [ ] `go build ./...` green from worktree root.
- [ ] Tracking table honest before reporting COMPLETE to master.
- [ ] Ported-source comments present (D1 audit).

## Open Questions

See master.md.
