# Telnyx client - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing gate tests ALREADY EXIST
> (parked .staged from leaf 00T) - make them pass; do NOT delete, skip,
> weaken, or rewrite any gate test. Do NOT commit. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat. Write
> once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** internal/voice/telnyx: borrowed call-control client, webhook parse, Ed25519 signature verification (webhook + media socket), secrets allowlist entry
- **Dependencies:** none (gate tests 00T T2)
- **Estimated Context:** 70K
- **Concurrency Group:** A
- **Files this leaf owns:** internal/voice/telnyx/*.go, scripts/audit-secrets-value-callers.py (one allowlist row)

## Goal

The daemon-side Telnyx surface, borrowed from uptractor
(core/internal/telnyx/{client.go,callcontrol.go}) with the spec's four
changes: L16/16000 requested with start-frame authority, signature
verification on BOTH webhook and media socket, agent-dial permitted (the
policy gates live in leaf 11, not here), and codec-parameterized
StartStreaming.

## Context

Borrow source (read with terminal cat, it is outside this repo):
- ~/git/aiedge-uptractor/core/internal/telnyx/client.go - Client struct, apiKey, DefaultBaseURL, httpClient
- ~/git/aiedge-uptractor/core/internal/telnyx/callcontrol.go - CallEvent, flexibleString, ParseCallEvent (incl. streaming.* no-ops), postCall, snippet, AnswerCall, StartStreaming, HangupCall, dialRequest, DialOut

Module path changes: `coordinator/internal/telnyx` -> `github.com/caimlas/meept/internal/voice/telnyx`. No jargo, no uptractor imports.

Signature scheme (Telnyx docs, verified 2026-11): webhooks and media
sockets use the account Ed25519 keypair. Media socket headers:
`telnyx-signature-ed25519` (base64) + `telnyx-timestamp` (unix seconds);
signed string is `{telnyx-timestamp}|{stream_url}` where stream_url is the
EXACT url passed to streaming_start including query. Webhook signing uses
the same keypair over the raw body per Telnyx's webhook signature scheme;
implement VerifyWebhookSignature(body, headers, pub) matching Telnyx's
documented header names, and reject timestamp skew beyond a configurable
window (default 300 s) to block replay.

The API token reaches the Client via internal/secrets Broker.Value at the
daemon wiring site (leaf 14). This leaf only defines the constructor
taking an already-resolved key string; the Value CALLER added in leaf 14
joins the allowlist - but this leaf adds the allowlist ROW pattern comment
so the wiring leaf does not forget.

## Verified Anchors

- ~/git/aiedge-uptractor/core/internal/telnyx/callcontrol.go:65-88 - ParseCallEvent exact shape (borrow verbatim, adapt module)
- ~/git/aiedge-uptractor/core/internal/telnyx/callcontrol.go:90-103 - callRequest (extend with BidirectionalSamplingRate)
- ~/git/aiedge-uptractor/core/internal/telnyx/callcontrol.go:184-241 - dialRequest + DialOut (borrow verbatim)
- scripts/audit-secrets-value-callers.py - ALLOWED_VALUE_CALLERS map

## Interface Contracts (From Parent)

### What This Leaf Exposes

C4 verbatim (master.md): Client, DialOut, AnswerCall,
StartStreaming(ctx, ccid, streamURL, Codec), HangupCall, ParseCallEvent,
VerifyWebhookSignature, VerifyMediaSocket, Codec{BidirectionalCodec,
SamplingRate} with defaults L16/16000, CallController interface satisfied
by *Client.

### What This Leaf Consumes

- none (leaf-internal; config comes in at wiring, leaf 14)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Activate gate tests; confirm RED | mv internal/voice/telnyx/client_test.go.staged -> client_test.go | go test -p 2 ./internal/voice/telnyx/ | package missing | RED confirmed | Given parked tests; When activated; Then compile fails on undefined package |
| T2 | Borrow client + callcontrol (module-adapted); StartStreaming gains Codec param emitting stream_bidirectional_codec + stream_bidirectional_sampling_rate | Create: internal/voice/telnyx/client.go, callcontrol.go | go build ./internal/voice/telnyx/ | - | compiles | Given Codec{L16,16000}; When StartStreaming; Then POST body carries stream_bidirectional_mode=rtp, codec L16, rate 16000 |
| T3 | ParseCallEvent borrowed verbatim incl. streaming.* no-ops and flexibleString | (same files) | go test -p 2 ./internal/voice/telnyx/ -run TestParseCallEvent | - | PASS | Given streaming.started webhook; When parsed; Then no error, event type preserved |
| T4 | VerifyWebhookSignature + VerifyMediaSocket (Ed25519, base64, timestamp skew window) | Create: internal/voice/telnyx/signature.go | go test -p 2 ./internal/voice/telnyx/ -run TestVerify | - | PASS | Given valid sig over "{ts}|{url}"; When verified with matching pub; Then nil. Given tampered body or skew > window; Then error |
| T5 | Allowlist row + justification for the leaf-14 Broker.Value caller (comment-anchored so the audit script sees the pattern; the real caller lands with wiring) | Modify: scripts/audit-secrets-value-callers.py | python3 scripts/audit-secrets-value-callers.py | - | exit 0 | Given the script run; When no Value caller exists yet; Then still green |
| T6 | Format/vet/race | - | gofmt -l internal/voice/telnyx; go vet ./internal/voice/telnyx/; go test -p 2 -race ./internal/voice/telnyx/ | - | clean | Given the package; When checked; Then no findings |

## Self-Verification Checklist

- [ ] Gate tests passing untouched
- [ ] Borrowed code carries the spec's four changes (no InsecureSkipVerify anywhere; codec parameterized; no trust ban baked into the client)
- [ ] No token logged (postCall error snippet excludes headers; apiKey never in error text)
- [ ] resp.Body always drained+closed
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list (name every departure from the borrowed source)

## Review Checklist (For Review Agent)

- [ ] C4 signatures exact
- [ ] Diff against uptractor source: only module path + the four spec changes + Codec param
- [ ] Signature verification cannot be bypassed by a missing header (missing header = error, not pass)
- [ ] No secrets in logs/errors

Output: APPROVED or specific gaps.

## Notes

Uptractor observed Telnyx redelivering webhooks rejected with non-2xx -
ParseCallEvent must keep streaming.* as parseable no-ops or redelivery
storms return. timeout_secs stays 30 default but is a parameter at the
leaf-11 call site, not hardcoded here.
