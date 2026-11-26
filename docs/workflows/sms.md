# SMS Leg (Telnyx Texting)

Plan tree: `docs/plans/20261110-telnyx-texting/` (dependency of #132,
SMS acknowledgments for dispatch). Branch landed from
`plan/telnyx-texting`. Daemon wiring: `internal/daemon/messaging_arming.go`,
`internal/daemon/sms_reply_glue.go`, `internal/daemon/contact_wiring.go`.

## What it is

A production-grade Telnyx SMS leg for meept: hardened webhook ingress,
hardened outbound send, a durable inbound/outbound message store with
bus topics, ARMED inbound/finalized hooks, and STOP/HELP/YES reply +
consent handling. Borrowed algorithms (not infrastructure) from the
owner's vetted strategist-usa provider workers (decision D2: SQLite +
in-process structures only — no Redis/Postgres/BullMQ).

## Opt-in posture (D3)

The whole leg ships DISABLED:

- `[voice.messaging].enabled` gates outbound send (false default: every
  attempt refuses before any HTTP call).
- `[voice.messaging].inbound_enabled` is the D3 kill switch for the
  inbound leg (false default). Unarmed boot keeps the historical
  Verify-only webhook posture; the SMS store (`$MEEPT_HOME/sms/sms.db`)
  and the replay NonceStore are created only when
  `enabled || inbound_enabled`.
- Disabled daemon behavior is byte-identical to the pre-branch boot
  (explicit false is honored; never force-defaulted true at the load
  boundary).

Config keys (`MessagingConfig`, internal/config/schema.go):
`enabled`, `from_number`, `profile_id`, `dlr_wait_seconds` (0 = the
telnyx default 30), `webhook_base_url` (empty = no webhook_url sent on
sends; the channel derives `{base}/api/v1/voice/messaging/webhook`),
`stop_reply`, `help_reply` (zero defaults — reply WORDING defaults live
in the `sms` pkg), `inbound_enabled`.

### Outbound egress gates (issue #165; fail-closed since M14)

Three more keys bound the outbound send path:

- `allowed_numbers` — exact E.164 recipient allowlist. **Default
  changed (M14, 2026-11-25): empty (the shipped default) now refuses
  ALL outbound SMS** with `ErrRecipientNotAllowed` before any HTTP
  POST, matching the voice dial gate (`voice.allowed_numbers` null/
  empty refuses all outbound dials). List E.164 recipients to enable
  sends. The pre-M14 posture (empty = all recipients, warning only) is
  retired. A construction-time security warning is still emitted when
  the allowlist (or a cap) is unset.
- `max_messages_per_call` — MESSAGES PER SEND CALL (one `SendSMS2`
  invocation). Zero (default) = unlimited.
- `max_messages_per_day` — MESSAGES PER UTC DAY. Zero (default) =
  unlimited. The counter is in-process; it resets on daemon restart.

All three gates run at the TOP of `SendSMS2`, BEFORE the Telnyx HTTP
POST. `ErrRecipientNotAllowed` and `ErrSendCapExceeded` are
`errors.Is`-checkable. The production client gets these options from
`internal/daemon/contact_wiring.go`. When messaging is enabled with no
allowlist or cap, the arming path logs a single startup warning
(`messagingUnrestrictedWarning`, internal/daemon/messaging_arming.go);
note that under the M14 posture an unset allowlist no longer means
"any number may be contacted" — sends are refused until
`allowed_numbers` lists recipients. The outbound voice dial used by
the same contact chain has always had this fail-closed allowlist:
`voice.allowed_numbers` null/empty refuses ALL outbound dials (inbound
calls are not gated).

## Packages

| Package | Role |
|---------|------|
| `internal/sms` | durable inbound/outbound store (`sms_events_raw` + state table, idempotent event-id insert-ignore, monotonic upsert) + typed bus topics `sms.inbound.received` / `sms.state.changed` + the reply classifier + in-memory PendingConfirms registry (D4: durable registry belongs to the dispatch tree) |
| `internal/telnyx` | outbound send leg: `SendSMS2` (per-message webhook_url + rate-limit errors), `SMSAttempt` (ordered send → DLR wait, `ResolvePending(providerMessageID, status)` wakes it), consent guard (`Attempt` refuses opted-out contacts with `contact.ErrOptedOut` BEFORE any HTTP call) |
| `internal/voice/telnyx` | webhook ingress: signature v2 (ed25519, 300s-past/60s-future window), replay `NonceStore` (TTL 6m, LRU 10k), tolerant normalizer → the normalized `Message` the seams carry |
| `internal/comm/http` | `messaging.go` webhook route: 401 before any parse/nonce touch; success → 202; noop/unknown/duplicate → 200 ack (stops Telnyx redelivery); seam error → 500 (Telnyx redelivers) |
| `internal/contact` | `messaging_opt_out` consent column (additive `ALTER ... NOT NULL DEFAULT 0`, guard-tolerant) consulted by ALL outbound sends (D5; voice consult deferred to the dispatch tree) |

## Arming (D6 — the dead-seam fix)

`MessagingWebhookDeps.OnInbound/OnFinalized` existed before this tree
but NOTHING wired them, so DLRs were unresolvable. When armed,
`buildMessagingDeps` (messaging_arming.go) assigns:

- `OnInbound` → `sms.Store.AppendEvent` + `sms.InboundTopic` publish +
  the consent reply hook (STOP/START toggle, HELP auto-reply).
- `OnFinalized` → audit row (direction outbound) + monotonic
  `UpsertState` + `sms.StateTopic`/`StateChanged` publish (only when
  the upsert applied) + `resolve(ProviderMessageID, Status)` waking any
  blocked `SMSAttempt` BEFORE answering 202.

The `SMSAttempt` and the consent hook close over handles that only
`BuildContactChain` has (contacts.db + the messaging client), so both
reach the armed seams through LATE-ARMED slots
(`dlrAttemptSlot` / `consentReplySlot` on `VoiceWiring`): components.go
arms them after `WireVoice` returns; an unarmed slot reads nil and the
seam does nothing (nil-safe both ways).

## Store lifetime

`Components.SMSStore` is built inside the voice block when
`enabled || inbound_enabled`, fail-soft (warn, nil store) on failure.
Closed in `stopComponents` AFTER the voice stack and the employee
ledger (the callstore ordering precedent — webhook consumers stop
before their store, so a late in-flight webhook write still finds an
open handle), and on BOTH `WireVoice` failure paths (fatal error +
degraded sidecar). Nil on every disabled boot.

## Consent semantics

- STOP from a KNOWN contact sets `messaging_opt_out`; the confirmation
  reply rides the documented TCPA exception (it deliberately bypasses
  the opt-out guard — an opted-out contact solicited the reply).
  Known gaps (accepted at review): STOP from an UNKNOWN number is not
  persisted (no ghost row — the opt-out is lost if the contact is
  seeded later), and HELP auto-replies go to opted-out contacts too.
- The toggle is a read-modify-`Put` on the contact record. TODAY the
  glue is the only production writer; any future contact-CRUD path that
  re-`Put`s a record silently clears `messaging_opt_out` — a dedicated
  column-update method is the follow-up (review finding, telnyx-texting
  merge review 2026-11-16).

## Webhook status codes

Success → 202 (distinguishes "processed" from the ack-noop replies);
noop/unknown event type → 200 ack; duplicate event id → 200 ack-noop
(seam not called); bad signature → 401; malformed body → 400; seam
error → 500 so Telnyx redelivers. Telnyx treats any 2xx as accepted.

## Tests

Hermetic e2e: `e2e/suites/telnyx-sms` (manifest-registered; the
`internal/sms` + `internal/telnyx` path_map entries route census there).
Unit gates: the plan tree's 00T leaves (sections R/S/N/T/K/P/H) over
`internal/{sms,telnyx,voice/telnyx,contact,comm/http,config}` +
`internal/daemon/messaging_arming_test.go` / `sms_reply_glue_test.go`.

## Deferred to #132 (dispatch tree)

Consumers of the two sms bus topics (currently publishers only —
baselined in `scripts/orphan-baseline.txt` with that justification),
the durable YES-registry, and the voice-leg consent consult.
