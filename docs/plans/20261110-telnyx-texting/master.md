# Telnyx Texting Leg — Master Plan

Date: 2026-11-10. Repo: meept. Tree: docs/plans/20261110-telnyx-texting/
Worktree: .hermes/worktrees/telnyx-texting (branch plan/telnyx-texting)
Issues: #132 (SMS acknowledgments for dispatch) depends on this tree.

## Goal

Give meept a production-grade Telnyx SMS leg: hardened webhook
ingress (signatures, replay, tolerant payloads), hardened outbound
send (idempotency keys, per-message webhooks, rate-limit errors), a
durable message store + bus topics, ARMED inbound/finalized hooks
(the seams exist but nothing wires them — SMS DLR is currently
unresolvable), and reply/consent handling (STOP/HELP/YES) that sets
up the dispatch ack channel.

Borrow proven logic from the owner's vetted strategist-usa repo
(go-web-hooks + telnyx_provider + retry/keyword workers); port
algorithms, not infrastructure (adaptations frozen in contracts).

## Decisions (owner-ratified 2026-11-10, do NOT re-litigate)

- D1 Provenance: strategist-usa is the owner's; direct borrowing
  allowed, no third-party license constraints (verified).
- D2 SQLite + in-process structures only — NO Redis/Postgres/BullMQ
  despite strategist using them; effects/nonce patterns are adapted.
- D3 SMS leg ships behind [voice.messaging].inbound_enabled (default
  false) + existing MessagingConfig.Enabled for send; disabled daemon
  behavior byte-identical.
- D4 YES-reply resolution v1 uses an in-memory PendingConfirms
  registry; durable registry belongs to the dispatch tree.
- D5 contacts table gains messaging_opt_out (guard ALTER) consulted
  by ALL outbound sends (SMS first; voice consult deferred to the
  dispatch tree).
- D6 The dead-seam fix (OnInbound/OnFinalized never armed) is a
  CRITICAL deliverable of this tree, not a refactor-bonus.

## Interface Contracts (FROZEN — all leaves)

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

| Child | Type | Deps |
|---|---|---|
| 01-webhook-hardening/00T | test-gate | — |
| 01-webhook-hardening/01-signature-v2 | leaf | 00T |
| 01-webhook-hardening/02-normalizer | leaf | 00T, 01 |
| 02-send-leg/00T | test-gate | — |
| 02-send-leg/01-client | leaf | 00T |
| 02-send-leg/02-channel | leaf | 01 |
| 03-inbound-store/00T | test-gate | — |
| 03-inbound-store/01-store | leaf | 00T |
| 03-inbound-store/02-arming | leaf | 01, 01-branch |
| 04-replies-consent/00T | test-gate | 03-arming |
| 04-replies-consent/01-reply | leaf | 00T |
| 04-replies-consent/02-consent | leaf | 01 |

## Dispatch Protocol

1. Per branch: dispatch its 00T gate first (tests only, RED for missing
   symbols in gate files only, Do NOT commit); validate IN-SESSION vs
   the branch matrix; commit gate tests; TESTS_VALIDATED.
2. Cross-branch waves: gates for 01,02,03 dispatch together (disjoint
   files). 04 gate after 03-arming (needs hooks shape). Impl leaves per
   branch order; 02-send-leg/01 and 01-webhook leaves parallel-safe.
3. SHARED-FILE SERIALIZATION (master-enforced):
   - internal/comm/http/messaging.go: 01/02 (envelope->normalizer swap)
     then 03/02 (deps wiring) — order enforced.
   - internal/daemon/voice_wiring.go: 03/02 (arming) then 04/01 (reply
     glue insertion point inside OnInbound).
   - internal/telnyx/messaging.go: 02/01 only.
   - internal/config/schema.go: 02/01 (webhook_base_url + replies)
     single writer per key set; 04/02 adds nothing there (defaults live
     in sms pkg).
4. Review IN-SESSION (main model) each return; max 3 loops; reset
   status on re-dispatch. Orchestrator commits ONLY, per-file pathspec.

## Coding Conventions

Per branch orchestrators. Additionally: every ported algorithm keeps a
source comment ("ported from strategist-usa <path>:<lines>") so
provenance is auditable in-tree.

## Test Gate

| Branch | Gate | Status |
|---|---|---|
| 01-webhook-hardening | 00T | COMPLETE |
| 02-send-leg | 00T | COMPLETE |
| 03-inbound-store | 00T | COMPLETE |
| 04-replies-consent | 00T | COMPLETE |

## Completion Tracking Table

| Child | Status | Commit | Notes |
|---|---|---|---|
| 01/00T | COMPLETE | 63482243 | |
| 01/01 | COMPLETE | 3837ff9a | ghost-window LRU ratified |
| 01/02 | COMPLETE | e8e2dfa4 | incl gate amendment: legacy webhook test supersession |
| 02/00T | COMPLETE | da648093 | line-197 mask repaired |
| 02/01 | COMPLETE | 48f635e8 | |
| 02/02 | COMPLETE | a8186602 | |
| 03/00T | COMPLETE | fe478143 | |
| 03/01 | COMPLETE | 044228ac | |
| 03/02 | COMPLETE | 065337b3 | closes D6 |
| 04/00T | COMPLETE | 4d0a0687 | |
| 04/01 | COMPLETE | dc560d0f | |
| 04/02 | COMPLETE | 99326c6c | |

## Integration Test Plan

1. go build ./... && go vet clean.
2. go test ./internal/voice/telnyx/ ./internal/sms/ ./internal/telnyx/
   ./internal/comm/http/ ./internal/daemon/ -count=1 — existing suites
   unchanged green; disabled-default golden: webhook route absent-or-
   200-noop + no send when enabled=false, byte-identical prompt/handler
   tests.
3. End-to-end fake-Telnyx (httptest base URL via existing
   MEEPT_TELNYX_TEST_BASE_URL seam): SendSMS2 -> capture request has
   Idempotency-Key + webhook_url; replay the canned message.finalized
   webhook twice -> store: one event, state delivered, SMSAttempt
   DLR RESOLVES (the D6 proof: attempt returns delivered not timeout);
   inbound message.received -> InboundTopic fires + dup suppressed on
   redelivery; STOP reply -> opt-out set + confirmation sent; second
   send attempt to opt-out contact -> refused.
4. -race on sms + telnyx + comm/http.
5. Merge order: plan/telnyx-texting -> main; #132 + dispatch board
   consume the seams afterwards.

## Review Checklist

- [ ] Ported-algorithm source comments present everywhere (D1 audit).
- [ ] Signature verify runs on RAW body before any parse (verify-then-parse).
- [ ] 401/200/500 posture matches the contract table.
- [ ] SPKI + raw key paths both tested (matrix port cases incl corrupted-header).
- [ ] No Redis/Postgres imports; uuid from pkg/id or documented crypto/rand.
- [ ] DLR resolve proof test exists and passes (D6).
- [ ] Opt-out consulted on ALL SendSMS2 paths (D5).
- [ ] Disabled posture byte-identical (D3).

## Open Questions

- Q1: MMS media — send MediaURLs field exists in contract; inbound
  media stored but not surfaced to bus InboundMessage.Text (strip).
  v1 sends text-only via contact channel; MMS later.
- Q2: multi-recipient `to` arrays beyond first entry — contract takes
  first; confirm no scenario needs full list.
- Q3: Should inbound non-STOP/YES messages publish a generic contact
  note to the ledger (employees "aware of texts")? Deferred to the
  dispatch tree.


## Implementation Record (2026-11-10)

All 12 nodes COMPLETE; commits 63482243/3837ff9a/e8e2dfa4 (webhook),
da648093/48f635e8/a8186602 (send), fe478143/044228ac/065337b3 (inbound
+ D6 close), 4d0a0687/dc560d0f/99326c6c (replies). Orchestrator
amendments: legacy webhook-test supersession (202->202 posture, unknown
200) shipped in e8e2dfa4; gate mask-corruption repair on
messaging_send2_test.go:197 in 48f635e8. Two contract readings ratified
at gates: finalized status verbatim pass-through; YES split from START
(A1). NonceStore eviction-ghost policy documented in 3837ff9a for
review. Master integration steps 2-5 executed: 9-package suite
-count=1 green; -race green on telnyx/sms/voice-telnyx (+daemon arming
-race green at leaf validation); the fake-Telnyx end-to-end (step 3) is
realized by the §T3/§T4 + §K3 daemon gates driving the armed handler
against httptest stubs — incl. DLR-resolve, redelivery dedupe,
STOP->opt-out, refused-send-before-dial. Merged to main after this
record.