# Agent Invariants: Voice Sidecar

Referenced from the root `AGENTS.md`. The voice contract is not loaded on every
agent turn: read this file only for voice work (phone channel, local leg, TTS,
STT, frames, sockets, clips). Update the root one-line pointer and this file in
the same commit.

Full contracts live in `internal/voice/AGENTS.md` (frames, sockets,
reorder/pacing/barge-in, release fan-out, secrets lanes, child confinement,
one-live-call-per-employee, local-leg invariants, filler clip cache + lane
pools + farewell, partial context warm). User-facing docs are
`docs/workflows/voice-sidecar.md` (spec rev 3 + as-built) and
`docs/workflows/voice.md`.

## Hard rules

C1 text frames are byte-exact JSON lines. Every frame carries `call_id`. Only
final prompts (`last:true`) start a turn. `say` goes out only after the
supervisor allows (fact-free filler/greeting excepted).

C2 audio rides per-call unix socket pairs with codec-derived 20 ms chunks.
Barge-in `clear` affects the interrupted call only.

A voice call is never quota-parked. A supervisor timeout means fallback line
and the call continues. Hangups fan out release (slot, goal unpark, effects)
exactly once.

The Telnyx token resolves via `Broker.Value` in-daemon only. Provider keys
reach the child over the unix socket and live in child memory only.

jargo is imported ONLY by `cmd/meept-voice` and `internal/voicechild`
(compile-time pins). The registry enforces one live call per employee and per
target number, with outbound dials gated by allowlist + effects reservation +
audit-before-dial.

The LOCAL leg (phone + local run concurrently on one child/registry/store)
never imports telnyx and skips effects + audit by contract. Its barge-in is RMS
VAD gated to `Local==true`. `LocalSplice` has no reorder window and negotiates
L16@16k only.

A LOCAL CALLER IS ALWAYS APPROVED: the local leg's turn ctx is stamped
Approved/Known so the step-1c caller gate is inert and full tool use works.
`voice.unapproved_caller_allowed_tools` is the allow-list for UNAPPROVED
phone/signal callers only, never a ceiling on the trusted local operator
(detected via the explicit `Instance.Leg` field, never a target-prefix
heuristic).

BARGE-IN CONTRACT (2026-11-11): the client-speaker silence lane is a per-call
daemon LISTENER `<ccid>.c.ev` beside the client pair (JSON-lines, NEVER PCM -
the pair stays raw-audio-only) carrying `{"event":"clear"}`, emitted by
`VoiceWiring.routeFrame` once per latch transition and consumed by the CLI
drain's `sink.Clear()` (post-latch audio must die within one oto callback,
~20 ms - the fixed defect was a ~750 ms ghost tail from `SpeakerSink.Clear()`
having zero production callers). `drainLiveSpeaker` must not flush a post-clear
EOF tail. Full contract in `internal/voice/AGENTS.md` (Sockets section).

Operator telemetry: each live call carries a fixed per-call event ring
(`internal/voice/events.go`) fed by the talk loop's optional
`TalkDeps.OnEvent` hook + the frame router's `heard` append (nil-safe, no new
bus topic), read over the seam-based `voice.local.events` RPC and printed live
by `meept voice link` / `voice local`; `voice local` also prints the call's
stored transcript at call end over the read-only `voice.local.transcript` RPC
(call-scoped, best-effort).

The silence nudge's clock is AUDIO-AWARE on local legs via the optional
`TalkDeps.AudioActive` seam (fire re-arms while the agent's own audio is
active, bounded at 2 deferrals) and CALLER-AWARE via optional
`TalkDeps.CallerActive` (a loud inbound chunk inside 1 s re-arms a fresh
window and does not speak; nil = byte-identical - full contract in
`internal/voice/AGENTS.md`).

The optional SECOND cleartext listener (`voice.loopback_plain_addr`, leaf 10 T6)
is booted by `internal/daemon/voice_plain_boot.go` from `VoiceWiring.Start` when
the knob is non-empty, rides the voice subsystem's stop context, and MUST bind
loopback: a non-loopback address (name-checked AND actual-bind re-checked) is
`ErrNonLoopbackVoiceAddr` and fails the startup loudly, never degrades. Its mux
carries the same per-IP rate limit + access logging as the main listener's
voice chain.

FILLER CLIP CACHE + LANE POOLS (plan 20261113-filler-clips-and-context-warm
phase 1): every filler-class line (greeting/filler/silence nudge/fragment
clarify/farewell) belongs to one enumerable C5 lane pool (ladder row list > row
scalar > global list > global scalar > default; list "set" = non-empty, so
unset new keys are byte-identical; the farewell pool ships non-empty and speaks
a goodbye on a detected caller bye, exactly once per call). The daemon's clip
planner pre-renders misses to the child as C3 `clip_render` frames ONLY behind
the R4 gate (live child + complete resolved TTS identity + voice employee -
sidecar disabled writes nothing), the child caches PCM under
`$MEEPT_HOME/voice_clips/` keyed by the frozen C1 sha256 formula, and
filler-class `say` frames carry `clip_id`; a store hit replaces synth entirely,
a miss or an old child falls through to live synth byte-identically. TEXT stays
the transcript source - a clip never makes a second speech path.

PARTIAL CONTEXT WARM (phase 2, `voice.context_warm` opt-in default false):
debounced STT partials fire `llm.Client.Warm` (max_tokens=1, same rendered
prefix + session-affinity scope as the imminent turn) so the real turn pays
decode, not prefill; warm never starts a turn or tools, consumes no budget,
never touches blocks/metrics, and its failures are silent (DEBUG). Full
contracts (C1-C6) in `internal/voice/AGENTS.md`.

RUMBLE STT (invariant): native streaming vendor (partial/final/end over a WS at
`/v1/audio/transcriptions/stream`, keyless); the client-side pseudo-stream
proxy is BYPASSED for native vendors (rumble, deepgram): `stt_partial_ms = 0`,
and server-stable finals drive barge-in/endpointing. Whisper default and its
batch proxy opt-in are unchanged.

OPT-IN ENDPOINTING/LATENCY SELECTORS (plan 20261118-voice-tts-stt-optimizations):
seven additive `VoiceConfig` keys cross the daemon->child C1 keys line as
omitempty `KeysFrame` selectors (zero/empty = byte-identical legacy), so the
child's turn-taking, STT and TTS adapters receive them per call:
`smart_turn_mode` (smart turn 0/1/2 semantics; default 0 = off, verdict unused;
mode 2 consumes Smart Turn under `turnMu` only, never `playMu`),
`whisper_server_addr` (empty = exec-per-utterance, persistent server when set),
`deepgram_endpoint_ms` (0 = timer only; positive = authoritative endpoint fast
path), `recognition_vocabulary` (Deepgram keyterm + Whisper -p),
`qwen3_chunk_size` (0 => body chunk_size 4), `clip_prewarm_delay_ms` (0 =
immediate prewarm; positive = defer clip renders past first played edge;
renders yield to live synth/barge-in), and `context_warm_window_ms` (0 =>
turn_confirm_ms). Whisper emits finals only, so context warm and the no-partial
limits apply only to partial-streaming STT. Full contract in
`internal/voice/AGENTS.md`. All new keys/selectors absent = byte-identical
legacy request/provider.
