# HTTP surface - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit. Do NOT use read_file
> on existing source files - explore with search_files or terminal cat.
> Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** HTTP routes on the daemon: POST /api/v1/voice/webhook (call control), GET-upgrade /api/v1/voice/media (Telnyx media WS), start-frame reader, signature gates, multiuser auth posture
- **Dependencies:** leaf 02 (telnyx parse/verify), leaf 05 (Manager)
- **Estimated Context:** 80K
- **Concurrency Group:** C
- **Files this leaf owns:** internal/comm/http/voice.go; surgical route registrations in internal/comm/http/server.go (registration lines only)

## Goal

Telnyx's two entry points into the daemon. Webhook: verify signature ->
ParseCallEvent -> Manager.HandleCallEvent -> 200 (streaming.* no-ops
included, so Telnyx never redelivers). Media: verify socket signature ->
accept WS upgrade -> read start frame (skip connected, bounded 30s/50
frames) -> hand to Manager.OnMediaStart with a per-call splice runner
(leaf 06 RunSplice injected as the dial func).

## Context

Borrow shape from uptractor core/internal/webhooks/handlers.go (Media
handler, readStartFrame with the connected-skip fix) - but WITH signature
verification (uptractor used InsecureSkipVerify; forbidden here).
websocket accept: github.com/coder/websocket (same lib as leaf 06).
Route registration: server.go mux.HandleFunc pattern
("POST /api/v1/..." style, see handleSelfImproveTrigger registration).
Auth posture: these routes are called by TELNYX, not by GUI clients -
they must NOT require the dev key/API key; they are gated by Ed25519
signature instead. Check how the server applies auth middleware and
exempt ONLY these two paths, with the signature check mandatory (a
missing public key config = routes disabled with a loud startup log,
never signature-optional). multiuser.enabled posture does not change
this (owner-trusted flat path stays byte-identical - AGENTS.md rule).
The media route derives stream_url for signature verification from
voice.public_stream_url + the media path + any query - it must match
byte-exactly what leaf 11/05 passed to streaming_start (coordinate: the
Manager builds the URL; the route verifies against the same builder -
expose StreamURL() and use it, never re-derive).
WS classification (internal/comm/AGENTS.md): any bus events published
here (voice.call.*) are "event" class, never chat_message. New 2+ segment
topics need explicit WS relay subscription patterns if GUI-visible - they
are NOT GUI-visible in this tree; do not add relay subscriptions.

## Verified Anchors

- ~/git/aiedge-uptractor/core/internal/webhooks/handlers.go:134-259 - Media handler + readStartFrame (borrow with signature added)
- internal/comm/http/server.go:1720 - mux.HandleFunc registration pattern
- internal/comm/http/server.go:186 - agentAPIHandler RouteRegistrar precedent for optional route groups
- internal/voice/telnyx (leaf 02) - VerifyWebhookSignature, VerifyMediaSocket, ParseCallEvent
- internal/voice/manager.go (leaf 05) - HandleCallEvent, OnMediaStart, StreamURL

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/comm/http/voice.go
type VoiceRoutes struct{ ... }
func NewVoiceRoutes(mgr *voice.Manager, verify VerifyFuncs, cfg VoiceRoutesConfig, logger *slog.Logger) *VoiceRoutes
func (v *VoiceRoutes) Register(mux *http.ServeMux)   // no-op + WARN log when public key unset
type VerifyFuncs struct{ Webhook func([]byte, http.Header) error; Media func(http.Header, string) error }
type VoiceRoutesConfig struct{ MediaPath string; SpliceRunner func(ctx context.Context, ccid string, conn WebSocketConn, start StartFrame) error }
type StartFrame struct{ CallControlID, Encoding string; SampleRate int }
```

### What This Leaf Consumes

- C4 verify funcs + ParseCallEvent (leaf 02); C5 Manager (leaf 05); leaf 06 RunSplice via SpliceRunner injection (wired in leaf 14)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Webhook route: signature fail -> 401 no parse; streaming.* -> 200 no manager call for action events; call.* -> HandleCallEvent; manager error -> 500 | Create: internal/comm/http/voice.go, voice_test.go | go test -p 2 ./internal/comm/http/ -run TestVoiceWebhook | - | PASS | Given bad sig; When POST; Then 401 and zero manager calls |
| T2 | Media route: signature over timestamp|stream_url verified before upgrade; fail -> 403, no WS | (same) | go test -p 2 ./internal/comm/http/ -run TestVoiceMediaAuth | - | PASS | Given tampered url header; When upgrade; Then refused |
| T3 | Start-frame reader: connected frame skipped, bounded (30s/50), encoding+rate extracted; PCMU sets transcode flag on StartFrame | (same) | go test -p 2 ./internal/comm/http/ -run TestVoiceStartFrame | - | PASS | Given connected then start; When read; Then ccid+encoding returned |
| T4 | OnMediaStart handoff: SpliceRunner invoked under per-call ctx; second start for same ccid no-op (idempotency through the manager) | (same) | go test -p 2 ./internal/comm/http/ -run TestVoiceMediaHandoff | - | PASS | Given duplicate start; When handled; Then runner called once |
| T5 | Public key unset: Register logs WARN once, routes return 503 (fail-closed, never signature-optional) | (same) | go test -p 2 ./internal/comm/http/ -run TestVoiceRoutesDisabled | - | PASS | Given no key; When webhook POST; Then 503 |
| T6 | Plain-HTTP loopback listener for tunnel ingress: when voice.loopback_plain_addr is set, a second listener serves ONLY the two voice routes in cleartext on loopback; main TLS listener unchanged; unset (default) = no extra listener; binds loopback only (refuse non-loopback addr with named error) | Create: internal/comm/http/voice_plain.go | go test -p 2 ./internal/comm/http/ -run TestVoicePlainListener | - | PASS | Given loopback_plain_addr=127.0.0.1:9090; When server starts; Then webhook POST over http://127.0.0.1:9090 verifies + handles; Given 0.0.0.0:9090; Then startup error names non-loopback |
| T7 | DTMF frames: media-adjacent {"event":"dtmf"} frames decode as a counted no-op (never an error, never reach the splice); counter logged at call end | Modify: internal/comm/http/voice.go | go test -p 2 ./internal/comm/http/ -run TestVoiceDTMF | - | PASS | Given a dtmf frame; When handled; Then no error, no audio spliced, count in end-of-call log |
| T8 | server.go registration lines (behind cfg.Voice.Enabled) + fmt/vet | Modify: internal/comm/http/server.go | go build ./internal/comm/http/; gofmt -l internal/comm/http; go vet ./internal/comm/http/ | - | clean | Given voice.enabled; When server builds; Then routes registered |

## Self-Verification Checklist

- [ ] No InsecureSkipVerify anywhere (grep)
- [ ] Signature verification cannot be skipped by config absence (503 path)
- [ ] stream_url for media verify comes from Manager.StreamURL(), not re-derived
- [ ] server.go diff is registration-only (no refactors)
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] Auth exemption is surgical: only the two voice paths bypass key auth, and only with mandatory signatures
- [ ] readStartFrame bounded (no unbounded goroutine on a silent socket)
- [ ] WS classification untouched; no chat_message leaks
- [ ] multiuser byte-identical posture preserved

Output: APPROVED or specific gaps.

## Notes

Telnyx redelivery: any 5xx on a webhook causes retries - manager errors
that are permanent (untracked id) should map to 200-with-log, not 500,
once observed live; keep 500 for transient only. Record the mapping
choice in code comments.
