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
- The child sends setup/prompt/interrupt/end/played; the daemon sends
  say/clear/end. `played` (EventPlayed) is the child's end-of-turn AUDIO
  marker: exactly one frame `{"call_id":...,"event":"played","last":true}`
  after the FINAL byte of a `last:true` say stream is written to the
  .out socket, never for a non-final stream, a barge-in-aborted stream,
  or a failed synthesis. The daemon's drain watch arms from this edge
  (see the release-path bullet below) — it is the true end-of-turn
  signal the blind turn_final TEXT arm could not be. A `say` frame is
  sent ONLY after the supervisor allows
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
  triggers mu-law transcode both directions. The client pair namespace
  (`<ccid>.c.in` / `<ccid>.c.out`, local-leg client transport) is NEVER
  swept by ChildProc.spawn's stale sweep - only child-pair files are
  (a respawn concurrent with another live call must not delete the
  client transport; bughunt 2026-11-10 A9). Daemon->child TEXT writes
  (TextServer.Send/SendRaw) are bounded by a 2 s write deadline, the
  child's own textWriteTimeout posture mirrored: a wedged child conn
  degrades to a write error, never blocks the talk loop or releaseCall's
  end-frame send (bughunt 2026-11-10 A4; the end-frame send must NEVER
  block teardown).
- Unix socket paths bind only under the kernel sun_path limit (103
  bytes on macOS). Production paths live under `$MEEPT_HOME` and are
  fine; e2e sandboxes shorten the test process TMPDIR for this (leaf
  15). Do not move the socket dir deeper without re-checking the limit.
- The voice LINK monitor tap (link.go LinkOpenOptions.Monitor) creates
  two extra unix listeners beside the pair sockets
  (`<dir>/<link id>.mon-a` / `.mon-b`) and tees each pump direction
  NON-BLOCKING: no attached client, a lagging client, or a dead tap
  conn only ever DROPS tap bytes (250 ms write deadline, latest-batch-
  wins channel) - monitor traffic NEVER stalls or gates the bridge.
  The derived path is length-checked at create against a 100-byte cap
  (ErrLinkMonitorPathTooLong) since link ids ride to the sun_path edge.

## Voice link: half-duplex turn-taking (turn-taking leaf)

- A voice link carries TWO independent talk loops; a live run proved
  they greet at once and dispatch over each other's speech. The
  contract the coordinator enforces: at most ONE side holds the floor
  (mid-turn OR draining its paced audio tail) at a time, the
  listener's final prompt dispatches only after the speaker's audio
  DRAINS (the splice's AudioDrained edge + tail pad, or the fallback
  margin - see the release-path bullet), and exactly ONE side greets -
  side A (side B's loop is built with TalkLoopConfig.SuppressGreeting;
  A's greeting audio crossing into B's mic is the conversation seed).
- The seams are OPTIONAL and nil-by-default: TalkDeps.HoldPrompt parks
  a denied final (one slot, latest question wins, telemetry
  kind=prompt_held / prompt_released / prompt_dropped), TalkDeps.
  TurnState publishes turn_start/turn_final/idle edges. Non-link calls
  (phone, plain local, signal) NEVER receive them - routeFrame's
  registry lookup on the call's TARGET returns nil and the loop
  constructs exactly as before. Do not wire these seams onto phone or
  plain-local loops.
- The coordinator rides LinkTurnRegistry keyed by the REGISTRY TARGET
  ("local:link-a-<suffix>"), registered by LinkManager.Open BEFORE the
  leg opens: the child's setup frame can reach routeFrame while Open is
  still running, and the ccid is not knowable then - the target is.
  Close/unwind unregisters; a stale target must never bind a future
  call.
- Every release path matters: turn_final NO LONGER releases the floor -
  and it no longer even BLIND-ARMS the drain watch. The floor releases
  on AUDIO DRAIN driven by the child's C1 played edge: the HOLDER's
  turn_final sets
  the splice's EXPECTED-final state (LocalSpliceFor(ccid).ExpectFinalDrain,
  the same registry HandleInterrupt rides) plus a LONG fallback margin
  (default 12 s, LinkDeps.TurnMargin); the child's played frame
  (routeFrame -> MarkFinalPlayed) then ARMS the watch, and the first of
  (LocalSpliceDeps.AudioDrained edge + 250 ms tail pad) or (fallback
  fire) clears the floor and flushes the peer's park (telemetry
  kind=floor_released, reason=drained|margin). HOLDER-ONLY RELEASE
  (link-b9d221d8d8cddcce, measured 13.70 s overlap): the coordinator
  tracks c.holder (set on claim/turn_start); a NON-holder's turn_final
  is a listening side's chained response to speech it heard and records
  bookkeeping ONLY - no expect-final, no margin arm, no release (the
  live defect: B's non-holder final armed its own margin, and the 12 s
  fire released a side=b floor while holder A was still audible; the
  release funnel refuses when c.holder != side). A bare turn_start from
  a side whose peer holds the floor is a LISTENER edge (bookkeeping
  only) - a real loop never emits one while parked - and must NEVER
  steal the floor. Only the holder's idle edge releases the peer now.
  MARGIN AUDIO-AWARENESS: the margin's fire consults the holder's live
  splice tail (runLocalSplice.TailActive: pending>0 or an outbound
  touch inside the margin window); a fire while the tail is still
  AUDIBLE RE-ARMS once (kind=margin_rearmed) instead of cutting it, and
  the second consecutive fire releases regardless - the hold stays
  bounded at 2 x margin. HOLDER CHAINING: a holder's turn_start AFTER
  its own turn_final marks reDispatched; a drained/margin edge then
  DEFERS the release (kind=floor_release_deferred) - the floor stays
  with the holder and the peer flush waits for the holder's next
  quiet-final release, so a parked prompt can never flush between the
  holder's sub-turns (the release->reclaim window of the 07:32-07:34
  chain). The defer branch arms a ONE-SHOT watchdog at one margin
  (reason=floor_watchdog): a live chain's own edges invalidate it (gen
  bump on every terminal edge), but a holder whose LLM hangs forever
  after chaining (turn cap 0, reaper suspended mid-turn) dispatches no
  further edges, so the watchdog is the bounded backstop - the fire
  bypasses the chain guard AND the busy gate and releases for real. Why the blind arm was
  still early (link-2fdc3f5fd8d81805: 3/4 drained releases fired, but
  MID-REPLY - the child's inter-sentence synth gaps of 1.4-3.8 s exceed
  the 1 s quiescence window, so pending==0+quiet faked a drain while
  queued sentences still had audio to play -> 60 short both-audible
  runs, 10.94 s overlap): at turn_final the TEXT stream is flushed but
  the child is only partway through PLAYING it. The played edge is the
  audio truth: after it nothing more CAN arrive for the turn, so the
  played-armed watch fires at pending==0 + the SHORT final quiet window
  (500 ms, localDrainQuietFinal) and DOES fire on a silent post-arm
  stream (the edge itself is the touch - emitted strictly after the
  final audioOut.Write on the same worker goroutine). If the edge never
  arrives (old child binary, denied/aborted final stream), the 12 s
  margin still releases - watchdog role unchanged; on a barge-in the
  idle edge already releases the floor immediately (the child emits NO
  played for aborted streams - deliberate: a phantom edge on dead audio
  would fake an end-of-turn). turn_final alone only clears BUSY - the
  daemon pacer still holds
  seconds of paced audio after the text flush (measured 6.70 s
  both-sides talk-over on link-1108d776b0a1efb3), and HoldPromptFor
  gates on floorHeld so a FRESH peer prompt cannot claim the floor
  during the tail either. The BLIND arm shape (ArmDrainWatch, kept for
  non-link callers/tests) fires only after (a) a touch
  POST-DATES the arm (the early-fire guard: the final sentence's TTS
  may still be synthesizing) and (b) the pacer pending backlog is zero
  and the stream is quiet for the FULL window; a silent stream NEVER
  fires on that shape - denied turns ride the fallback margin (late,
  never early). The played-armed shape replaces (a) with the edge
  itself and shortens (b)'s window; both shapes keep the pending==0
  gate. The pump's sub-chunk CARRY
  never gates the fire (a real TTS stream's byte total is never a
  multiple of 640 - carry==0 was unreachable, 0/5 drained releases on
  link-6eb490abd8349958). idle (barge-in
  cancel, unbind, teardown) releases IMMEDIATELY (nothing is playing),
  and a park alive at loop exit is dropped with a log (never dispatched
  into a dead call). The splice-side watch only exists for LINK calls:
  LocalDeps.LinkTurns resolves per Open by registry target (plain local
  -> nil dep -> no watch goroutine, byte-identical). The bridge
  seeds each mic with linkSilenceSeedChunks exact zero chunks at dial:
  with B silent on open, the splice's first-byte JoinTimeout gate must
  not depend on greeting audio (0-byte reads never satisfy it).

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
- The daemon sends a C1 `end` frame (`{"call_id":...,"event":"end",
  "reason":"<outcome>"}`, `last` OMITTED per the frames contract) as the
  FIRST sink of `VoiceWiring.releaseCall`, best-effort before the loop
  drop and ctx cancel: the child's `handleEnd` tears the pipeline down
  through the frame protocol instead of only noticing socket EOF
  (audit 2026-11-09 finding 10). A send failure (no child, dead conn)
  is logged and NEVER blocks or fails the teardown — EOF stays the
  fallback. Do not move the send behind the loop cancel.
- Webhook decoupling of the goal hold (audit 2026-11-09 finding 1): the
  inbound spawn seam (`voice_wiring.go`) must NEVER run `holdPhoneCall`
  synchronously — `AcquireCallHold` blocks for a full prior goal round,
  and stalling `HandleCallEvent` pins the carrier webhook handler until
  Telnyx times the POST out and redelivers an already-answered call. The
  spawn arms a per-call hold gate (`VoiceEmployeeWiring.gates`,
  `holdTracker`) and runs hold + `OnCallStart` on a goroutine. The
  serialization the synchronous hold provided is preserved at the only
  place it matters: the talk loop's turn runner (`turnRunnerFor`) awaits
  the gate before the employee's FIRST turn. Fact-free filler/greeting
  lines MAY play while the gate pends (they read no employee state; the
  supervisor gate stays the `say` contract). Release ordering is
  load-bearing: `releaseCall` drops the gate BEFORE the `ReleaseHook`
  heldCalls peek, so a hold still pending on the mutex at terminal time
  is torn down by EXACTLY one side — the hook when the hold already
  registered, the hold goroutine's rollback (re-checked after
  `AcquireCallHold` AND after `OnCallStart`: `ReleaseForCall` +
  `OnCallEnd`) when it had not. Both sides ride the same once-guards
  (heldCalls map delete, binding live check). Keep the rollback
  re-checks if you touch either path — without them a
  released-while-pending call parks the employee mutex forever.
- Deliberate redelivery-storm posture (audit 2026-11-09 finding 11): the
  webhook handler answers 500 on manager errors so Telnyx redelivers
  (the mapping choice, leaf 10 Notes, still "500 until observed
  live"). A redelivered `call.initiated` for a leg already answered can
  therefore re-enter the spawn seam while the first attempt is live:
  the registry Acquire fails (`ErrDuplicateTarget`/`ErrBusy` employee
  already live), the manager hangs the leg up, and Telnyx eventually
  stops redelivering a call that is answered-then-hung-up. Accepted:
  no webhook-path dedupe by ccid exists on purpose (a persisted
  seen-set across restarts would be the only sound version, and the
  carrier gives no idempotency key). Do not "fix" the storm with a
  synchronous dedupe on the handler goroutine.
- `call.answered` for an untracked id is refused; hangup for an unknown
  id is nil (idempotent teardown). `OnMediaStart` is ERROR-AWARE
  idempotent (audit 2026-11-09 finding 4): a registered CLEAN splice
  stays a no-op on re-send, but a splice that failed with a
  non-cancellation error re-arms — the next media start re-splices
  (media reconnect restores audio); the marker dies with releaseOnce.
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
- Restart-mid-ring residual (audit 2026-11-09 finding 8, documented
  posture): the dialer's ccid -> done map and the manager's per-call done
  channels are in-process. After a daemon restart, an effect ledger
  row reads completed + receipt, and `Dialer.doneFor` re-resolves the
  ccid through `Manager.WaitCall` — which for an id this process never
  tracked returns a FRESH channel that never closes (M3: no retention
  across teardown/restart; the outbound tombstone covers only SAME-
  process post-teardown re-resolution). The caller's bounded ctx is the
  ONLY safety net by design: a mid-ring call whose carrier leg survived
  the restart is genuinely "state unknown" — the new process has no
  media bridge for it and no way to know if the leg answered elsewhere.
  Do NOT close the gap by guessing "ended": a false terminal would
  unpark/release state for a call that may still be live on the
  carrier. Hardening landed (leaf 04 T3): in `doneFor`, when
  neither `d.dones[ccid]` nor a tracked/tombstoned `WaitCall` channel
  exists for a receipted ccid, return an ALREADY-CLOSED channel + WARN
  "state unknown" instead of `make(chan struct{})`, so WaitCall-based
  callers observe the unknown terminal immediately rather than hanging
  to their ctx deadline. Until that lands, treat an unbounded Done wait
  after a restart as expected and rely on the caller timeout.

## CallLeg adapters (tree 20261110-voice-callleg, leaf 01)

- The `CallLeg` interface (`callleg.go`) + three adapters
  (`callleg_phone.go`, `callleg_local.go`, `callleg_signal.go`) are
  OBSERVABILITY + facade only: every method is a call-through onto the
  leg's existing public API (Dial/Open/End). `Manager.releaseOnce` +
  the release hook remain the SINGLE terminal funnel (frozen 20261109)
  — the adapters never add a second teardown path. `Events()` rides the
  ADDITIVE terminal-observer lanes: `Manager.AddTerminalObserver`
  (phone/local; notified after the hook, outside the mutex) and
  `signalleg.Leg.AddTerminalObserver` (signal; claimTerminal-guarded
  sites only). DO NOT steal the single-occupancy `SetReleaseHook` for
  adapter events.
- Events coverage (tree 20261110-voice-callleg leaf 02, shipped per
  leg): terminals (Ended / Errored, mapped from the Manager Outcome:
  OutcomeFailed -> Errored, everything else -> Ended) on all three
  legs, plus NON-terminal kinds where the leg has an observable seam:
  phone `Connected` rides the ADDITIVE answered-observer lane
  (`Manager.AddAnsweredObserver`, notified after the single-occupancy
  answerHook — the H1 timer-cancel seam is untouched); local
  `Connected` rides the client-join edge (`LocalLeg.AddClientJoinObserver`,
  fired once by the splice's first mic byte via `LocalSpliceDeps.OnClientJoin`
  — observability only, the join gate never waits on an observer);
  signal `Reconnecting` rides `Leg.AddTransitionObserver` (the same
  lane carries CONNECTED, which the signal adapter deliberately does
  NOT write to the bus: the frozen 00T parity gate reads Ended as the
  FIRST signal-bus event after an injected CONNECTED). Ringing is
  unmapped on every leg (no observable ring edge: Telnyx delivers no
  outbound-ring webhook event, and a Place-time emit would pre-empt
  the frozen gates' terminal reads). A non-terminal emit observed
  AFTER the ccid's terminal latched is dropped by the bus — terminals
  win the ordering.
- Import-cycle rule (build binding): `internal/signalleg` imports
  `internal/voice`, so `internal/voice` may NEVER import signalleg.
  The signal adapter consumes the voice-defined `SignalLegSeam`
  interface; `*signalleg.Leg` satisfies it structurally. Nothing in
  voice names signalleg.
- `Accept` on legs without a string-id inbound path is a NAMED refusal:
  local returns `ErrNoInbound` (contract); phone/signal return
  `errInboundAdoptionUnwired` until leaf 02 extends the seam.
- Event delivery is best-effort non-blocking (buffered bus, drop on
  full): a stalled or absent Events() reader can NEVER block call
  teardown.

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
  (per-barge-in, F-1 re-arm by the next say); the splice re-arms
  ONLY on FRESH audio of the new turn (bughunt 2026-11-10 A6): the
  interrupt bumps the SHARED bargeGate generation (barge_gate.go); each
  chunk's staleness is judged ONCE, at READ time (the pump snapshots the
  generation before the socket read and carries it with the chunk).
  Chunks whose snapshot is behind the bump, or whose read landed inside
  the post-cancel residue window (stamped only when playback was live at
  the cancel), are stale: the PHONE leg discards them at the pump
  pre-enqueue (they never join the queue), the LOCAL leg drops them at
  the pacer consume from that same carried verdict (itemStale ->
  ConsumeStale). The interrupted stream's buffered tail can neither
  re-arm the latch nor replay over the new answer. The pendingInterrupt handshake keeps the
  C5 interrupt-before-prompt order
  even though the RMS trip fires on the audio-pump goroutine; mic PCM
  keeps flowing to STT during playback so the employee reforms from what
  the caller said. The trip's STT EVIDENCE is ONE-SHOT: a trip zeroes
  lastFinalAt, so a second window needs a NEW final transcript (stale
  evidence cancelling the very turn its own final dispatched was a live
  defect, call local-2d84c384fd93f447 2026-11-09, leaf 02). The evidence
  must additionally POST-DATE the turn's DISPATCH watermark
  (lastDispatchAt, stamped by consumeTranscripts after a FINAL prompt
  emit succeeds; dispatch-watermark defect call local-611f46b1c3fe5c5b
  2026-11-11): the prompt that started the in-flight turn is never
  evidence of NEW speech, so a suppressed trip keeps the evidence and
  the playback intact. A suppressed trip NEVER consumes the watermark
  evidence (pinned by pipeline_rmsdispatch_test.go). non-speech
  finals ([BLANK_AUDIO]/(sighs) class, nonSpeechTranscript) never dispatch
  a turn and never stamp evidence. RMS must NEVER run on phone legs -
  they keep the Tier A STT-partial trigger. Never move the trigger to a
  leg-agnostic branch.
- Barge-in must SILENCE the client speakers, not just stop new frames:
  internal/tts.AudioPlayer tracks ALL live oto bursts and Stop() pauses
  every one of them (Stop was a documented no-op - cancelled audio played
  under the next answer, leaf 01); SpeakerSink.Clear drops the buffered
  burst too; the CLI playout caps pending socket audio
  (liveMaxPendingAudioBytes, 375 ms at L16/16k, cmd/meept/voice.go) so
  kernel backpressure bounds the tail.
  Play NEVER pauses predecessors: bursts overlap in oto's mixer (the
  leaf-01 per-handoff pause chopped paced real-time audio at every ~150 ms
  boundary - the same-day chop regression; cancel-time pause only is the
  contract, pinned by player_recall_test.go).
- LocalSplice has NO reorder and NO transcode by contract:
  unix sockets preserve order (no reorder window), and the local leg
  negotiates L16 s16le@16000 only - any other chunk size is the named
  ErrL16Only refusal (returned by RunLocalSplice; declared as
  localsplice.go's ErrL16Only sentinel).
  Outbound stays paced at one 640 B chunk per 20 ms (the uptractor
  rule; localsplice.go:14-16). Do not "harden" it with a reorder buffer.
- The shared barge gate (barge_gate.go, bughunt 2026-11-10 A6
  generalized from the local leg to BOTH media splices): bargeGate owns
  the fresh-audio generation and the post-cancel residue window
  (bargeResidueQuiet, 250 ms). Bump(lastTouch) is the ONLY
  generation-increment and is called by the cancel path
  (HandleInterrupt / bargeIn); it stamps the residue deadline only when
  playback was live at the cancel (an outbound paced touch within the
  window) - with no live stream the very first post-cancel PCM is
  unconditionally fresh (the silent-then-say posture is preserved). The
  residue window is NOT a parameter: do not plumb it into config, and
  do not fork a second gate (1c86c369's fork-prevention rule) - both
  splices ride the one type.
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
  - phone inbound: the spawn seam in `voice_wiring.go` arms the call's
    hold gate and runs `holdPhoneCall` (AcquireCallHold + OnCallStart)
    ASYNC on a goroutine after registry Acquire — never on the webhook
    goroutine (finding 1, see Release fan-out above). The gate is
    awaited by the talk loop's turn runner before the first employee
    turn, so the hold still serializes the goal round. The closure reads
    the employee pointer filled after `WireVoiceEmployee`.
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

## Operator telemetry (voice-events tree)

- Every live call carries a fixed-size per-call event ring
  (`internal/voice/events.go`, `TelemetryRing`; 64 events/call, rings for
  the last 32 call ids, FIFO evicted). Producers: the talk loop's
  OPTIONAL `TalkDeps.OnEvent` hook (nil default = zero emissions, the
  loop is byte-identical when unset - mirror rule of the link seams above:
  NEVER make it mandatory) appended at the existing telemetry log sites
  (prompt_dispatch / turn_final / barge-in (both trigger paths) /
  call_ended / the link park lifecycle), plus `VoiceWiring.routeFrame`,
  which appends `heard` when a FINAL (last:true) prompt frame arrives -
  the mic-speech evidence that precedes the loop's dispatch of the same
  text (partials never record). The `heard` append rides a SUCCESSFUL
  enqueue: a prompt frame the full-queue default branch drops is never
  recorded as heard (the telemetry must not lie about delivery).
  `releaseCall` appends a terminal `call_ended` ONLY for a call that
  never armed a talk loop AND never recorded anything (`TelemetryRing.
  HasRing` - the ever-recorded set, which SURVIVES FIFO eviction: a
  live-map miss cannot tell "evicted" from "never existed", so probing
  Since/found there resurrects evicted rings with synthetic ends).
- Egress is the seam-based `voice.local.events` RPC (cursor-poll over the
  existing socket). NO new bus topic, NO new WS class: the ring is
  in-process control-plane state. The daemon adapter (`callEventsSeam` in
  voice_local_rpc.go) projects onto `rpc.VoiceCallEvent` rows - the ring
  event's `Seq` is the cursor domain (per-call, 1-based, monotonic).
  The result's `cursor` ECHOES the request cursor when nothing new came
  back (advance-by-assignment is safe). `truncated` means ONLY genuine
  eviction (rows the cursor pointed at were overwritten); a limit-capped
  backlog reads as `has_more` instead (add fields, never remove, on this
  wire). A ring-less or unwired read answers the empty envelope
  (found:false), never an error: the CLI poller ticks at 500 ms and
  teardown races must not surface as failures.
- Consumers: `cmd/meept/voice.go` `voiceEventTracker` - one poller
  goroutine per known call id printing `[<label> <employee>] <line>`;
  best-effort by contract (a failed poll retries, never fails the run);
  a found=false answer ends THAT ccid's poller (one feed-ended line when
  it had consumed events; silent when never seen - an evicted/unknown id
  must not tick forever); each round-trip is bounded (5 s) so stop()
  cannot hang on the RPC client's multi-minute retry ladder, and an
  abandoned poll never prints after stop returns. `voice link` (auto-on,
  a/b prefixes) and `voice local` (auto-on) both drain the tail
  synchronously after close/end so the final seconds' events print before
  the transcripts.

