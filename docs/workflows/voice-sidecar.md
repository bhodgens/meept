# Voice sidecar (jargo)

**Status**: Implemented
**Date**: 2026-11-02 (spec rev 2); as-built appended 2026-11-04; rev 3
(local leg, plan docs/plans/20261106-voice-local-leg/) amended in
2026-11-07

This spec covers both voice legs: the Telnyx phone channel and the local
(client audio) channel. It is not the client text-to-speech path in
`docs/workflows/tts.md`. That path runs in the TUI and the Flutter process.
It never reaches the daemon. A phone call must own capture and playback.

Rev 2 adds: Telnyx call-out (borrowed from the aiedge-uptractor
coordinator, see "Borrowed from uptractor"), media frame ordering, the
barge-in contract, the model-contention mitigation, and concurrent voice
instances.

Rev 3 adds: the local leg (section "Local leg (C11)") - a real-time voice
conversation with an employee over local client audio (mic + speaker)
instead of a Telnyx phone call. Same child, same talk loop, same
supervisor gate, same session/binding/day-brief machinery. The phone leg
stays fully functional; the two legs run CONCURRENTLY on one child, one
text server, one registry, and one call store.

## Purpose

A caller talks to a meept employee as if on a phone call. The employee can
call out when work needs a human. The employee can answer an inbound call.
Both directions carry the day's prior context. The employee can start work
while the caller waits. A second employee can block a fact before anyone
hears it. Multiple voice instances can run at the same time, to different
numbers, on different model endpoints.

Example. The caller asks where John is, and whether John reached the
Worthington site. The voice side says "Hold on, let me check." A worker
looks up both facts. A supervisor allows the site status. The supervisor
denies John's location. The caller hears only the allowed sentence.

## Non-goals

- Do not add a roster agent type. A phone path is a channel an employee
  invokes.
- Do not link jargo into `meept-daemon`. A linked Go package cannot be
  unloaded.
- Do not use a Go plugin. A plugin loads into the daemon.
- Do not use Telnyx Conversation Relay. That product owns the ears and the
  mouth.
- Do not use a speech-to-speech model for the answer. This spec has no
  LFM3-Omni path.
- Do not arm a cron row to redial. A cron row has no expiry.
- Do not send the call on `employee.notify` or the speak push bridge.
- Do not call `request_handoff` from the call. That path needs a task step.
- Do not route calls through the `ExecuteBot` adapter. It mints a fresh
  `bot-exec-*` conversation id per round and drops the constitution prompt,
  so no call would carry prior context.

## Three roles

| Role | Process | Owns |
| --- | --- | --- |
| Voice child | separate binary | ears, turn-taking, barge-in, mouth |
| Employee | meept daemon | tools, draft reply, other agents |
| Supervisor | meept daemon | allow or deny each spoken sentence |

Ears means speech-to-text. Mouth means text-to-speech. Barge-in means the
caller talks over the agent, and the agent stops speaking.

The voice child uses [jargo](https://github.com/gojargo/jargo) (BSD-2-Clause).
jargo is a Go library. A call is a chain of processors. The child adds two
processors: one that sends caller text to meept, and one that speaks only
text meept has allowed.

Speech-to-text and text-to-speech providers are config. The spec does not
pick a vendor. jargo already wraps several. The first build may use one of
those wrappers. Swap the provider without a change to the socket.

## Process model

`voice.enabled` defaults to false. A false flag starts no child and loads
no ONNX Runtime. ONNX Runtime is the shared library that runs the local
speech detector. jargo loads it at run time, not at compile time.

On the first call, the daemon starts the child. The child loads jargo and
ONNX Runtime. The child hosts one jargo pipeline per live call (see
"Concurrency and instances"). Further calls reuse the same child.

After the last call ends, an idle timer stops the child. Proposed default:
30 seconds. The idle timer fires only at zero live calls. The operating
system then frees the model heap.

A killed daemon must not leave the child behind. Use the same parent-death
supervisor as a local model server. On the next boot, the orphan sweep reaps
a leftover child only when the same ownership rules hold. Do not kill a
child owned by another daemon instance.

Missing ONNX Runtime fails the call start. Do not answer the phone with
barge-in off. The caller hears nothing from this channel. The daemon logs
the missing file path. It does not log secrets.

The published jargo Docker runtime image is amd64 only. An Apple Silicon
host needs a native arm64 child. Do not use that image on arm64.

## Telnyx integration

The daemon owns Telnyx. The voice child never sees Telnyx.

### Public ingress

The media stream is a WebSocket that Telnyx opens toward meept. The daemon
listener stays loopback (`transport.http.addr` is `127.0.0.1:8081` in the
shipped config). A public `wss://` URL must front it: a tunnel or reverse
proxy terminates TLS and forwards to the loopback listener. The URL is
config (`voice.public_stream_url`). An empty URL refuses both directions
before any HTTP call, so a missing origin never places a paid call or
strands an answered caller.

**The plain loopback listener (`voice.loopback_plain_addr`).** The main
listener is TLS-mandatory, so a TLS-terminating tunnel cannot post to it
in cleartext. When `voice.loopback_plain_addr` is non-empty (and
`voice.enabled`), the daemon boots a SECOND listener
(`internal/comm/http/voice_plain.go`, wired at boot by
`internal/daemon/voice_plain_boot.go` from `VoiceWiring.Start`) that
serves ONLY the two voice routes in cleartext on that address — every
other path 404s. The address MUST be loopback (127.0.0.0/8 or ::1;
"localhost" only after the actual bind is re-verified): a non-loopback
value raises the named `ErrNonLoopbackVoiceAddr` and fails daemon startup
loudly, never degrades. The listener rides the voice subsystem's stop
context and shares the main chain's protections: the same per-IP rate
limiter (tuned by `transport.http.rate_limit_rpm` /
`rate_limit_burst` / `trust_xff`) and access logging. Telnyx signatures
stay mandatory on both listeners — plain HTTP never relaxes them.

### Signatures

- Call-control webhooks: verify the Telnyx Ed25519 signature before
  parsing. Reject on failure.
- Media socket: Telnyx signs every media WebSocket connection with
  `telnyx-signature-ed25519` and `telnyx-timestamp` headers. The signature
  covers `{telnyx-timestamp}|{stream_url}`, where `stream_url` is the exact
  URL passed to `streaming_start`, including any query string. Verify
  before accepting the upgrade. Do not accept with origin checks skipped
  and no signature (uptractor used `InsecureSkipVerify`; do not copy that).

The Ed25519 public key comes from the Telnyx Mission Control portal and is
stored like other provider material. The API token stays in the secrets
broker as a `Broker.Value` caller (add the caller to
`scripts/audit-secrets-value-callers.py`). Never put the token in the child
environment, a log line, or a config file the child can read.

### Codec and framing

Request on `streaming_start` (and on the outbound `dial` when supported):

- `stream_bidirectional_mode`: `rtp`
- `stream_bidirectional_codec`: `L16` (case-sensitive)
- `stream_bidirectional_sampling_rate`: `16000`

Telnyx `L16` on this stream is signed 16-bit little-endian mono PCM at
16 kHz (`s16le`), not big-endian RTP L16. Forward payloads without byte
swaps or resampling. One 20 ms frame at 16 kHz mono is 640 bytes.

The start frame reports what was actually negotiated
(`start.media_format.encoding`, `start.media_format.sample_rate`). Read it;
do not assume the request was honored. If the encoding is `PCMU` (8 kHz),
transcode mu-law to PCM16 16 kHz before the child, and encode outbound the
same way. A PCMU 20 ms frame is 160 bytes. Chunk size is codec-derived,
never a fixed constant.

### Frame order

Telnyx does not guarantee media frame order. Frames carry `sequence_number`
and `media.timestamp`. The daemon reorders before the child:

- Buffer inbound frames in a per-call reorder window
  (`voice.reorder_window_frames`, default 10). Emit the contiguous run.
- A gap that persists past `voice.reorder_gap_ms` (default 60) is filled
  with silence for the missing duration and counted as a metric. Never
  splice out-of-order audio into the child; a swapped pair corrupts the
  transcript.
- Backpressure: the per-call audio queue is bounded. A full queue marks the
  call degraded; after a grace window the daemon hangs up. Do not silently
  drop the oldest frame (uptractor's arrival-order forward and the rev-1
  drop-oldest rule are both rejected: a mid-utterance hole is a wrong
  transcript, not a late start).

### Socket lifecycle

Telnyx sends a `connected` frame first, then `start`. Skip non-start frames
(bounded read: 30 s, 50 frames). The `start` frame carries
`call_control_id`; that id keys the call everywhere. Route the socket
through an idempotent media-start registration keyed by `call_control_id`
so a Telnyx re-send cannot spawn two bridges on one socket. Parse
`streaming.started|stopped|failed` webhook events as no-ops with a 200
response; rejecting them triggers Telnyx redelivery (observed live in
uptractor).

## Outbound dial (call-out)

Borrowed from uptractor `Client.DialOut` / `Manager.TrackOutbound`, with
the trust model changed: in uptractor only a human CLI may dial; here the
phone employee may dial, behind an allowlist and the effects ledger.

Flow:

1. Trigger: a bus event, a scheduled assess, or a decision inside a live
   call. Only the phone employee (or another employee publishing a
   call-request event the phone employee consumes) reaches this path.
2. Policy checks, all before any HTTP call: `voice.enabled`; target in
   `voice.allowed_numbers`; `voice.public_stream_url` non-empty;
   `voice.connection_id` non-empty; a concurrency slot free; no live call
   to that number. Any failure refuses the dial with a named reason.
3. Reserve the effect: `internal/effects` ledger keyed on
   (employee_id, target, request_id). A retry of the same request must not
   ring twice. A refused or failed dial completes the effect with its
   outcome.
4. Write the audit row (direction `outgoing`, target number) BEFORE the
   POST, so the attempt is attributable even if the dial fails.
5. `POST /v2/calls` with `{connection_id, to, from, timeout_secs}`
   (default 30). Parse `data.call_control_id`.
6. Track the id. On its `call.answered`, call `streaming_start` with the
   negotiated codec params. Do NOT call `AnswerCall` on an outbound leg:
   Telnyx already answered the dialed leg, and `AnswerCall` returns 404 on
   a live call and aborts the session (uptractor live drill, 2026-10-28).
7. Park the employee goal round for the life of the call. Each `prompt`
   and `say` publishes a progress event so any attached turn liveness sees
   life. (The goal loop itself has no silence reaper; the progress events
   matter for turn-attached paths and observability, not for a reaper that
   does not exist.)
8. Release on any terminal outcome: `call.hangup`, the no-answer
   `timeout_secs` expiry, or `streaming.failed`. Release unparks the goal,
   frees the concurrency slot, and completes the effect. An unanswered
   call must never hold a plan slot: employees often allow one active
   plan, so a missed call would otherwise block the employee.

## Inbound answer

As shipped by plan docs/plans/20261110-caller-authz (issue #125), the
inbound gate is an ACTION gate, not an ANSWER gate (decision O1). This
section replaces the earlier "caller must be in voice.allowed_numbers"
spec, which the shipped chain deliberately dropped:

1. Webhook `call.initiated`, Telnyx Ed25519 signature verified (see
   "Signatures"). `from`/`to` arrive flat or as `{"phone_number": ...}`;
   parse both shapes.
2. Answer EVERY call. `voice.allowed_numbers` does NOT gate inbound —
   it is OUTBOUND-only (see "Outbound dial"). An unknown number gets an
   employee to talk to; it does not get an open action surface.
3. Resolve the caller ONCE at the answer site
   (`internal/daemon/voice_wiring.go` spawn seam, 2 s budget): the
   contacts store lookup yields `callerid.Info{Number, Name, Approved,
   Known}`. Approved = the contact row's `voice_approved` column
   (decision O2; default FALSE). An unknown number is NOT approved
   (`Known=false`). Every failure mode (contact feature off, store
   error, expired budget) degrades fail-soft to `Info{Number}` with a
   WARN — answering is never blocked (O1).
4. The identity rides the registry `Instance.Caller`;
   `bindingTurnRunner.Run` stamps it onto EVERY turn context
   (`callerid.With`, alongside the employee-id stamp). Voice loops
   execute as the employee id (decision O5), so the per-employee
   constitution tool gates (steps 1a/1b) finally apply on call turns —
   voice loops used to run under agent id `default` and skipped them.
5. The agent executor injects the presence-only details keys
   `caller_approved` / `caller_known` (never the number or name) into
   every tool-call permission check; the employee `PreExecChecker`
   step 1c (`internal/employee/enforcement.go`) then trims an
   unapproved or unknown caller to the message lane
   (`[ledger_append, send_agent_message]`, config
   `voice.unapproved_caller_allowed_tools`, decision O3). Anything the
   employee constitution already forbids is denied even earlier —
   steps 1a/1b run first and their deny wins (O5); a constitution
   allow does NOT rescue an unapproved caller. Every other tool is
   denied with the frozen model-facing text: "This caller is not
   approved for that action. Take a message instead." (reason token
   `caller_not_approved`).
6. The 1c denial is warning severity with NO auto-pause (contrast
   `never[]`, which is a critical stop): refusing a stranger caller is
   a routing decision, not an employee emergency. Each denial emits
   one `voice.unapproved_action` audit row (tool + call_control_id)
   through the checker's audit seam — nil-safe by contract. Wiring
   caveat as shipped: the daemon's `RegisterPreExecCheckers` boot site
   does not call `SetAuditStore` on the per-employee checkers, so on a
   stock daemon the seam is uninstrumented and the row is a silent
   no-op (the deny itself is unaffected);
   `internal/daemon/caller_matrix_test.go` pins the frozen row shape
   with the seam armed. Flagged to the owner (leaf 02-02 report).
7. `AnswerCall`, spawn or attach the call session, then on `call.answered`
   run `streaming_start`.
8. Telnyx opens the media socket; the lifecycle in "Socket lifecycle"
   applies.
9. A `call.hangup` for an unknown id is a nil no-op. Teardown is idempotent
   against duplicate or late hangups.

Residual risk (decision O4, owner Open Question Q1): `ledger_append`
sits in the message lane, and step 1c cannot distinguish a
state-setting (`set_status`) append from a note-taking one, so an
unknown caller CAN drive employee-ledger rows including state changes
until Q1 is settled. Accepted for this tree; pinned in the shipped
behavior by `internal/daemon/caller_matrix_test.go`. The signal leg
shares the turn-stamp site but not the phone leg's contact-resolve site
(master Q2 fast-follow): signal instances carry a zero `Info`, which
step 1c treats as unknown = unapproved — fail-closed, never an open
gate.

## Barge-in contract

Barge-in is kept end to end. The pieces, in order:

1. The child detects caller speech during playback (Silero VAD + Smart
   Turn) and sends an `interrupt` frame on the text socket.
2. The daemon, for THAT call only:
   - stops writing outbound frames;
   - sends `{"event":"clear"}` on the media socket, which flushes the
     Telnyx playback buffer so the caller hears silence immediately
     (without this frame the caller still hears the old sentence);
   - cancels the in-flight employee draft and any pending supervisor
     verdict for that call, so the answer to the pre-interrupt question
     never resurfaces;
   - leaves every other live call untouched.
3. The child drops its own queued TTS audio for the call.
4. Outbound writes are paced at one 20 ms frame per 20 ms real time. Never
   burst faster: the carrier playout buffer overflows as loud crackle over
   the speech (uptractor live drill 2). Pacing also guarantees a barge-in
   lands between frame writes.
5. The agent speaks first on a connected call (a fact-free filler or
   greeting). Without it both sides wait in silence, because the pipeline
   otherwise replies only to a finished utterance (uptractor live drill 1).

## Model contention mitigation

Two local model calls per fact sentence (employee draft, then supervisor
verdict) is the latency risk. Mitigations, all required:

- Per-call endpoint pinning: each voice instance runs its employee on a
  dedicated voice model endpoint (the employee's `model` field / a
  models.json5 alias, `voice.voice_model` as the default pin). Chat
  traffic never enters the voice endpoint queue, and a phone call never
  blocks a chat turn.
- The supervisor is a short allow/deny call on a second, faster endpoint
  (`voice.supervisor_model`) with a hard timeout
  (`voice.supervisor_timeout_ms`, default 400). On timeout, error, or
  quota the caller hears the configured fallback line ("I cannot confirm
  that yet.") and the call continues. A voice call is NEVER quota-parked:
  parking is a text-turn mechanism; a live call cannot wait for a provider
  reset.
- Pipelining: while Telnyx plays allowed sentence N, the supervisor checks
  sentence N+1. The wait hides behind speech.
- Only a final transcript (`prompt` with `last: true`) starts an employee
  turn. Partials update the transcript preview only. Firing a model turn
  per partial fragment cannot keep up on a local model.
- Per-call limits: max call duration (`voice.max_call_minutes`, default
  15) and a silence prompt at `voice.silence_prompt_seconds` (default 45),
  then hangup. The silence clock is TURN-AWARE: while an employee turn is
  in flight (final prompt dispatched, reply not yet fully spoken) the
  silence window is suspended, so a slow cloud model (30-90s) is never
  reaped mid-generation; the window re-arms fresh once the reply is
  spoken. The optional `voice.turn_timeout_seconds` hard cap (default 0 =
  disabled) bounds a hung LLM, since the suspended silence clock no
  longer can.

## Concurrency and instances

A voice instance is the tuple (employee, call_control_id, media socket,
pinned model endpoint). Multiple instances run concurrently to different
numbers.

- The employee invoke mutex serializes goal rounds, so one employee holds
  at most one live call. N concurrent calls need N voice employees; each
  may own its number binding and endpoint pin. This is the multi-agent
  shape: several employees of the same voice kind, not one employee on
  several sockets.
- One live call per target number, globally: the live-call map plus the
  effects key refuse a second concurrent dial to the same number.
- `voice.max_concurrent_calls` caps instances. Over the cap, inbound calls
  are not answered and outbound dials return busy with a named reason.
- The child hosts one jargo pipeline per call. Each call gets its own audio
  socket pair named by `call_control_id`, so a slow call cannot block
  another call's audio. The text socket is shared; every text frame carries
  `call_id`.
- If measurement shows one child cannot host N pipelines on this host, the
  fallback is one child per call under the same parent-death supervisor,
  reaped by the call context. The socket contract does not change.

## Call context (the day brief)

Built at answer time and at dial time, injected into the employee prompt
(the model does not have to remember to search):

- Activity rows since LOCAL midnight via `internal/activity`
  (`Query.Since/Until`). One calendar convention per day-keyed store: a
  UTC-derived day key reads a different day than the local one. Summaries
  are capped (200 runes each) and the list is capped.
- Transcripts of earlier calls with this number today.
- Durable memory only: name, preferences, standing rules. Capped block.
  `memory_search` has no time filter, so the day facts come from the
  brief, never from model recall.
- The employee's active goals.

Persistent phone session: one meept session per (number, employee), bound
via `sessions.employee_id`. Inbound and outbound calls append to the same
session, so a call carries the prior call. `session_id` and
`conversation_id` stay distinct.

## Boundary

```
Caller
  |  phone audio
Telnyx
  |  media-stream WebSocket (L16 s16le, 16 kHz, one stream per call)
meept-daemon  (signature check, reorder, pace, per-call splice)
  |  raw audio frames, per-call unix socket pair, capped queue
voice child (jargo, one pipeline per call)
  |  caller text (call_id, prompt/interrupt/end), shared text socket
meept-daemon
  |  employee draft (pinned voice endpoint),
  |  then supervisor verdict (fast endpoint, hard timeout)
  |  allowed text only (say, per call_id)
voice child
  |  speech audio
meept-daemon -> Telnyx
  |  caller hears the allowed sentence
```

The daemon accepts the Telnyx socket. The public URL stays on the daemon.
The daemon does not decode speech and does not run a model. It verifies
signatures, reorders frames, and splices audio. Request Telnyx
`stream_bidirectional_codec` `L16` and `stream_bidirectional_mode` `rtp`.
Only one bidirectional stream is allowed per call.

jargo has a Twilio phone example. It has no Telnyx transport. Do not port
Twilio into this design. The child sees PCM frames. The daemon speaks
Telnyx.

## Socket

One shared text socket per daemon; one audio socket pair per call, named by
`call_control_id`. Text frames are JSON and every frame carries `call_id`.
Audio frames are raw PCM on the per-call socket, so a slow text reader
cannot block audio and one call cannot block another.

Text from the child to the daemon:

- `setup` - call id, from, to, direction, negotiated codec + sample rate
- `prompt` - caller text. `last: false` is a partial (transcript preview
  only). `last: true` is final and starts the employee turn
- `interrupt` - caller spoke over the agent (barge-in contract above)
- `end` - call ended

Text from the daemon to the child:

- `say` - text to speak. `last: false` streams a sentence. `last: true`
  ends the turn
- `clear` - stop playback and drop queued speech
- `end` - hang up

A `say` frame is sent only after the supervisor allows that text. A fixed
filler is the one exception. The filler list is config. The default list
has one line: "Hold on, let me check." A filler must not contain a fact.
The greeting on connect is also filler-class: fact-free, interruptible.

## Call flow (inbound, condensed)

1. Telnyx sends `call.initiated`. The daemon verifies the Ed25519
   signature (inbound is answer-all; see "Inbound answer" — the caller
   allowlist is OUTBOUND-only).
2. The daemon starts the child if needed, answers the call, and starts the
   media stream with L16/16000.
3. The media socket opens. The daemon verifies the socket signature, reads
   the start frame (skipping `connected`), and registers the pipeline by
   `call_control_id` (idempotent).
4. The daemon parks the employee goal for the life of the call and builds
   the day brief.
5. The child sends the caller question as a final `prompt`.
6. The daemon sends the filler `say` at once.
7. The employee starts the lookup on its pinned endpoint. It may ask
   another employee to do the work. It does not speak the result yet.
8. The supervisor reads the draft on the fast endpoint. It returns allow
   or deny per sentence within the hard timeout.
9. The daemon sends `say` for allowed sentences only, paced 20 ms per
   frame. A denied sentence never enters a `say` frame, so the child never
   speaks it.
10. Barge-in at any point runs the barge-in contract for this call only.
11. Hangup sends `end` both ways. The call session stores call id, target,
    direction, state, and transcript. The goal unparks. The concurrency
    slot frees.

Outbound differs only at the front: steps 1-2 become the dial flow above
(no `AnswerCall`; `call.answered` triggers `streaming_start`).

## Config

Proposed keys, all under `voice`:

- `enabled` - bool, default false
- `idle_stop_seconds` - int, default 30
- `onnx_runtime_lib` - path passed only to the child
- `stt_provider` - string
- `tts_provider` - string
- `filler` - list of strings, default one hold line
- `supervisor_employee` - employee id that must allow each spoken sentence
- `supervisor_model` - alias for the fast supervisor endpoint
- `supervisor_timeout_ms` - int, default 400
- `voice_model` - alias for the default per-call employee endpoint pin
- `public_stream_url` - public wss URL fronting the loopback listener
- `connection_id` - Telnyx Call Control application id
- `from_number` - caller id for outbound dials
- `allowed_numbers` - E.164 list; OUTBOUND-only gate (plan
  20261110-caller-authz: inbound is answer-all, actions are gated per
  caller by the contacts `voice_approved` flag + PreExec step 1c)
- `timeout_secs` - outbound ring timeout, default 30
- `max_call_minutes` - int, default 15
- `silence_prompt_seconds` - int, default 45; the clock pauses while the
  employee turn is in flight and re-arms after the reply is spoken
- `turn_timeout_seconds` - int, default 0 (disabled); optional hard cap
  on one in-flight voice turn (bounds a hung employee LLM)
- `fresh_session_per_call` - bool, default false (opt-in). When true,
  each voice call opens a NEW session instead of appending to the shared
  (employee, target) conversation, so turn history never snowballs; the
  day brief stays the only cross-call context channel. Per-employee
  override: `employee_leg[].fresh_session` (absent = inherit). Fresh
  sessions are named `voice <employee> <ccid>` (distinct from the shared
  `voice:<employee>:<number>` key, so they can never hijack it).
- `max_concurrent_calls` - int
- `reorder_window_frames` - int, default 10
- `reorder_gap_ms` - int, default 60

Provider API keys are secrets-broker values. The child asks the daemon
for a key over the unix socket at start. The daemon reads `Broker.Value`.
The key is not in the child environment, the command line, or a log. The
key lives in child memory only while the child runs. That is required,
because the child calls the provider.

## Borrowed from uptractor

Source: `~/git/aiedge-uptractor/core` (module `coordinator`).

Included as-is (shapes proven on live Telnyx calls):

- `DialOut`: POST /calls `{connection_id, to, from, timeout_secs:30}`,
  parse `data.call_control_id` (internal/telnyx/callcontrol.go).
- `TrackOutbound` + `WaitCall`: track the dial id; its `call.answered`
  starts streaming; a done channel closes on hangup
  (internal/callsvc/manager.go).
- The no-`AnswerCall`-on-outbound rule (404 aborts the session).
- Pre-dial refusals: empty public URL or connection id refuses before any
  HTTP call; the attempt row is logged before the dial.
- Start-frame reading: skip `connected`, bounded wait, read
  `call_control_id` + encoding (internal/webhooks/handlers.go).
- Idempotent media-start registration keyed by `call_control_id`.
- `streaming.*` webhook events parsed as no-ops (redelivery avoidance).
- Flexible `from`/`to` parsing (flat string or `{phone_number}`).
- PCMU fallback transcode when the start frame says PCMU.
- Greeting-first rule; 20 ms outbound pacing; `{"event":"clear"}` to Telnyx
  on barge-in; per-connection STT construction.

Changed (do not copy the uptractor shape):

- Trust: uptractor bans agent dialing (human CLI only). Here the phone
  employee may dial, gated by allowlist + effects ledger + audit row.
- Frame order: uptractor forwards in arrival order with no
  `sequence_number` handling. This spec reorders (see "Frame order").
- Socket auth: uptractor accepts the media WebSocket with
  `InsecureSkipVerify` and no signature check. This spec verifies the
  Ed25519 socket signature before accept.
- Codec: uptractor hardcodes PCMU/8 kHz. This spec requests L16/16000 and
  derives chunk size from the negotiated start frame (640 bytes per 20 ms
  at L16 16 kHz; uptractor's fixed 160-byte chunk is PCMU-sized).
- Process shape: uptractor is one call per process. This spec is
  multi-instance (one child, one pipeline per call).

## Open points

- Measure the child resident size on this Mac before the idle timer is
  tuned. jargo does not publish a memory figure. Silero's JIT file is about
  2 MB. Smart Turn v3 int8 is 8 MB. ONNX Runtime is extra and unmeasured.
- Measure one-child-N-pipelines vs one-child-per-call on this host; the
  socket contract supports both.
- Confirm the generated `telnyx-go` method names for `dial` and
  `streaming_start` before import. The module is
  `github.com/team-telnyx/telnyx-go/v4`. Uptractor hand-rolls the HTTP
  client; borrowing that client shape is acceptable if the generated SDK
  lags.
- Confirm `streaming_start` accepts `stream_bidirectional_sampling_rate`
  16000 with L16 on the account; the start frame is the authority either
  way.
- The first provider pair (stt/tts) is not chosen.
- The public ingress choice (Cloudflare quick tunnel like uptractor's
  `make tunnel`, or a fixed tunnel/reverse proxy) is not made. A quick
  tunnel URL changes per run; `SetStreamURL`-style live update of
  `public_stream_url` is then required.
- Tuning of `reorder_window_frames` / `reorder_gap_ms` on the real network
  path.
- No CLI or GUI surface in this spec. A later change adds `meept voice`
  status and a matching Flutter control. UI text is lowercase.

## As-built (2026-11-04, plan docs/plans/20261102-voice-sidecar/)

All leaves 00T-16 complete. Deviations from this spec, as named in the
completion reports (leaf ids cited):

- Boot degrade posture (leaves 14/15): a missing secrets broker or an
  unresolvable `telnyx_api_key` DEGRADES the boot to voice-disabled with
  a loud WARN instead of failing the daemon. Voice is opt-in; the
  dial-time gates and route-level fail-closed posture keep every call
  path refused without credentials. A HALF-configured voice block (bad
  public key, empty default employee) still fails the boot loudly.
- New config key `voice.default_employee` (leaf 15): this spec's Config
  section had no owner for inbound calls. The inbound spawn path
  Acquires the registry slot for that employee; empty value is a named
  boot error. Also gate-caught: `max_concurrent_calls=0` made every
  Acquire busy; the Registry now treats cap <= 0 as unlimited and
  DefaultConfig sets 2.
- Inbound registry Acquire (leaf 15 fix of a leaf-14 gap): the C1 setup
  router resolves talk loops through Registry.Live(), so a call that
  skipped Acquire could never arm its loop. The inbound SessionSpawner
  now Acquires (ccid, default_employee, from) before arming.
- U1000 sweeps (leaves 11/15): dead symbols in committed files swept at
  leaf 11; unused-but-activation-surface e2e fake helpers pinned via
  function-value vars at leaf 15.
- `FailOutcome` (leaf 05, consumed at leaf 11): the manager gained a
  Fail variant with an explicit outcome so the outbound no-answer timer
  reports OutcomeNoAnswer distinctly from OutcomeFailed.
- Spawn wrap decision (leaf 13): the child process lifecycle WRAPS
  llm.RuntimeProcess rather than hand-rolling kill/pid logic; the
  supervisor mode (`--supervise-parent`) gained a stdin seam so the
  voice child's stdin-only start token survives the wrapper (leaf 14).
- jargo pinned at v0.1.0 (leaf 07): v0.2.0+ needs go 1.27 and the tree
  pins 1.26.x. Six API drifts vs this spec's assumptions recorded in
  internal/voicechild/jargo.go comments (pipeline package alias, no
  STT/TTS interfaces, TranscriptionFrame signature, Worker.Run
  blocking, processor.Base embed, StartFrame worker-owned).
- Frame codec relocated (leaf 07): the C1 codec lives in
  internal/voiceframe (stdlib-only, shared by daemon and child) instead
  of internal/voice alone; internal/voice keeps an alias.
- macOS sun_path limit (leaf 15): unix socket paths over 103 bytes fail
  to bind; the default macOS per-user TMPDIR pushes e2e sandbox paths
  past the limit, so the suites shorten the test process TMPDIR once.
  Production sockets under $MEEPT_HOME are unaffected.
- Supervisor gate model: `supervisor_model` resolves through the
  daemon's fast-model chatter seam; nil chatter degrades the gate to
  allow-all with a logged warning (leaf 14 wiring).
- Child providers (leaf 08): Deepgram STT + OpenAI TTS adapters behind
  the leaf-08 interfaces; jargo internals reused instead of a
  hand-rolled loop (leaf 08 wrap-vs-handroll decision).
- Manifest (leaf 15): voice-lifecycle / voice-bargein /
  voice-concurrency suites registered in e2e/manifest.json; the full
  inbound C1-to-talk-loop leg is skipped honestly (needs live model
  turns).
- CLI surface deferred as planned: no `meept voice` status command in
  this tree. The user surface is the employee tool `voice_call`
  (internal/tools/builtin/voice_call.go) plus the inbound path; the CLI
  status command and a Flutter control remain future work.
- The full invariant contract lives in internal/voice/AGENTS.md.

## As-built (2026-11-07, plan docs/plans/20261106-voice-tierA-qwen3-tts/)

Tier A closes the wiring gaps that kept every call on stub audio. The
spec above stays rev 2; this section records the shipped deltas.

### Config-driven providers (C1 keys frame)

The daemon emits one extended C1 `keys` line per call through
`TextServer.SendRaw`, immediately after the talk loop registers on setup
(internal/daemon/voice_keys.go; idempotent setups never re-send, refused
setups never send). The line carries both key fields AND the vendor
selectors:

- `stt_vendor` / `stt_model` / `stt_base_url`
- `tts_vendor` / `tts_model` / `tts_base_url` / `tts_voice`

Config mapping (C4): `voice.stt_provider` -> `stt_vendor`,
`voice.stt_model` -> `stt_model`, `voice.tts_provider` -> `tts_vendor`,
`voice.tts_model` -> `tts_model`, `voice.tts_base_url` ->
`tts_base_url`, `voice.tts_voice` -> `tts_voice`. An empty vendor falls
back to the hardcoded defaults (deepgram / openai), so existing
deployments keep their behavior. Keys resolve through the secrets broker
in-daemon (`deepgram_api_key`, `openai_api_key` via `Broker.Value`,
allowlisted); a missing broker/secret sends `""` - the child factory
fails THAT call only, the child and other calls stay up. Key material
never appears in logs, env, or argv (the shared `voiceframe.Frame`
carries no key fields by design; `SendRaw` is the keys seam).

### Config reference rows (under `voice`)

- `stt_provider` - STT vendor selector; empty = `deepgram`
- `stt_model` - STT model; empty = vendor default (`nova-3`)
- `tts_provider` - TTS vendor selector: `openai` (cloud, key required)
  or `qwen3` (local, keyless)
- `tts_model` - TTS model; empty = vendor default (`gpt-4o-mini-tts`
  cloud; qwen3 keeps it empty = server's loaded default)
- `tts_base_url` - OpenAI-compatible TTS endpoint; REQUIRED for qwen3
  (empty is the named `ErrMissingTTSBaseURL` refusal for that call),
  empty = `api.openai.com` for openai
- `tts_voice` - TTS voice name; empty = vendor default (`alloy`)

### Tier A: local Qwen3-TTS

`qwen3` is a keyless local vendor speaking any OpenAI-compatible
streaming endpoint (vLLM-Omni or a community server the user deploys;
D3 keeps the server out of repo scope). The adapter posts
`stream=true`, `response_format=pcm` to `<tts_base_url>/v1/audio/speech`
and consumes raw s16le mono 24 kHz incrementally - first audio leaves
the adapter as the vendor emits it, never buffered whole - resampled to
the negotiated rate inside the adapter. No Authorization header is sent
(D2 keyless). The child process itself is `cmd/meept-voice`; its
factories dispatch on the per-call config the keys frame carries (C3),
so provider selection is per call, not per deployment.

### Barge-in trigger: STT partial (D1 deviation)

Barge-in is triggered by STT partial transcripts, NOT by ONNX/jargo VAD.
The child pipeline (internal/voicechild/pipeline.go) pumps daemon->child
PCM into the bound STT session; a non-empty partial while playback is
live trips `noteBargeIn` and emits the interrupt frame BEFORE the
partial's prompt frame (C5 order: playback stops before the new turn's
say can start), exactly once per barge-in until the next say re-arms
playback. Finals start employee turns as before; empty-text silence
results emit nothing.

**DEVIATION from spec rev 2:** the ONNX hard dependency is GONE for
Tier A. The spec said "a phone call without barge-in is refused" and
described the ONNX probe as fatal; as built, the ONNX probe stays
non-fatal and jargo VAD is an optional future upgrade. Zero new runtime
deps; the trigger is fully unit-testable. `voice.onnx_runtime_lib`
remains accepted config (passed to the child env) but nothing requires
it.

### E2E coverage of the integrated chain

- voice-lifecycle `TestVoiceKeysToProviders`: the daemon's keys line
  (qwen3 + loopback fake endpoint) reaches the fake child in
  real-providers mode, the REAL qwen3 adapter constructs keyless from
  it, and synthesized PCM reaches the fake Telnyx media socket as
  decoded audio frames. The pre-tierA named skip
  (TestVoiceLifecycleGreetingAudioAtTelnyx) is retired - the gap it
  documented is now covered by a live assertion.
- voice-bargein `TestVoicePartialBargein`: a STT-partial during live
  playback (local leg) trips exactly one interrupt, the talk loop clears
  the call, and the fresh final starts a new employee turn whose reply
  reflects the interrupting prompt (FakeLLM request-log observable).

## Local leg (C11) - spec rev 3

The local leg is a real-time voice conversation with an employee over
LOCAL client audio (mic + speaker) instead of a Telnyx phone call. Same
child, same talk loop, same supervisor gate, same session/binding/day
brief machinery. Purposes:

1. Local testing of the whole voice stack without outbound calls or
   cloud STT/TTS.
2. Agent interaction over voice for any same-user client (CLI now, TUI
   as a later part).
3. Concurrent legs: Telnyx stays fully functional; the two legs share
   one child, one text server, one registry, and one call store.

### The open frame (CL1)

The daemon sends one C1 `open` frame per call, right after the AudioPair
exists, for BOTH legs (phone and local). The child creates its pipeline,
dials the C2 audio pair, stores the call identity, and answers with the
existing `setup` frame; the daemon's routeFrame arms the talk loop on
setup and the keys line follows (Tier A emission site). Frame additions,
all `omitempty`, zero value = pre-rev-3 behavior byte-exact
(internal/voiceframe/frame.go:40-46, EventOpen at :61):

```
Local      bool   `json:"local,omitempty"`
BargeInRms int    `json:"barge_in_rms,omitempty"`
```

Open frame shape (daemon -> child):

```
{"call_id":"<ccid>","event":"open","from":"<id>","to":"<id>",
 "direction":"inbound|outbound|local","codec":"L16","sample_rate":16000,
 "local":true,"barge_in_rms":3000}
```

Rules: vendors/models/voices NEVER ride the open frame - they ride the
Tier A `keys` frame. The open frame carries only call identity, the leg
marker, and the RMS threshold the local leg needs before any keys arrive.
Existing wire tests (byte-exact prompt/say lines) still pass. The phone
path's producer is internal/daemon/voice_wiring.go `sendOpenFrame`
(:689, warn-and-continue on send failure at :745); the local leg's is
internal/voice/local.go `sendOpenFrame` (:360).

### Leg matrix

A call is one of phone-inbound, phone-outbound, local; the daemon
originates local legs and the open frame's `direction` names the wire
value. Barge-in trigger, effects reservation, audit row, carrier, and
reorder/transcode policy differ per leg:

| Property | Phone (inbound) | Phone (outbound) | Local |
| --- | --- | --- | --- |
| Audio source | Telnyx media socket | Telnyx media socket | client dials the C2 unix pair (mic/speaker) |
| Identity (`direction`) | `inbound` | `outbound` | `local` |
| Target shape | caller E.164 (resolved to `callerid.Info` at answer; NOT allowlisted — plan 20261110-caller-authz) | allowlisted E.164 | `local:<label>` (label = config name or hostname), never E.164-normalized (internal/voice/dial.go:629-633) |
| ccid shape | `call_control_id` | `call_control_id` | `"local-" + pkg/id.Generate()` (internal/voice/local.go:237) |
| Registry leg | `phone` | `phone` | `local` (`Instance.Leg`, internal/voice/dial.go:555-569) |
| Effects reservation | no (inbound) | YES, before POST | NO (deliberate: no paid side effect) |
| Audit row before dial | no | YES | NO (deliberate: no external party) |
| Carrier | Telnyx POST/webhooks | Telnyx POST/webhooks | NONE - no telnyx import, no carrier seam (internal/voice/local.go:14-15,165) |
| Reorder window | YES (Telnyx untrusted order) | YES | NO - unix sockets preserve order (internal/voice/localsplice.go:8-12) |
| Transcode | PCMU negotiated = mu-law both ways | same | NO - L16 s16le@16000 only, anything else is a named refusal (ErrL16Only) |
| Barge-in trigger | STT partial (Tier A D1) | STT partial (Tier A D1) | RMS VAD, Local==true only (internal/voicechild/pipeline.go:430-432) |
| Keys frame | at EventSetup (Tier A) | at EventSetup (Tier A) | the wiring emits it at setup, same wire shape |
| Cap / one-live-per-employee / per-target | shared ACROSS legs (internal/voice/dial.go:621-628) | shared | shared |

### Local leg lifecycle (CL5)

`internal/voice/local.go` owns the lifecycle. Open order (LocalLeg.Open,
:222):

1. Gates: leg enabled (`voice.enabled` && `voice.local.enabled`, folded
   into one bool by the daemon glue) and non-empty employee id, both
   named refusals before any side effect (ErrLocalDisabled, ErrNoEmployee).
2. ccid = `"local-" + pkg/id.Generate()`; target = `"local:" + label`
   (label = the tool/RPC arg, then `voice.local.name`, then the
   hostname, :205-217).
3. Registry `AcquireLeg(ccid, employeeID, target, "local")` (:243) -
   ErrBusy / ErrDuplicateTarget surface unchanged; capacity and
   one-live-per-employee rules are shared across legs.
4. `Manager.TrackOutbound(ccid)` (:249) so FailOutcome / WaitCall /
   release fan-out work unchanged.
5. AudioPair under SocketDir (:256) - one listener each, one accept
   each; the pair PATHS go to the caller.
6. Send the open frame (:291) - identity + `local:true` +
   `barge_in_rms` + the CL4-resolved provider profile; the child builds
   its pipeline, dials the pair, answers with setup, and the wiring
   emits the keys line at the Tier A site. Send failure degrades to
   warn-and-continue on stub audio (:292).
7. RunLocalSplice in a goroutine (:304) - the client join grace bounds a
   never-dialed pair (ErrNoClient); the pre-accept "not connected"
   window is filtered, not terminal (:311).
8. Day brief + `Binding.OnCallStart` with direction `"local"` and number
   `"local:<label>"` (:324-339) - the same call-site shape as the phone
   path.

Teardown (End, :407, and any terminal splice error): `Manager.FailOutcome`
runs the once-per-outcome release fan-out (childproc note, talk-loop
cancel, registry release, binding end) exactly as a phone hangup does;
End additionally cancels the splice context and closes the audio pair.
Double End is idempotent; a foreign id is a named error
(ErrUnknownLocalCall). No carrier POST anywhere on this leg.

Local session persistence: `Binding.OnCallStart` binds the same
(number=local:<label>, employee) session the phone path binds, and the
call row + transcripts land in the same calls.db (direction `"local"`).

### Leg and voice selection (CL4, config verbatim)

Config, all under `voice`, additive; defaults preserve pre-rev-3
behavior byte-identically (internal/config/schema.go:5261-5264,
5277-5297):

```
default_leg        "phone"          // phone | local
tts_model          ""               // global TTS model (openai voice id etc.)
tts_voice          ""               // global TTS voice
stt_model          ""
local.enabled      false            // local leg master switch (opt-in)
local.stt_provider ""               // default "whisper"; empty = voice.stt_provider
local.tts_provider ""               // default "piper"; empty = voice.tts_provider
local.tts_model    ""
local.tts_voice    ""
local.barge_in_rms 3000             // s16 RMS threshold, local leg only
local.name         ""               // session label; empty = hostname
employee_leg       []                // per-agent defaults:
  // {employee, leg, tts_voice, tts_model, fresh_session?} so different employees default
  // to different legs/voices with no tool arg (the concurrent-legs case);
  // fresh_session (absent = inherit voice.fresh_session_per_call) picks
  // per-employee session policy (secretary per-call, companion shared)
```

Selection precedence (frozen, resolved in ONE place:
`voice.ResolveProfile`, internal/voice/profile.go:49):

- TTS voice/model: voice_call `voice` arg > `voice.employee_leg[employee]`
  > per-leg config (`voice.local.*` for local, `voice.*` for phone) >
  provider default (represented by an empty field).
- Leg: voice_call `leg` arg > `voice.employee_leg[employee].leg` >
  `voice.default_leg` (internal/tools/builtin/voice_call.go:157;
  an empty arg with no armed default seam means phone).
- LLM pin unchanged (`voice.voice_model` + employee Model - the LLM
  model is not the TTS voice; Tier A already separates them).
- `"local"` means the direct-TTS leg: agent speech is synthesized and
  played on the client device; no Telnyx anywhere on the call.

Local providers are SUBPROCESSES, never vendored or linked: whisper.cpp
STT (`whisper-cli`, internal/voicechild/providers/stt_whisper.go:22) and
piper TTS (internal/voicechild/providers/tts_piper.go:19). GPL as a
subprocess is allowed; both are keyless (empty keys ride the Tier A
keys frame per its keyless rule).

### Policy differences: effects, audit, carrier, VAD

These differences are DELIBERATE and pinned in code:

| Difference | Rule | Where pinned |
| --- | --- | --- |
| Effects reservation | local leg skips it (no paid side effect) | internal/voice/local.go:10-13 (mirror comment); phone path keeps the claim |
| Audit row | local leg writes none (no external party) | same mirror comment; phone path writes it before the POST |
| Carrier | local leg has NO telnyx import and NO carrier seam | internal/voice/local.go:14-15,165 (build-pinned; grep for telnyx in local.go/localsplice.go/voice_local.go finds comments only) |
| ONNX / barge-in rule | rev 2's "no call without barge-in (ONNX)" is superseded by Tier A (STT-partial trigger, ONNX non-fatal) for the phone leg; the LOCAL leg uses RMS VAD instead - 10 sustained chunks at 200 ms over `local.barge_in_rms` triggers one interrupt; RMS NEVER runs when Local==false | internal/voicechild/pipeline.go:430-432; phone legs keep the STT-partial trigger |

### LocalSplice (CL7)

`internal/voice/localsplice.go` `RunLocalSplice` is the carrier-free
media path - simpler sibling of splice.RunSplice:

- child .out -> client speaker, paced 20 ms per 640 B L16@16k chunk
  (same pacing rule; the client buffer underruns are the local crackle
  equivalent) (:14-17).
- client mic -> child .in, chunk-sized 640 B (:8-10). NO reorder window
  (unix sockets preserve order). NO transcode: the local leg negotiates
  L16@16000 only and REFUSES anything else (ErrL16Only, :49-51).
- Barge-in: interrupt for THIS ccid stops outbound writes, sends the
  clear frame to the CHILD (there is no carrier playout to flush - the
  child's TTS queue is what needs flushing), cancels the draft once,
  and re-arms on fresh child PCM (:19-26). Same per-barge-in latch
  contract the phone splice pins.
- Join grace: the client must produce its first byte within 10 s or the
  splice ends with ErrNoClient (:28-30) - a never-dialed leg cannot
  wedge the registry slot.
- Backpressure: a socket write error ends the leg with a named error;
  there is no drop-oldest path (:32-33).

### Control plane (CL8)

RPC (internal/rpc/voice_local.go, seam-based - internal/rpc never
imports internal/voice):

- `voice.local.open {employee_id, label}` -> `{call_id, in_path,
  out_path, session_id}`
- `voice.local.end {call_id}` -> `{ok}` (idempotent; double end is ok)
- `voice.local.status {}` -> `{calls: [{call_id, employee_id, target,
  since_rfc3339, leg}]}` - live calls for BOTH legs (phone rows carry
  leg `"phone"`, local rows `"local"`)

Named refusals: ErrVoiceLocalDisabled (unwired leg), ErrVoiceLocalUnknownEmployee
(checked before any side effect), ErrVoiceLocalBusy /
ErrVoiceLocalDuplicate (shared registry rules), ErrVoiceLocalUnknownCall
(End on a foreign id). Audio NEVER rides RPC: the client dials the C2
unix pair directly from the returned paths (same-user trust, the same
model as the RPC socket).

CLI (cmd/meept/voice.go):

- `meept voice local --employee <id> [--label <l>] [--in <wav>]
  [--out <wav>] [--list]` - opens the call over RPC, dials both pair
  sockets, and shuffles audio. `--in/--out` is FILE mode: a scripted,
  mic-free, deterministic leg (pure io.Copy both directions, canonical
  WAV in/out) for CI and demos. Without them, LIVE mode: ffmpeg
  avfoundation mic capture and oto speaker playback.
- `meept voice status` - one table of live calls for BOTH legs.
- SIGINT ends the call via `voice.local.end` before exit, so the release
  fan-out runs exactly once.

GOOS support: live mic capture is darwin-only (ffmpeg avfoundation);
any other GOOS refuses with a named error pointing at `--in` file mode
(internal/voice/clientaudio.go:89-90). File mode works everywhere.

### e2e coverage

Suite `e2e/suites/voice-local` (leaf 11, commit 73d5b900) runs the whole
local leg hermetically: RPC open -> pair paths -> scripted client PCM
through the REAL whisper adapter (fake subprocess shim) -> employee turn
on a fake LLM -> say through the REAL piper adapter -> paced PCM at the
out path -> voice.local.end -> registry empty + calls.db row with
transcripts. Two scenarios are registered in e2e/manifest.json
(voice-local-e2e-01, voice-local-refusals-01).

### Ended-state transition

`Binding.OnCallEnd` marks calls.db rows ended. It rides
`ReleaseHook -> heldCalls`, and the hold is registered by
`AcquireCallHold` on every live leg: local (`LocalDeps.Hold`), phone
inbound (spawn seam), and phone outbound (`DialConfig.Hold`). Release
is the manager hook exactly once; a nil hold seam degrades to no
ended-state. The phone leg also calls `Binding.OnCallStart` from
`holdPhoneCall` — without that row, OnCallEnd is a no-op.
