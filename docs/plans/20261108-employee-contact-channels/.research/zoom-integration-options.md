# Research archive: Zoom integration options (2026-11-08)

> Condensed from the delegated investigation; full verdict posted on GH #115/#116/#117.
> This tree does NOT implement Zoom. It is the recommended NEXT tree (observe leg
> first). Kept here so the follow-up tree does not re-research.

## Verdict (ranked)

1. BUILD: native RTMS observe leg in Go (~6d). Pure WS+HMAC, no SDK, no cgo; audio arrives L16 16k mono 20ms - byte-identical to meept's child chunk format. Zoom staff now steer AI notetakers to RTMS over SDK bots.
2. BUY: speak-in-meeting via self-hosted Attendee (source-available Meeting Bot API, WS audio in/out, ~5-8d integration, no per-hour metering). Alternative: Recall.ai Output Media (~$0.75-0.90/hr all-in, ~1wk, webpage-based output). DEFER native Meeting SDK (~15d, C++ .so, 48k virtual-mic quirks, ZAK/OBF churn, marketplace review risk; Zoom deprioritizing it).
3. SKIP: reverse-engineered Go web clients (ToS/ban risk); Vexa for Zoom (transcript-only, joins via web client).

## RTMS observe - concrete requirements (re-verify at build)

- App: General app on Zoom Marketplace, USER-MANAGED (S2S OAuth cannot carry RTMS scopes). Scopes: meeting:read:meeting_audio, meeting:read:meeting_transcript (+meeting:update:participant_rtms_app_status[:admin] for REST start).
- Webhook only: meeting.rtms_started / .rtms_stopped; signature verification; payload carries meeting_uuid, rtms_stream_id, server_urls.
- Handshake: HMACSHA256(client_id + "," + meeting_uuid + "," + rtms_stream_id, client_secret); signaling WS (msg_type 1) then media handshake (msg_type 3) PER MEDIA TYPE - media_type:32 unified mode is DEPRECATED.
- Audio: content_type 2 (raw), 16kHz mono L16, send_rate 20ms, mixed (user_id 0) or multi-stream. Keep-alive every 10s; 3 missed = terminated; reconnect ~30s media/60s signaling; no session resume.
- Start: auto-start user setting, or REST PATCH /live_meetings/{id}/rtms_app/status (invitee must have JOINED; host token can start pre-join since Jan 2026).
- Cost: Zoom Developer Pack credits (~0.01/min audio, 0.02/min w/ transcript ≈ $0.55-1.10/hr list; 20 free trial credits; concurrency default 2000).
- Consent: native indicator; host/admin gates; external meetings depend on host org settings (unreliable across account boundaries) - internal-tenant use with own OAuth user is the unambiguous case.
- Go: official zoom/rtms-samples (MIT) working_go boilerplate + RTMS_CONNECTION_FLOW.md; zoom/rtms Go bindings "Planned" only - implement protocol directly (~400 LOC).

## Speak leg - vendor landscape

- Attendee (attendee.dev / github attendee-labs/attendee): Django single-Docker self-host; POST /api/v1/bots with meeting URL; WS audio IN+OUT (documented voice-agent path, ships voice-agent-example + attendee-pipecat); ZAK/OBF token support; RTMS ingestion also supported; license is source-available - read before redistributing.
- Recall.ai: SaaS; POST /bot; Output Media = webpage inside bot does getUserMedia + TTS playback (<50ms added); Meeting Direct Connect = botless RTMS (credits bundled). $0.50/hr + $0.15/hr STT + $0.10/hr web_4_core.
- MeetingBa.as: speaking-bots API, WS streaming in/out; prepaid tokens ~$0.35-0.50/hr + plan fee; credentials stored in THEIR vault (AES-256-GCM) - secrets-lane concern.
- Vexa (Apache-2.0, self-host): transcript-only, no speak; Zoom joins via web client in 0.12.x.
- All vendor Zoom joins now require authorized tokens (OBF/ZAK; MeetingBa.as cites March 2, 2026 deadline) - a small Zoom app on meept's side is needed regardless.

## Meeting SDK native (if ever)

- Linux SDK only; C++ .so from Marketplace; JWT (SDK key/secret) + ZAK/OBF for joins; no SDK licensing fee; raw receive needs recording rights/token; SEND via setExternalAudioSource + JoinVoip, frames 48kHz mono s16 @10ms (resampler needed); dev creds fine for internal unpublished use; marketplace review only if published. Headless sample: zoom/meetingsdk-headless-linux-sample (Docker).

## Sources

developers.zoom.us/docs/rtms/* (+ events, quickstart-rest-api); zoom/rtms-samples; devforum threads 145391 (pricing), 146344 (SDK vs RTMS guidance), 146469 (SDK audio send quirks); recall.ai/pricing + docs; pricing.meetingbaas.com; vexa.ai + docs; attendee.dev + github; zoom/meetingsdk-headless-linux-sample.
