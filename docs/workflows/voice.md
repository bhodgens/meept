---
title: Voice
---

# Voice (phone + local legs)

Status: Implemented (plans docs/plans/20261102-voice-sidecar/ and
docs/plans/20261106-voice-local-leg/ complete; see the as-built sections
of the spec). Authoritative design: docs/workflows/voice-sidecar.md.
Implementation plans: docs/plans/20261102-voice-sidecar/ and
docs/plans/20261106-voice-local-leg/.

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
future transport change, not a GUI gap. Parity note: TUI (`/voice`
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
