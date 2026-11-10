# internal/voice/AGENTS.md

Guidance for AI agents working in `internal/voice/` (and, by contract,
its child-side siblings `internal/voiceframe`, `internal/voicechild`,
`cmd/meept-voice`). Referenced from the root AGENTS.md; the full voice
sidecar contract lives here. Update both in the same commit per the
root maintenance rule. User-facing docs: `docs/workflows/voice-sidecar.md`
(spec rev 3 + as-built), `docs/workflows/voice.md`,
`docs/workflows/voicechild.md`. Plan trees:
`docs/plans/20261102-voice-sidecar/` and
`docs/plans/20261106-voice-local-leg/`.

## Frames (C1): one JSON object per line, every frame carries call_id

- The shared text socket hosts ONE child connection; every pipeline is
  keyed by call_id. Frame JSON keys are BYTE-EXACT (`call_id`, `event`,
  `text`, `last`, `from`, `to`, `direction`, `codec`, `sample_rate`) -
  a wrong key silently breaks the child. The codec lives in
  `internal/voiceframe` (stdlib-only; shared by daemon and child; see
  below).
- `Last` is `*bool`: an omitted `last` (setup, interrupt, end) must stay
  distinguishable from an explicit false. Only final prompts
  (`last:true`) start an employee turn; partials update preview only.
- DTMF frames decode as a counted no-op - a voicemail-tree "press 1"
  must never error the call.
- The child sends setup/prompt/interrupt/end; the daemon sends
  say/clear/end. A `say` frame is sent ONLY after the supervisor allows
  the sentence; filler/greeting lines (fact-free, config list) are the
  sole exception, emitted by the daemon talk loop (greeting on connect,
  filler immediately on each final prompt - order pinned by
  TestTalkLoopGreetingAndFiller).
- The daemon sends ONE extended C1 `keys` line per call (voice-tierA
  leaf 03, internal/daemon/voice_keys.go) through `TextServer.SendRaw`
  right after the talk loop registers on setup: `stt_key`, `tts_key`,
  plus the selectors `stt_vendor`/`stt_model`/`stt_base_url`/
  `tts_vendor`/`tts_model`/`tts_base_url`/`tts_voice` (absent = "" = the
  hardcoded deepgram/openai defaults; a non-empty `stt_base_url`/`tts_base_url`
  is dialed instead of the cloud default - `[voice].stt_base_url` /
  `[voice].tts_base_url` feed them daemon-side, global config by design).
  The vendor/model/voice selectors resolve per call through
  `voice.ResolveProfile` (local-leg CL4: `voice.employee_leg` row >
  per-leg config > provider default) over the call's registry row
  (employee id + leg); an untracked ccid lands on the global values.
  The shared `voiceframe.Frame`
  carries NO key fields by design - `SendRaw` is the keys seam, and the
  child decodes the raw line (KeysFrame String/LogValue redacts). The
  child constructs its real providers from this frame (C3 factories in
  cmd/meept-voice); a pre-Tier-A child that ignores unknown keys fields
  keeps working (D4). A daemon-side `open` frame (local-leg CL1) now
  precedes setup: the child answers open with setup, which arms the talk
  loop and triggers the keys emission.

## Sockets (C2): per-call unix pairs, codec-derived chunks

- `$MEEPT_HOME/voice/sock/<ccid>.in` (daemon->child PCM) and
  `<ccid>.out` (child->daemon PCM); raw frames, no JSON. Chunk size is
  codec-derived, 20 ms: L16@16k = 640 bytes (s16le, NOT big-endian RTP
  L16), PCMU@8k = 160 bytes. The Telnyx start frame
  (`start.media_format`) is the authority for what was negotiated; PCMU
  triggers mu-law transcode both directions.
- Unix socket paths bind only under the kernel sun_path limit (103
  bytes on macOS). Production paths live under `$MEEPT_HOME` and are
  fine; e2e sandboxes shorten the test process TMPDIR for this (leaf
  15). Do not move the socket dir deeper without re-checking the limit.

## Media splice: reorder, pacing, barge-in

- Reorder window (`reorder_window_frames`, default 10) with silence
  fill; frames arriving within `reorder_gap_ms` (default 60) are
  sequenced, not forwarded in arrival order (uptractor deviation).
- Playback paces 20 ms per frame outbound. Child chunks are ALWAYS
  L16@16k = 640 bytes (`ChunkBytes("L16", 16000)`) regardless of the
  negotiated wire codec (audit H2: the wire codec must NEVER size child
  chunks). The wire codec selects only the transcode direction
  (L16 = byte-identical passthrough, PCMU = mu-law 8 kHz up/downsample),
  and the wiring REFUSES a codec/rate mismatch (L16@!=16000, PCMU@!=8000)
  rather than silently mis-resampling (bughunt 2026-11-07 voice F-3).
- Barge-in is PER CALL and per BARGE-IN: `clear` stops playback and drops
  queued drafts for THAT call_id only; other calls' pipelines are untouched
  (pinned by TestBargeInPerCall and the voice-bargein e2e suite). The
  latch is per barge-in, not per call: the child pipeline re-opens playback
  on the next say (playGen retirement kills the interrupted stream), and
  the splice re-arms when fresh child PCM arrives (bughunt 2026-11-07
  voice F-1 — both latches used to be once-per-call, muting the agent
  after its first interrupt). One interrupt latch per barge-in in the
  child pipeline.
- The barge-in TRIGGER is the child's STT-partial transcript branch
  (voice-tierA leaves 05/06, D1): pumpAudioIn feeds the bound STT
  session; a non-empty partial while playback is live emits interrupt
  BEFORE the partial's prompt frame (C5 order), once per barge-in
  (noteBargeIn latch, re-armed by the next say). ONNX/jargo VAD is NOT
  required for Tier A - the ONNX probe in cmd/meept-voice stays
  non-fatal and the "a phone call without barge-in is refused" spec line
  is superseded (see docs/workflows/voice-sidecar.md Tier A as-built).
  The local leg keeps its RMS VAD trigger (Local==true only); STT
  partials drive the phone leg.

## Release fan-out

- A call end fans out through `Manager.fireRelease`: hangup events (and
  the no-answer timer via `FailOutcome(ccid, OutcomeNoAnswer, ...)`)
  release the registry slot, unpark the employee goal, and complete the
  outbound effects reservation exactly once. `FailOutcome` exists so
  no-answer is distinct from hard failure (leaf 05/11); use it for any
  new terminal-not-failure outcome.
- `call.answered` for an untracked id is refused; hangup for an unknown
  id is nil (idempotent teardown). `OnMediaStart` is idempotent.
- Outbound legs are NEVER AnswerCall'd (404 aborts the session);
  `call.answered` triggers StartStreaming only.

## Supervisor gate (C6): fail-open per sentence, never parked

- Every candidate sentence is checked on the fast endpoint
  (`voice.supervisor_model`) with a hard `supervisor_timeout_ms`
  timeout. Timeout/error/quota sends the fallback line and the call
  CONTINUES - a voice call is never quota-parked. Sentence N+1 is
  checked while N plays.
- Nil supervisor chatter (unresolved fast model) degrades the gate to
  allow-all with a logged warning (leaf 14 wiring); that is the only
  sanctioned degrade. Missing telnyx credentials, by contrast, degrade
  the whole voice block to disabled at boot (see secrets below).

## Secrets lanes: token in daemon only, keys to child over the socket

- `Broker.Value("telnyx_api_key")` resolves in-daemon, in-process only
  (allowlisted in scripts/audit-secrets-value-callers.py - add any new
  Value caller in the same commit). The token never reaches a log line,
  env entry, argv, or the child.
- Provider STT/TTS keys reach the child via the C1 `keys` frame on the
  text socket at call start; they live in child MEMORY only (redacting
  String/LogValue in internal/voicechild/keys.go), never disk, env,
  argv, or logs. A missing broker/secret DEGRADES the boot to
  voice-disabled with a loud WARN (scratch boots must still come up);
  every call path stays refused without credentials.

## Child confinement: jargo NEVER leaks daemon-side

- jargo is imported ONLY by `cmd/meept-voice` and
  `internal/voicechild`. `internal/voice` and `internal/voiceframe`
  must not import it - pinned by compile-time import checks
  (internal/voice/import_test.go, internal/voiceframe/pin_test.go).
  Keep those pins green; do not "temporarily" import jargo in daemon
  code. jargo is pinned at v0.1.0 (v0.2.0+ needs go 1.27); API drifts
  are documented in internal/voicechild/jargo.go.
- `internal/voiceframe` is dependency-free (stdlib only): both sides
  share the codec and neither may drag the other's world in through it.
- The child never imports telnyx code and never sees the Telnyx token.
- The one-time start token is an ENFORCED authentication seam:
  ChildProc.spawn() generates the per-spawn crypto/rand token, pipes it
  to the child STDIN (never argv/env), and - with ChildProcConfig.TokenAuth
  wired (internal/daemon/voice_wiring.go NewChildProc sets
  `TokenAuth: textServer`) - arms it on the text server via
  SetExpectedToken BEFORE the process exists; each spawn re-arms a fresh
  token. The child writes that raw stdin token as a newline-terminated
  FIRST line after dialing text.sock, before any C1 frame; the auth line
  is NOT a C1 event (the frame protocol is unchanged). TextServer
  authenticates each would-be child BEFORE claimChild (first line under a
  5s deadline, subtle.ConstantTimeCompare; mismatch/timeout/EOF logs a
  generic WARN - token material never logged - and closes without
  claiming; a later correct dialer can still claim). No token armed =
  legacy first-dial-wins (unit fixtures / hand-built dialers; e2e
  fakechild proves the token like the real binary). Keep any new spawn
  wrapper stdin-compatible and arm the gate on its TextServer.

## One live call per employee (C8)

- The registry enforces one live call per employee (invoke mutex) and
  one live call per target number globally; `max_concurrent_calls`
  caps instances (cap <= 0 means unlimited; DefaultConfig sets 2).
  N concurrent calls need N phone employees. The employee `voice_call`
  tool (internal/tools/builtin/voice_call.go) and the inbound path both
  Acquire through the registry - inbound Acquires
  (ccid, voice.default_employee, from) in the spawn seam (leaf 15 fix);
  an empty default_employee is a named boot error.
- Outbound dials reserve internal/effects keyed (employee_id, target,
  request_id) BEFORE the POST and write the audit row BEFORE DialOut;
  a retry never rings twice.

## Process lifecycle (C-lifecycle, leaf 13)

- Child spawn WRAPS llm.RuntimeProcess (pid files, kill logic, orphan
  sweep reuse); do not hand-roll a second mechanism. Idle stop after
  `idle_stop_seconds`; parent death is covered by the daemon
  supervisor mode; only the startup sweep reaps orphans (mirrors the
  root daemon invariants).
- Child RSS is logged at spawn, first call, and idle stop.

## Local leg (C11, local-leg plan): invariants

The local leg (`internal/voice/local.go`, `internal/voice/localsplice.go`)
is the carrier-free sibling of the phone leg. Two legs exist
CONCURRENTLY - one child, one text server, one registry, one call
store. Invariants a future agent must NOT break:

- Local leg NEVER imports telnyx or touches the broker key: local.go
  imports no telnyx package and carries no carrier seam at all
  (internal/voice/local.go:14-15 - build-pinned, not convention), and
  `Broker.Value("telnyx_api_key")` has no local-leg caller. The leg may
  ride a Manager whose `telnyx.CallController` is nil (local.go:194) -
  never
  give the local leg a live carrier client, and never add a carrier
  call to its teardown.
- Registry leg field semantics: `Instance.Leg` is `"phone"` or
  `"local"`; the ZERO value means "phone" so existing composite
  literals stay valid (internal/voice/dial.go:583-598). Acquire via
  `AcquireLeg`; `NormalizeE164` runs ONLY for leg "phone" - local
  targets look like `local:<label>` and must never be mangled
  (dial.go:657-663). Capacity, one-live-per-employee, and one-live-per-target
  rules are SHARED across legs (dial.go:663-680); never add a leg-scoped
  bypass of them.
- Open-before-setup ordering: the daemon sends the C1 `open` frame
  right after the AudioPair exists, for BOTH legs; the child creates
  its pipeline, dials the pair, and answers with `setup`; the talk
  loop arms on setup and the keys line follows at the Tier A emission
  site (internal/daemon/voice_wiring.go routeFrame EventSetup branch,
  sendKeysFrame call at :604, builder internal/daemon/voice_keys.go:80;
  local producer internal/voice/local.go:368 sendOpenFrame call,
  builder :437). Do not send keys or say frames before
  setup arrives, and do not make open failure fatal (warn-and-continue
  on stub audio is the pinned posture, local.go:368-370).
- RMS VAD only when Local==true: the local leg's barge-in trigger is
  the INCOMING-audio RMS window - pumpAudioIn taps caller-mic PCM from
  the .in socket while playback is live; 10 sustained chunk-lengths
  (200 ms at L16/16k) of over-threshold energy (`voice.local.barge_in_rms`,
  rmsBargeInCheck in internal/voicechild/pipeline.go) that CLEAR the
  echo guard trip the latch. The outbound .out stream is only the echo
  REFERENCE (playbackStream records each written chunk's energy): a trip
  requires window-average energy > 130% of the last outbound chunk's
  average, so speaker-through-mic echo never self-trips and the agent's
  own loud TTS never interrupts itself (feeding the VAD outgoing PCM was
  a pinned bug, fixed 615808e3). The trip shares the noteBargeIn latch
  (one interrupt per barge-in, F-1 re-arm by the next say) and the
  pendingInterrupt handshake keeps the C5 interrupt-before-prompt order
  even though the RMS trip fires on the audio-pump goroutine; mic PCM
  keeps flowing to STT during playback so the employee reforms from what
  the caller said. The trip's STT EVIDENCE is ONE-SHOT: a trip zeroes
  lastFinalAt, so a second window needs a NEW final transcript (stale
  evidence cancelling the very turn its own final dispatched was a live
  defect, call local-2d84c384fd93f447 2026-11-09, leaf 02); non-speech
  finals ([BLANK_AUDIO]/(sighs) class, nonSpeechTranscript) never dispatch
  a turn and never stamp evidence. RMS must NEVER run on phone legs -
  they keep the Tier A STT-partial trigger. Never move the trigger to a
  leg-agnostic branch.
- Barge-in must SILENCE the client speakers, not just stop new frames:
  internal/tts.AudioPlayer tracks ALL live oto bursts and Stop() pauses
  every one of them (Stop was a documented no-op - cancelled audio played
  under the next answer, leaf 01); SpeakerSink.Clear drops the buffered
  burst too; the CLI playout caps pending socket audio (~150 ms,
  liveMaxPendingAudioBytes) so kernel backpressure bounds the tail.
  Play NEVER pauses predecessors: bursts overlap in oto's mixer (the
  leaf-01 per-handoff pause chopped paced real-time audio at every ~150 ms
  boundary - the same-day chop regression; cancel-time pause only is the
  contract, pinned by player_recall_test.go).
- LocalSplice has NO reorder and NO transcode by contract:
  unix sockets preserve order (no reorder window), and the local leg
  negotiates L16 s16le@16000 only - any other chunk size is the named
  ErrL16Only refusal (internal/voice/localsplice.go:58-60, named at
  :172).
  Outbound stays paced at one 640 B chunk per 20 ms (the uptractor
  rule; localsplice.go:14-16). Do not "harden" it with a reorder buffer.
- Deliberate policy differences, pinned not accidental: the local leg
  skips the effects reservation and the dial audit row (no paid side
  effect, no external party; internal/voice/local.go:11-14 mirror
  comment) and writes no carrier POST. Do not "restore symmetry" -
  add the leg's invariants, not the phone leg's policy.
- Teardown parity: `LocalLeg.End` fans out through
  `Manager.FailOutcome` exactly like a phone hangup (release hook,
  registry release, binding end; local.go:484-510, FailOutcome at
  :510). Double End is
  idempotent nil; a foreign (non-`local-`-shaped) id is
  ErrUnknownLocalCall. Keep the prefix shape test honest - it is what
  distinguishes those two.
- Goal-hold feeding: the call row's ended-state transition rides
  `ReleaseHook -> heldCalls`. `AcquireCallHold` is registered once per
  live call, and released exactly once by `ReleaseHook -> ReleaseForCall`
  (the map delete is the once-guard; do not also call the returned
  release func). Production callers:
  - local: `LocalLeg.Open` -> `LocalDeps.Hold` (wired in
    `internal/daemon/voice_wiring.go`). Nil Hold degrades to no
    ended-state. Do not change this contract; do not add a second
    OnCallStart on the local path.
  - phone inbound: the spawn seam in `voice_wiring.go` calls
    `holdPhoneCall` after registry Acquire (the closure reads the
    employee pointer filled after `WireVoiceEmployee`).
  - phone outbound: `Dialer.holdCall` after a successful Acquire, via
    `DialConfig.Hold` (nil degrades to no ended-state). Refusals and
    the idempotent retry path do not hold.
    `holdPhoneCall` also runs `Binding.OnCallStart` — the phone leg had
    no other call site, and `OnCallEnd` is a no-op without a live
    binding entry, so the row would never reach 'ended' on the hold
    alone. OnCallStart takes the fresh-session-per-call flag as a
    trailing variadic (voice.fresh_session_per_call / employee_leg
    fresh_session, resolved via voice.FreshSessionFor at the wiring
    layer) - when true the call gets a NEW session named "voice <emp>
    <ccid>" instead of the shared (employee, number) one; the hold/
    release flow is unchanged (keys off ccid).
  Keep both Hold seams optional.
- RPC stays seam-based: `internal/rpc/voice_local.go` takes
  `VoiceLocalSeams` func fields and NEVER imports internal/voice (the
  daemon adapts in internal/daemon/voice_local_rpc.go). No audio ever
  rides RPC - clients dial the C2 pair paths directly (internal/rpc/
  voice_local.go:23-24).

