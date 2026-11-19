---
title: Voice
---

# Voice (phone + local legs)

Status: Implemented (plans docs/plans/20261102-voice-sidecar/ and
docs/plans/20261106-voice-local-leg/ complete; see the as-built sections
of the spec; plan docs/plans/20261113-filler-clips-and-context-warm/
adds the filler clip cache + lane pools + farewell rule and the
opt-in partial context warm). Authoritative design:
docs/workflows/voice-sidecar.md. Implementation plans:
docs/plans/20261102-voice-sidecar/,
docs/plans/20261106-voice-local-leg/, and
docs/plans/20261113-filler-clips-and-context-warm/.

## Overview

Voice lets meept employees talk in real time. Two legs exist
CONCURRENTLY - they are alternatives per call, not per deployment:

- The PHONE leg (Telnyx): an employee answers inbound calls and dials
  out, talks through a separate jargo voice child (ears, turn-taking,
  barge-in, mouth), and speaks only sentences a supervisor employee
  allows. Multiple phone instances run at once to different numbers.
- The LOCAL leg (no carrier, no cloud): an employee holds a voice
  conversation over local client audio - your mic and speaker (CLI now).
  Local STT/TTS run as subprocesses (whisper.cpp, piper); nothing leaves
  the machine unless you configure cloud providers for the leg.

Both legs share one child, one talk-loop machinery, one supervisor gate,
one registry, and one call store. Different employees can be on
different legs at the same time.

## Problem

Push channels are one-way text. A voice call is realtime audio with
interrupt, and the caller expects the assistant to know the day's
context. Neither exists in the text chat path. The local leg adds a
zero-cost path: test the whole voice stack and talk to employees
without a Telnyx account, cloud STT/TTS keys, or a phone number.

## Which leg answers a call

The calling agent picks the leg per call through the `voice_call` tool's
`leg` argument; without the argument the chain falls to the employee's
configured default (`voice.employee_leg`) and then `voice.default_leg`
(else phone). The optional `voice` argument overrides the TTS voice for
that one call.

## Conversation memory across calls

By default every call to the same employee and target (the local label
defaults to your hostname) shares ONE session: each call's turns append
to that stored conversation, so the employee remembers - and replays -
prior calls. When that history snowball becomes noise, set
`voice.fresh_session_per_call = true` (opt-in, default false): every
call opens its own session and starts clean. The day brief ("prior
calls today", one line per call) remains the only cross-call context
either way, and every call's full transcript still lands in the call
store plus an `employee.notify` relay. Per-employee policy: an
`employee_leg` row's `fresh_session` key overrides the global flag
(`{employee: "companion", fresh_session: false}` keeps continuity for
one employee while the rest start fresh). Until the flag, a quick
manual reset is a distinct `--label` per call.

## Configuration options

All under `voice` in meept.json5. `enabled` defaults false. See
docs/workflows/voice-sidecar.md Config section for the full key list,
and its "Local leg (C11)" section for the local keys:

- `default_leg` - `phone` (default) or `local`.
- `employee_leg` - per-employee `{employee, leg, tts_voice, tts_model}`
  defaults, so different employees default to different legs/voices.
- `local.enabled` - the local leg master switch (opt-in; default false).
- `local.stt_provider` / `local.tts_provider` - local vendors; empty
  falls back to the global `voice.stt_provider` / `voice.tts_provider`.
- `local.tts_model` / `local.tts_voice` - local leg TTS selection.
- `local.barge_in_rms` - the local leg's RMS barge-in threshold
  (s16 level, default 3000). Local leg only.
- `local.name` - session label; empty = your hostname.

Local providers are subprocesses, not libraries: the daemon needs the
`whisper-cli` (whisper.cpp) and `piper` binaries on PATH. Both are
keyless - no secrets-broker entry is required for the local leg.

## TTS wire contract (qwen3 token-level streaming)

The qwen3 TTS vendor (`tts_vendor: "qwen3"`) speaks the local server's
**token-level** route, `POST <tts_base_url>/v1/audio/speech/pcm-stream`,
not the OpenAI-shaped `/v1/audio/speech` route. The latter ignores
`stream=true` and buffers the whole utterance before its response body
begins (~1.2 s first byte); the pcm-stream route emits progressive
codec-frame PCM (measured header TTFB ~7 ms, first audio ~0.37 s on a
tier-1 faster-qwen3-tts server). Same server, same model, same cloned
voice id (`vc_ce7d47c6`), so voice-clone identity is unchanged.

- Request body (route's own schema): `{"text": "...", "voice": "...",
  "chunk_size": 4}`. There is NO `stream`, `response_format`, `input`,
  or `model` field; an empty `tts_model` means "use the server's loaded
  default". The `chunk_size` value is 4 by default and is overridable
  with `voice.qwen3_chunk_size` (opt-in; 0 = legacy 4). Cloud `openai`
  is untouched and still posts `stream=true, response_format=pcm` to
  `/v1/audio/speech`.
- Response body is a length-prefixed frame stream:
  `[4-byte big-endian uint32 length][length bytes s16le PCM] ...`, with a
  zero-length frame marking the end. The child strips the envelope
  incrementally (`internal/voicechild/providers/pcmstream.go`) before
  the shared 24 kHz -> 16 kHz resampler, so first audio is not gated on
  buffering a whole frame or the whole utterance.
- A barge-in `clear()` cancels the request ctx; the route aborts cleanly
  on client disconnect, so the existing per-barge-in cancel semantics
  are preserved. Connection hygiene (one fresh POST per utterance, no
  keep-alives, one narrow retry) is unchanged.

## Per-employee voice controls

Every voice.employee_leg row can override the provider selection AND
every acoustic/noise knob for ONE employee's calls (voice-peragent
leaf 01). Precedence is fixed: **row value > global `voice.*` > shipped
default**, and every per-row key is tri-state - ABSENT means inherit,
an explicit `0` / `false` / `""` means a real override. A row that
carries no acoustic keys reproduces today's global behavior
byte-for-byte: the pin, not the hope (see
`internal/voice/AGENTS.md`). Resolution lives in
`voice.ResolveProfile` / `voice.ResolveAcoustics`
(`internal/voice/profile.go`); the values reach the child over the
per-call C1 keys frame (gate knobs, `internal/daemon/voice_keys.go`)
and the C1 open frame (`barge_in_rms`), and feed the daemon talk loop
directly at construction (`internal/daemon/voice_wiring.go`).

| key | type | global default | per-row? | meaning + tuning hint |
|-----|------|----------------|----------|-----------------------|
| `tts_voice` | string | `""` (vendor default) | yes | voice id; row beats per-leg config |
| `tts_model` | string | `""` | yes | TTS model for this employee's calls |
| `fresh_session` | bool | `voice.fresh_session_per_call` = false | yes | new session per call; raise isolation, loses cross-call memory |
| `guided_intake` | bool | off (no global) | yes | one-question-at-a-time intake procedure |
| `backchannel_soften` | bool | false (#130 A/B) | yes | soften short-backchannel flush vs the peer tail. Measured cost 2026-11-12: ON cuts talk-over 4.96 s -> 2.54 s per 90 s link, but turn pace drops (extra tail pad + drained-edge waits) - the operator chose responsiveness and kept false; enable only if double-talk texture bothers you more than slower turn-taking |
| `turn_confirm_ms` | int | 900 | yes | child-side breath-pause aggregation window; lower = faster turns but more fragments; 0 = legacy per-final dispatch |
| `min_turn_words` | int | 0 (gate off) | yes | short-final gate: drop finals with < N stripped words; set 2-3 in loud rooms; explicit 0 turns the gate off for this row even when the global gate runs |
| `exempt_phrases` | []string | stop, cancel, yes, no, thanks, thank you, okay, ok, mm-hm, mmhm, uh-huh, go ahead | yes | phrases the gate never drops (whole-residue, case-insensitive); extend for a domain's voice commands; empty list = no exemptions |
| `energy_gate` | bool | false | yes | loudness-aware gate: drop a short final ONLY when the utterance window's average mic RMS is below `min_rms`; turn ON when the word gate eats things you actually said |
| `min_rms` | int | 0 (unused while off) | yes | energy floor, s16 RMS units (same scale as `barge_in_rms`); raise when TV audio passes as "loud", lower when your quiet speech drops |
| `fragment_decay_s` | int | 0 (off) | yes | after K consecutive drops within S seconds the effective N rises by min(K,3), resetting on any dispatch; set 30-60 for bursty chatter rooms |
| `reply_exempt_s` | int | 15 | yes | short final within S seconds of the agent's OWN last spoken audio is a reply, exempted; 0 disables; lower if immediate reactions flood turns |
| `filler_grace_ms` | int | 2500 | yes | wait before speaking filler after a prompt; 0 = immediate (legacy); raise for a fast employee, lower for a slow one |
| `silence_prompt` | string | "Are you still there?" | yes (non-empty wins) | dedicated dead-air nudge line for this employee; superseded for this row by a non-empty `silence_prompt_lines` |
| `silence_prompt_lines` | []string | `voice.silence_prompt_lines` (empty) | yes (non-empty wins) | C5 lane pool: dead-air nudge lines, round-robin per fire; empty inherits the scalar layers (ladder: row list > row scalar > global list > global scalar) |
| `fragment_clarify_lines` | []string | `voice.fragment_clarify_lines` (empty) | yes | C5 lane pool: "you trailed off?" lines, round-robin per fire; a non-empty list beats `voice.fragment_clarify` |
| `farewell` | []string | `voice.farewell` (default "Goodbye!", "Bye!", "Talk soon.") | yes | C5 farewell pool: goodbye lines spoken when the caller says bye (see "Filler audio: pre-rendered clips, lane pools, and the farewell rule") |
| `silence_prompt_seconds` | int | 45 | yes | S: nudge fires after this much caller silence; lower (10) for snappy re-engagement |
| `silence_hangup_after_seconds` | int | 45 | yes | H: quiet time AFTER the nudge before hangup (nudge@S, hangup@S+H); 0 reuses S |
| `barge_in_rms` | int | `voice.local.barge_in_rms` = 3000 | yes (local open frame) | RMS VAD threshold; raise if TV trips the barge-in, lower if you must shout to interrupt |

Row keys are the same names on the row object (acoustic overrides ride
as pointers child-side; see the template comment at
`config/meept.json5` `voice.employee_leg`). Telemetry: the child logs
`voicechild: short final dropped` with `reason=wordcount|energy` per
drop and `voicechild: short final exempted` with `reason=phrase|reply|energy`
whenever an improvement saved a would-be drop - the operator tunes
`min_turn_words` up/down straight from the log.

## Latency and endpointing tuning knobs (opt-in, plan 20261118-voice-tts-stt-optimizations)

All keys below live under `voice` in meept.json5. Every knob is additive
and OPT-IN: leaving it unset reproduces pre-change behavior byte-identically.
All seven cross the C1 keys frame to the child as omitempty selectors (zero
or empty = legacy). They are global-only (no `employee_leg` per-row override).

| key | type | default | effect |
|-----|------|---------|--------|
| `smart_turn_mode` | int | 0 | semantic endpointing mode: 0 = off (legacy, analyzer installed and fed but verdict unused); 1 = kill switch (no analyzer built or fed, VAD stays intact); 2 = consume `AnalyzeEndOfTurn` at confirm-window expiry (complete=true dispatch now, complete=false one bounded re-arm). Mode 2 needs paired hesitation fixtures before production; default stays 0 |
| `whisper_server_addr` | string | `` (empty string) | empty = exec `whisper-cli` per utterance (legacy, ~350 ms cold start); non-empty = persistent whisper.cpp server transport with the same segmentation and finals-only behavior. The Whisper leg emits finals only, never partials |
| `deepgram_endpoint_ms` | int | 0 | 0 = endpoint ignored, fixed `turn_confirm_ms` timer only (legacy); positive = a Deepgram `speech_final` / `UtteranceEnd` dispatches accumulated finals immediately. Needs paired hesitation/backchannel fixtures before production; default stays 0 |
| `recognition_vocabulary` | []string | `[]` | empty = provider wire requests byte-identical; non-empty = forwarded as Deepgram `keyterm` params and a Whisper `-p` prompt to bias names, addresses, and identifiers. Keep the list short; needs a paired critical-entity fixture |
| `qwen3_chunk_size` | int | 0 | 0 = request body `chunk_size: 4` (legacy); positive = override. Local measurements at 1/2 were noisy; default stays 4. A/B first-audio on the target server before changing |
| `clip_prewarm_delay_ms` | int | 0 | 0 = render clip fills immediately at call setup (legacy); positive = defer `clip_render` frames until the child's first `played` edge plus this delay, moving the render burst off the first-audio path. A/B on the target box; benefit unmeasured |
| `context_warm_window_ms` | int | 0 | 0 = use the resolved `turn_confirm_ms` (legacy); positive = override the context-warm debounce window so the warm lands before confirm-window dispatch. See "Partial context warm" |

Notes:
- `smart_turn_mode` and `deepgram_endpoint_ms` both change WHEN a turn dispatches. Keep paired fixtures (a hesitation/backchannel recording and its clean counterpart) and compare before enabling either in production; both are opt-in with default 0.
- `recognition_vocabulary` needs a paired critical-entity fixture (the same utterance with and without the terms) to confirm accuracy improves on your names without over-biasing. An over-long list logs a warning.
- `qwen3_chunk_size` and `clip_prewarm_delay_ms` are latency knobs: A/B first-audio timing on the target server/box; neither default changes current behavior.
- Whisper emits FINALS ONLY (no STT partials), so `context_warm_window_ms` and the whole context-warm feature affect only providers that stream partials (the phone leg with Deepgram). On the Whisper/local leg the warm path has no partials to debounce and stays inert - this limit is documented, not fixed.

## Filler audio: pre-rendered clips, lane pools, and the farewell rule

Every short "holding" line the call speaks - the greeting on connect,
the working-turn filler ("Hold on, let me check."), the dead-air nudge,
the fragment-clarify, and the NEW farewell - is FACT-FREE and comes
from a finite configured pool. That makes them cacheable, and phase 1
of plan docs/plans/20261113-filler-clips-and-context-warm/ uses it:

- **Lane pools.** Each filler-class line belongs to exactly one lane
  (`greeting` / `filler` / `silence` / `fragment` / `farewell`). Pools
  round-robin per lane with their own cursor, so multi-line pools are
  fully honored. `voice.filler` and `voice.greeting` keep their shapes;
  the scalar `voice.silence_prompt` / `voice.fragment_clarify` gain
  list overrides `voice.silence_prompt_lines` /
  `voice.fragment_clarify_lines` (a non-empty list wins; the ladder is
  employee_leg row list > row scalar > global list > global scalar >
  default, and a list counts as "set" only when non-empty, so unset
  new keys behave exactly as before). The farewell pool
  `voice.farewell` is new and ships non-empty ("Goodbye!", "Bye!",
  "Talk soon."); per-employee override `employee_leg[].farewell`.
- **The farewell rule.** When the caller's finished utterance IS a bye
  phrase (whole-utterance match against a small builtin set - bye,
  goodbye, see ya, see you, talk later, later, that's all - never a
  phrase buried in a longer sentence), the employee speaks one
  farewell-lane line, cancels any in-flight draft (no LLM turn), and
  ends the call with reason `caller farewell`. The goodbye rides the
  normal final-say drain, so its audio completes before teardown.
  Exactly one goodbye per call; a repeat "bye" is ignored. The pool
  ships non-empty and an empty list INHERITS the shipped default, so
  there is no config-level off switch by emptying (override the pool
  with the lines you want instead); a talk loop built without any
  resolvable farewell line (programmatic callers with empty Lanes and
  FarewellLines) never fires the rule and "bye" rides the normal turn
  path byte-identically.
- **Clip cache.** At call setup (and again after a config reload) the
  daemon diffs the resolved pools against a content-addressed clip
  store and asks the voice child to render only the MISSING lines, in
  the background, never blocking the conversation. Rendered clips land
  in `$MEEPT_HOME/voice_clips/` keyed by sha256 over
  (text, tts_vendor, tts_model, tts_voice, clone-reference-audio hash):
  change the voice, model, a pool line, or the `voice.tts_ref_audio`
  reference file, and the affected clips simply regenerate. An empty
  `tts_model` is a valid identity: it is the qwen3 "use the server's
  loaded default" sentinel, so prewarm works with the normal qwen3
  config (the key carries that same empty model string the keys frame
  sends and the child synthesizes with — never a fabricated default).
  When the talk loop speaks a pool line its say frame names the clip; the
  child plays the cached PCM through the exact same pacing/barge-in lane as
  synthesized audio (zero vendor call), and any miss falls back to
  live synthesis. Net effect: filler lines that used to cost 0.7-4.8 s
  of synthesis now start instantly, and they always play in ONE
  timbre - the drifting bug was short live-synth utterances. Nothing is
  generated or written when the voice sidecar is not configured and
  running (`voice.enabled=false` = the store dir is never even
  created), and clip audio never carries secrets: the store holds PCM
  + a small JSON manifest per line. By default the render burst fires
  at call setup; `voice.clip_prewarm_delay_ms` (opt-in, default 0) can
  defer `clip_render` frames until the child's first `played` edge plus
  the configured delay, moving setup-time synthesis off the first-audio
  path. A background render also yields to live synthesis and barge-in
  (it is idempotent and re-requested, so deferral loses no clip).

## Partial context warm (opt-in)

`voice.context_warm = true` (default FALSE) spends the STT PARTIALS a
live call already streams: while you are still talking, a debounced
max_tokens=1 throwaway request plants the imminent turn's exact prompt
prefix (system + history + the partial text) into the local model's
prompt cache under the same session-affinity scope the turn will use.
When the final transcript dispatches the real turn, it pays decode
time, not full prefill - measurably lower first-token latency on long
conversations. The warm is strictly invisible: it never starts an
employee turn or tools, consumes no turn budget, logs nothing above
debug, and its failures are silently ignored (a failed warm is simply
the old latency). `voice.context_warm_min_tokens` (default 60) skips
warming trivial prefixes. The debounce window rides the global
`voice.turn_confirm_ms` (no per-employee override; 0/legacy mode =
fixed 900 ms) unless `voice.context_warm_window_ms` (opt-in, default 0)
is set to a positive value, which overrides the window so the warm
lands before confirm-window dispatch. Whisper emits finals only and
NO partials, so on the Whisper/local leg the warm path has nothing to
debounce and stays inert - an honest documented limit, not a bug.
A final transcript or hangup cancels any pending or in-flight warm.
The payoff is the llama.cpp-family local endpoints (prompt_cache_key +
affinity header are the existing session-affinity plumbing); the
Anthropic and Codex clients implement Warm as an explicit no-op.
Disabled = byte-identical.

## The default voice employee

The repo ships a default voice employee:
[`config/employees/secretary.json5`](../../config/employees/secretary.json5) -
a personal-assistant/small-business receptionist. It answers inbound calls
(phone leg via `voice.default_employee`) and desk conversations (local leg),
takes messages, and relays notes to the operator as notifications. It never
makes outbound calls: `voice_call` is forbidden by its constitution and
omitted from its tools.

The template ships DISABLED. To hire it: run
`meept agents create config/employees/secretary.json5`, set
`voice.default_employee = "secretary"`, and flip its top-level `enabled`
to `true`.

## The companion employee (desk conversation, read-only reporting)

[`config/employees/companion.json5`](../../config/employees/companion.json5)
is the desk companion for the local leg: a warm, spoken-word conversationalist
that opens with a "how can I help?" offer and can truthfully report on the
operator's recent work and on what the platform is running. It holds ONLY
read-only reporting tools (memory_search, memory_recall, memory_get_context,
platform_status, platform_agents, task_list). Its
constitution's `tools_forbidden` is an explicit mutating-tool deny list, not
the `["*"]` wildcard: enforcement checks the forbid list BEFORE the allow
list, so a wildcard would deny the reporting tools too. The template
ships DISABLED; hire with `meept agents create` after flipping `enabled`.

## The local leg: how to run it

The local caller is trusted by construction: you are the operator at
this machine, there is no carrier and no phone number to resolve. Local
turns are stamped as an approved, known caller, so the voice-action gate
(`voice.unapproved_caller_allowed_tools`) does NOT restrict them — a local
call has full tool use. That config list governs only unapproved *phone*
(and signal) callers, whose numbers are looked up in contacts.db. There
is no config to grant local callers more tools because they already have
all of them.

Prereqs: daemon running with `voice.enabled = true` and
`voice.local.enabled = true`; for live mode, `ffmpeg` on PATH (mic
capture) plus the oto-backed speaker (built in); for file mode, only
the daemon.

File mode (deterministic, mic-free - CI and demos):

```sh
meept voice local --list                      # employees you can call
meept voice local --employee eve --in q.wav --out a.wav
```

The call reads mic input from the WAV file and writes the employee's
reply to the output WAV (raw s16le 16 kHz mono under the hood; the
files are canonical 44-byte-header WAVs). The call ends when the input
is drained and the reply stream goes quiet.

Live mode (real mic + speaker):

```sh
meept voice local --employee eve --label desk
```

Your mic is captured through ffmpeg (avfoundation, raw s16le 16 kHz
mono) and replies play through the speaker. Talk over the employee to
barge in - the local leg detects your speech by RMS level
(`local.barge_in_rms`). The barge-in SILENCES the speaker: the queued
tail is dropped and the burst already playing is paused within ~20 ms
(recallable playback), and live mode keeps at most ~150 ms of
not-yet-played audio ahead of the speakers, so the old answer never
talks over the new one. Ctrl-C ends the call cleanly (`voice.local.end`
fires before exit, so the registry slot and call row are released
exactly once). The first live capture on macOS triggers the standard
system mic-permission prompt for the terminal app; grant it once.

GOOS support:

| OS | Live mic + speaker | File mode |
| --- | --- | --- |
| darwin (macOS) | yes (ffmpeg avfoundation) | yes |
| linux | no - named error pointing at --in | yes |
| other GOOS | no - named error pointing at --in | yes |

Status and both-legs concurrency:

```sh
meept voice status        # live calls for BOTH legs, one table
```

A phone call and a local call can run at the same time (different
employees; the registry's one-live-per-employee and capacity rules are
shared across legs). Agents mix legs freely: the same `voice_call` tool
places a phone call with `leg="phone"` (default) and opens a local
session with `leg="local"` - on the local leg `target` is an optional
session label, not a phone number, and no carrier or allowlist applies.

## GUI (Flutter web)

The GUI ships the same control plane as a page, not a new transport:
`ui/flutter_ui/lib/features/voice/voice_page.dart` drives
`voice.local.open` / `voice.local.end` / `voice.local.status` over the
daemon's HTTP RPC-call bridge (`POST /api/v1/bus/call`, through
`lib/services/voice_service.dart`). Reach it from the hamburger tools
menu ("voice"), the command palette ("voice", key `p`), or the route
`/tools/voice` (`goToolVoice`). The page has an employee picker (from
the employee roster), a start button (open), a 2s live status poll
(both legs), and an end button per call. Every daemon error envelope
surfaces verbatim - the named refusals (disabled leg, unknown employee,
busy, duplicate) appear exactly as the RPC plane words them. Browser
mic capture/playback (`lib/services/voice_audio.dart`) provides the
capture/playback lifecycle via getUserMedia/MediaRecorder and the
WebAudio API; a browser tab cannot dial the local leg's unix socket
pair (no audio over RPC, CL8), so audio streaming to the daemon is a
future transport change, not a GUI gap. Each live call also streams
telemetry: a bounded per-call poller (500 ms, the CLI cadence) reads
`voice.local.events` through `VoiceService.events()` and renders the
feed under the call's row — heard / thinking / replied / held /
released / dropped / barge-in / call ended, barge-in rows highlighted.
Parity note: TUI (`/voice`
slash command), CLI (`meept voice local`), and this GUI page all drive
the same `voice.local.*` RPC methods - leaf 13a + 13b.

Key components: internal/voice/local.go (leg lifecycle),
internal/voice/localsplice.go (media path),
internal/rpc/voice_local.go (voice.local.* RPC),
cmd/meept/voice.go (CLI), internal/voicechild/providers/
(whisper + piper adapters).

## Observability

voice.call.started / progress / ended bus events (WS class event) fire
for both legs. Child RSS logged at spawn, first call, idle stop. Call
rows and transcripts land in $MEEPT_HOME/voice/calls.db with direction
"local" for local calls and "incoming"/"outgoing" for phone calls. The
ended-state transition fires on every leg that registered
`AcquireCallHold` (local Hold seam, phone inbound spawn, phone outbound
`DialConfig.Hold`); release is once, through ReleaseHook.

Operator telemetry (voice-events tree): every call carries a small
fixed-size event ring inside the daemon (internal/voice/events.go,
64 events per call, rings for the 32 most recent calls). The talk loop
appends dispatch/final/barge-in/ended (and the link park lifecycle) at
its existing log sites through the OPTIONAL TalkDeps.OnEvent hook — nil
on any wiring that has not armed it, byte-identical loop either way —
and the frame router appends a `heard` event when a FINAL prompt frame
arrives (mic-speech evidence preceding the dispatch). The ring is read
back over `voice.local.events` (params {call_id, cursor, limit?},
seam-based like every other voice.local method — no new bus topic) and
`meept voice link` / `meept voice local` poll it every 500 ms printing
per-side lines: `heard:` / `thinking (N words)` / `replied (N words,
Xms)` / `>> barge-in` / `call ended: <reason>` / `silence deferred (...)`.

Per-phase events (voice/telemetry-phases): three ADDITIVE event kinds give
the operator precise timing and text for each voice phase, on the same
per-call ring and `voice.local.events` RPC as the lifecycle events (no new
bus topic, no wire change to the RPC envelope):

| kind | phase | text | `ms` |
| --- | --- | --- | --- |
| `stt_final` | caller speech → turn dispatch | FULL final transcript (not the `prompt_dispatch` preview) | speech-end (FINAL prompt frame arrival) → dispatch |
| `model_final` | employee model → turn final | FULL reply text | dispatch → turn-final (same value as `turn_final`) |
| `voice_play` | first spoken model output handed to the child | FULL spoken sentence (the fallback line when the check failed) | dispatch → say-send (NOT audible playback) |

All three are emitted only when the talk loop's optional `OnEvent` seam is
armed (nil = zero work, byte-identical loop), ride the same 64-per-call ring,
and are **in addition to** — never replacements for — `prompt_dispatch`,
`turn_final`, and `said`. `voice_play` fires once per turn (the first allowed
sentence, or the fallback), never for fact-free greeting/filler/fragment/
farewell lines, which have no employee turn. Full text means the operator
feed no longer has to reconstruct a reply from 40-rune previews. A new client
sees unknown kinds via its existing default renderer; old clients continue to
render the lifecycle kinds unchanged.

The CLI renders these per-phase kinds with their full text and `ms` values.

Spoken sentences print as `<send-time> said: <full text>` — the stamp is
the event's **say-send** time (when the talk loop handed the sentence to
the child, per `internal/voice/events.go`), NOT when the speaker played it.
The live speaker window prints `<time> speaker on (played)` at the first
played burst and `<time> speaker off (played, <elapsed>)` after a 350 ms
gap / clear / EOF, so the operator can tell send from actual playback (the
fix for the bare-`said` regression: a `said` line is never contentless —
an old daemon's empty row renders `said: (no text)` rather than a bare
`said`). Each completed reply also gets one compact per-turn timeline:
`turn: heard <t> -> thinking <t> -> replied <t> -> played <t> (model
<ms>ms, <N> words)`, joining STT, the model reply, and real audio playback
on one line. Queued `said` sentences keep their own send stamp when the
speaker window joins them (they are not re-stamped to playback time). The
Flutter GUI reads the same ring for parity (bughunt C-12):
`VoiceService.events()` over the same RPC, one bounded 500 ms poller
per live call, rendering the same operator lines under each call's row
in `/tools/voice`. Best-effort by
contract: the poller never fails a run, and a final synchronous drain
after close prints the tail events (rings outlive the call). A
found=false answer at cursor > 0 ends that ccid's poller (eviction /
teardown); found=false at cursor 0 is the ring's lazy-creation race on a
fresh call and keeps ticking (it once silenced the whole live feed —
call local-53b347f028f0a43f). `meept voice local` additionally reads the
call's stored transcript over `voice.local.transcript`
(params {call_id}; the call store, call-scoped) at call end and prints
every role-tagged line with a `HH:MM:SS→HH:MM:SS user|assistant: <text>`
start→stop window so the operator can make precise observational
references against the recording: each line's stop is the NEXT line's
start (the closest storage-backed bound — no per-line end timestamp is
stored), and the FINAL line is bounded by the call's `ended_at` when the
daemon provides it (an old daemon / live call renders that line
start-only rather than fabricating a stop). The read retries briefly to
absorb the end/flush race, then degrades honestly: an old daemon or
unwired store prints "(no transcript lines)" and never changes the run's
exit.

## Edge Cases

- No-answer outbound releases the plan slot within timeout_secs.
- Barge-in cancels only the interrupted call's drafts (both legs).
- Missing Telnyx public key fails closed: no phone call is answered.
  Barge-in rides STT partial transcripts on the phone leg (no ONNX
  requirement; see the Tier A as-built notes in
  docs/workflows/voice-sidecar.md); the local leg uses RMS VAD.
- Untracked call IDs are refused; hangups are idempotent (including
  double `meept voice local` teardown).
- The local leg refuses without whisper-cli/piper for its configured
  vendors, and live mic capture refuses on non-darwin GOOS with a named
  error pointing at file mode.
- Local targets are never E.164-normalized; a duplicate local session
  label while a call to the same target is live refuses with a named
  duplicate error.
