# Research archive: signal-cli voice/messaging + Telnyx SMS (2026-11-08)

> Condensed from the delegated investigation (full report posted in session;
> verdict summary in GH issues #119/#120/#121). Leaf agents read THIS file,
> not the web. Re-verify at build time: signal-cli docs lag its code.

## Signal voice (signal-cli >= v0.13.0, PR #1932 merged 2026-04)

- Run: `signal-cli -u +NUMBER jsonRpc --socket <path>` (JSON-RPC 2.0 over unix socket).
- Call methods: `startCall` (recipient UUID/number, audio options) -> call id; `acceptCall`, `hangupCall`, `rejectCall`, `listCalls`; `subscribeCallEvents` (subscriptionId per upstream API change).
- Notifications: method `callEvent`; states RINGING_INCOMING | RINGING_OUTGOING | CONNECTING | CONNECTED | RECONNECTING | ENDED (ENDED reason carries busy/timeout/declined/hangup).
- Audio: THREE modes. (1) host default devices (needs virtual devices: PulseAudio null-sink/loopback on Linux, BlackHole on macOS) - host-global, fragile; (2) device-hint flags (names shift between 0.13.x point releases - verify against installed binary); (3) SOCKET AUDIO via the Rust `signal-call-tunnel` helper (RingRTC PCM over a socket) - PREFERRED for meept. The tunnel Rust source (in signal-cli tree) is the framing authority; RingRTC network leg is 48k, I/O leg 16k or 48k - resample in the daemon adapter to L16 s16le@16k 640B/20ms.
- Constraints: 1:1 calls only; one signal-cli process per account; treat ONE live call per account as the operating point (matches the registry's one-live-per-target rule). TURN: Signal service issues credentials automatically; check UDP 3478 + TURN/443 reachability if audio fails behind symmetric NAT.
- Account model: signal-cli is a LINKED device on a dedicated number's account; primary device must stay active or linked devices get disconnected (relink runbook + alerting needed). Primary registration displaces the number from any phone app - avoid.
- Maturity: young; early 0.13.x fixed call-audio/hangup bugs - pin the newest release; expect source-reading.

## Signal messaging (same daemon process - zero extra infra)

- Send: json-rpc `send` {recipient|recipients, message, attachments? [paths or base64]} -> message timestamp (delivery handle). Also `typing` {recipient, stop}, `sendDeliveryReceipt`, `sendReadReceipt`. Groups via group id (out of scope this tree).
- Receive: json-rpc NOTIFICATIONS while daemon runs (no polling): method `envelope` -> source (number), sourceUuid (canonical per-person KEY - use UUID), sourceName, timestamp, dataMessage {groupContext?, body, attachments[], timestamp}. Dedupe by (sourceUuid, timestamp).
- Abuse posture: Signal rate-limits aggressively; throttle outbound (few/min per account), allowlist recipients, back off on 4xx-style failures; privacy-setting rejections arrive as send failures - treat as channel-unavailable, not daemon errors.
- signal-cli-rest-api (bbernhard): does NOT wrap call methods; native json-rpc preferred for messaging too.

## Telnyx SMS

- Same Mission Control API key as Call Control (one Broker.Value caller); SEPARATE Messaging Profile required; a number can carry voice (Call Control connection) AND SMS (messaging profile) simultaneously.
- Send: POST https://api.telnyx.com/v2/messages {from, to, text, messaging_profile_id?} -> data.id (DLR correlation). Errors: standard {errors:[{code, detail}]}.
- Webhooks: messaging profile webhook_url; SAME Ed25519 telnyx-signature scheme as Call Control - REUSE the existing verifier. `message.received` = inbound (payload: id, from.phone_number, to[].phone_number, text, direction, received_at). `message.finalized` = terminal DLR (status: delivered|failed; also message.sending/sent intermediates).
- E.164 gate: strict ^\+[1-9]\d{7,15}$ on input.
- 10DLC: US local-number A2P needs brand+campaign registration (days-weeks + carrier fees). FAST PATH: verified toll-free number (~$0.01/msg outbound, ~$0.003-0.005 inbound). Ops decision, documented not coded.
- Pricing source: telnyx.com/pricing/messaging (verify at decision time).

## Addressing / fallback

- Signal key: Signal UUID (numbers can re-register; store UUID once observed). Signal usernames unreliable in signal-cli - do not use.
- SMS key: E.164. Voice (Telnyx): E.164.
- Contact record: phone E.164 (always) + nullable signal_uuid. Fallback detection: Telnyx voice = existing no-answer/hangup outcomes; Signal voice = callEvent terminal states (seconds); Signal msg = send failure; SMS = message.finalized failed. SMS is the universal floor (last in any default chain).

## Effort estimates (Go daemon, investigated)

- Signal text messaging: 4-6d. Signal voice: 8-12d. Telnyx SMS: 3-5d.

## Sources

- https://github.com/AsamK/signal-cli + PR #1932 + wiki
- https://github.com/bbernhard/signal-cli-rest-api (calls NOT wrapped)
- https://developers.telnyx.com/docs/messaging (+10DLC guides), https://telnyx.com/pricing/messaging
- https://existential.audio/blackhole/ (macOS fallback audio)
