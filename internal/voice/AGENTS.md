# internal/voice/AGENTS.md

Guidance for AI agents working in `internal/voice/` (and, by contract,
its child-side siblings `internal/voiceframe`, `internal/voicechild`,
`cmd/meept-voice`). Referenced from the root AGENTS.md; the full voice
sidecar contract lives here. Update both in the same commit per the
root maintenance rule. User-facing docs: `docs/workflows/voice-sidecar.md`
(spec rev 3 + as-built), `docs/workflows/voice.md`,
`docs/workflows/voicechild.md`. Plan trees:
`docs/plans/20261102-voice-sidecar/`,
`docs/plans/20261106-voice-local-leg/`, and
`docs/plans/20261113-filler-clips-and-context-warm/` (filler clip cache +
lane pools + farewell + partial context warm — Contracts C1-C6 live in
that tree's master.md; the invariants below are the as-built form).

## Frames (C1): one JSON object per line, every frame carries call_id

- QWEN3 TTS WIRE CONTRACT (child-side, `internal/voicechild/providers`):
  vendor `qwen3` posts to `<tts_base_url>/v1/audio/speech/pcm-stream`
  (NOT the OpenAI `/v1/audio/speech` route, which buffers the whole
  utterance — measured ~1.2 s first byte). The request body is the
  route's own shape `{"text","voice","chunk_size":4}` with NO
  `stream`/`response_format`/`input`/`model` fields; cloud `openai`
  keeps the legacy shape byte-identically. The response is a
  length-prefixed frame stream (`[4-byte BE uint32 length][s16le PCM]`,
  zero-length = EOF) — `pcmstream.go` strips the envelope incrementally
  before the shared 24 kHz -> 16 kHz resampler, so first audio is not
  gated on a whole-frame/whole-utterance buffer. The clone reference
  still resolves server-side by voice id (`vc_ce7d47c6`); no reference
  audio is ever sent in the request. Barge-in `clear()` cancels the
  request ctx (route aborts cleanly on disconnect); keyless
  construction, resampling, and the fresh-dial/one-retry transport are
  unchanged. Pinned by `tts_qwen3_test.go` (URL + body shape) and
  `pcmstream_test.go` (split headers, terminator, truncated header).
- The shared text socket hosts ONE child connection; every pipeline is
  keyed by call_id. Frame JSON keys are BYTE-EXACT (`call_id`, `event`,
  `text`, `last`, `from`, `to`, `direction`, `codec`, `sample_rate`) -
  a wrong key silently breaks the child. The codec lives in
  `internal/voiceframe` (stdlib-only; shared by daemon and child; see
  below).
- `Last` is `*bool`: an omitted `last` (setup, interrupt, end) must stay
  distinguishable from an explicit false. Only final prompts
  (`last:true`) start an employee turn; partials update preview only;
  final transcripts within the `voice.turn_confirm_ms` confirm window
  aggregate into ONE turn (voice-startguard C1: the provider keeps
  flushing segments at 500 ms wall-idle - link pumps need it - but the
  child's dispatch layer aggregates the breath-pause fragments instead
  of answering each). A final that lands while playback is live (or
  the barge-in latch is tripped) dispatches immediately, merged with
  any pending buffer; `clear` DROPS the pending buffer (the
  interrupted partial rides the dispatch-cancel); stream EOF and
  hangup flush it; `turn_confirm_ms=0` restores legacy per-final
  dispatch.
- DTMF frames decode as a counted no-op - a voicemail-tree "press 1"
  must never error the call.
- The child sends setup/prompt/interrupt/end/played/clip_ready; the
  daemon sends say/clear/end/clip_render. `played` (EventPlayed) is the
  child's end-of-turn AUDIO
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
  TestTalkLoopGreetingAndFiller). Every filler-class say may carry a
  `clip_id` naming pre-rendered PCM (lane pools + clip cache: see
  "Filler clip cache"); Text stays the transcript source either way.
- The daemon sends ONE extended C1 `keys` line per call (voice-tierA
  leaf 03, internal/daemon/voice_keys.go) through `TextServer.SendRaw`
  right after the talk loop registers on setup: `stt_key`, `tts_key`,
  plus the selectors `stt_vendor`/`stt_model`/`stt_base_url`/
  `tts_vendor`/`tts_model`/`tts_base_url`/`tts_voice` (absent = "" = the
  hardcoded deepgram/openai defaults; a non-empty `stt_base_url`/`tts_base_url`
  is dialed instead of the cloud default - `[voice].stt_base_url` /
  `[voice].tts_base_url` feed them daemon-side, global config by design)
  plus the startguard C1 selector `turn_confirm_ms` (the RESOLVED
  per-employee `voice.turn_confirm_ms` since voice-peragent leaf 01:
  `voice.employee_leg` row > global; the child decodes an ABSENT
  field as its own 900 ms default and an explicit 0 as legacy
  per-final dispatch) plus the voice-microfrag short-final gate and
  its leaf-01 improvements: `min_turn_words` (N>0 drops a FINAL whose
  bracket/asterisk-stripped residue has < N words, before evidence
  stamping and aggregation; absent/0 = off), `exempt_phrases` (whole-
  residue exemption set; absent = the child's shipped default set),
  `energy_gate` + `min_rms` (drop only when the utterance window's
  average mic RMS is below the floor), `fragment_decay_s` (effective
  N += min(consecutive drops, 3) for S s after a drop), and
  `reply_exempt_s` (short final within S s of the child's own last
  spoken-audio write is a reply: exempt). All five are additive
  optional keys; every improvement is strictly inert while
  min_turn_words is 0, so an old daemon line keeps the exact
  pre-leaf gate. Telemetry: INFO 'short final dropped' with
  reason=wordcount|energy; INFO 'short final exempted' with
  reason=phrase|reply|energy.
  The vendor/model/voice selectors resolve per call through
  `voice.ResolveProfile` (local-leg CL4: `voice.employee_leg` row >
  per-leg config > provider default) over the call's registry row
  (employee id + leg); an untracked ccid lands on the global values.
  The shared `voiceframe.Frame`
  carries NO key fields by design - `SendRaw` is the keys seam, and the
  child decodes the raw line (KeysFrame String/LogValue redacts). The
  child constructs its real providers from this frame (C3 factories in
  cmd/meept-voice); a pre-Tier-A child that ignores unknown keys fields
  keeps working (D4). The frame also carries `tts_ref_hash` (filler-clip
  identity, plan 20261113-filler-clips C1): hex sha256 of the clone
  reference audio, "" for a preset voice — sent always (no omitempty),
  read ONLY for clip-key identity, never by a TTS request; see "Filler
  clip cache". A daemon-side `open` frame (local-leg CL1) now
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
- The local leg's CLIENT CONTROL lane (`<ccid>.c.ev`, client_events.go)
  is a daemon-owned unix LISTENER beside the client pair carrying the
  barge-in clear as JSON-lines (`{"event":"clear"}`, one object per
  line, additive kinds allowed) - the pair sockets stay RAW PCM ONLY,
  so silence CANNOT ride them (bughunt 2026-11-11 defect 1: the ~750 ms
  ghost tail = the CLI's 375 ms SpeakerSink backlog + the last 375 ms
  oto burst, played because `SpeakerSink.Clear()` had ZERO production
  callers). BARGE-IN CONTRACT (2026-11-11): VoiceWiring.routeFrame
  emits EXACTLY once per LatchBargeIn transition; the CLI's
  drainLiveSpeaker marshals the token onto the drain goroutine (the
  sink's sole owner) and calls sink.Clear() - keep at least one
  production caller wired, and never Flush a post-clear EOF tail.
  Emit is drop-never-block (100 ms deadline, monitor-tap discipline);
  a missing/degraded lane = the old ghost-tail behavior, never a call
  failure.
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
- PACER FEED / SYNTH PREFETCH (starvation fix, call local-611897f3c43b2c98):
  the local pacer holds only a 64-chunk (~1.28 s) pump buffer, so it
  CANNOT cover a multi-second synthesis gap — once the buffer drains,
  the next client write waits on fresh child PCM and the gap is permanent
  dead air (`localsplice pacer stall gap_ms=...`). The feed contract is
  therefore CHILD-SIDE, not pacer-side: the child's depth-2 synth
  prefetch (`internal/voicechild/pipeline.go` `tryLaunchPrefetch`) MUST
  start the next utterance's `Speak` the instant its `say` frame is
  enqueued — even while the head is still synthesizing (head-of-burst,
  `writingNow=false`) or the worker is parked between frames. Gating
  prefetch on `writingNow` left the talk loop's fast-first-audio remainder
  unsynthesized until the short opening finished playing, so qwen3
  voice-clone generation (0.7-18 s) surfaced verbatim as dead air. Depth
  stays bounded at two lookahead synths, playback stays strict FIFO
  (single worker, one reader at a time), and a barge-in still cancels
  every registered synth through the playGen gate (`clear()`), so the
  375 ms client recall bound is unchanged. Pinned by
  TestPrefetchStartsWhileHeadStillSynthesizing.
- Barge-in is PER CALL and per BARGE-IN: `clear` stops playback and drops
  queued drafts for THAT call_id only; other calls' pipelines are untouched
  (pinned by TestBargeInPerCall and the voice-bargein e2e suite). The
  latch is per barge-in, not per call: the child pipeline re-opens playback
  on the next say (playGen retirement kills the interrupted stream), and
  the splice re-arms when fresh child PCM arrives (bughunt 2026-11-07
  voice F-1 — both latches used to be once-per-call, muting the agent
  after its first interrupt). One interrupt latch per barge-in in the
  child pipeline.
- Barge-in drains the local splice's STALE AUDIO at the latch (turn-2
  leaf 01): the pump channel holds up to 64 already-read child chunks
  (1.28 s at 640 B / 20 ms) and drop-on-consume paced that backlog out
  AFTER the resume, so the operator heard the old answer under the new
  turn (live local-4dcfb291c2094ec9). `drainStalePump` empties the queue
  non-blockingly + the pump resets its sub-chunk carry via the
  `drainSeq` signal (the pump goroutine owns carry), counted by the INFO
  line "voice: localsplice barge-in drained stale chunks". After the
  clear the client hears at most the ~1 chunk already in paceOne. The
  outbound pump is DATA-ONLY: a terminal child-read error rides the
  `pumpErr` stash + `pumpDead` close, never the queue (a drained queue
  behind a latch would otherwise swallow it until ctx cancel — CL7);
  the pacer surfaces it once the queue is empty. ALSO production wiring:
  before this leaf NOTHING ever tripped the local splice's audio latch
  in production (`HandleInterrupt` had no caller outside tests) —
  `routeFrame` now calls `LocalSpliceFor(ccid).LatchBargeIn()` on every
  C1 interrupt frame BEFORE the talk-loop queue handoff (audio plane:
  latch + gate bump + drain; the loop keeps the control plane — clear
  frame, draft cancel). Pinned by TestVoiceRouteFrameInterruptLatchesLocalSplice.
- The barge-in TRIGGER is the child's STT-partial transcript branch
  (voice-tierA leaves 05/06, D1): pumpAudioIn feeds the bound STT
  session; a non-empty partial while playback is live emits interrupt
  BEFORE the partial's prompt frame (C5 order), once per barge-in
  (noteBargeIn latch, re-armed by the next say). The local leg's RMS
  trigger is now ML-GATED (Tier B, landed): a Silero VAD analyzer, when
  constructed, requires a loud window to also be VAD-confirmed SPEECH or
  the RMS trip is suppressed (see the four-contracts block); STT partials
  still drive the phone leg. When the ONNX runtime lib is absent the
  local leg degrades to the legacy RMS + STT-evidence gate — never a
  call failure (the "a phone call without barge-in is refused" spec line
  is superseded; see docs/workflows/voice-sidecar.md Tier A as-built).

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

## Per-call silence path (plan 20261111-voice-turn2 leaf 02): dedicated nudge, S+H windows

- **Three conversational-audio contracts for the talk loop (natural-speech fixes, live call local-4aaa6ea97de14a22):**
  1. **Connected-utterance speech** (`TalkLoopConfig.ConnectSentences`, wired from
     `!voice.supervisor_enabled`): the reply-per-sentence split exists ONLY to let
     the supervisor gate each sentence. When the gate is OFF, splitting into separate
     per-sentence TTS requests made qwen3 read the reply as a flat list with 400-760 ms
     artificial gaps between every sentence. ConnectSentences=false is MANDATORY when
     a supervisor is enabled (it must check each sentence); the daemon wires it from
     the config, so re-enabling the gate automatically returns to per-sentence.
     Acceptance on recorded audio: the child cadence line shows ONE synth stream over
     the whole reply (synth_ms≈write_ms) and the daemon pacer reports pace_stalls=0.
  2. **Fragment endpointing** (`voice.fragment_confirm_ms` / `pipeline.fragmentConfirm`,
     default 3000): whisper punctuates complete thoughts and leaves a mid-word cutoff as
     a trailing ellipsis (or no terminal punctuation). Such an unfinished final is held
     for the fragment budget (longer than `turn_confirm_ms`) so a caller who pauses to
     think is never answered mid-sentence; a complete sentence still dispatches on the
     fast window. The continuation joins the fragment into ONE prompt when it lands
     inside the budget.
  3. **Fragment clarification** (`TalkLoopConfig.FragmentClarify` / `voice.fragment_clarify`,
     default empty = off): when a dispatched prompt STILL ends in a trailing ellipsis after
     the fragment budget (the caller genuinely trailed off), the loop speaks the configured
     line (e.g. "Was that all? I'm not sure I understand you.") and SKIPS the LLM turn —
     a human asks for completion rather than coldly answering the half-broken sentence.
     The intercept runs BEFORE `Turns.Run`; only the trailing-ellipsis shape trips (a
     completed command like "Stop." never does). Fact-free like filler, so it bypasses the
     supervisor gate. Empty keeps the legacy ready-path byte-for-byte.
  4. **ML input speech gate (Silero VAD, Tier B)** — `VADAnalyzer` seam in the child
     (`internal/voicechild/vad.go`), wired by `cmd/meept-voice` via `SetVADFactory` →
     `providers.NewSileroVAD` (jargo `audio/vad.NewSilero`, embeds `silero_vad.onnx`).
     Built in `handleOpen` for the LOCAL leg only; fed per-chunk in `pumpAudioIn` (before
     `rmsBargeInCheck`); gates the RMS barge-in: when an analyzer is installed, a loud
     window that ALSO passes STT-evidence must additionally be VAD-confirmed SPEECH or the
     trip is suppressed (`speaking` state only) — background noise / a door slam / the
     agent's own TTS bleed that STT mis-heard can never cancel the answer. The underlying
     model runs once per ~32 ms frame, sub-ms, on the pump goroutine (never under playMu —
     mutexio), one ONNX thread. The ONNX runtime lib reaches the child as the daemon's
     `VOICE_ONNX_LIB` env (from `voice.onnx_runtime_lib`); the child translates it to
     `JARGO_ONNXRUNTIME_LIB` at startup (`cmd/meept-voice/main.go` run). A nil factory, a
     construct-time failure (runtime lib absent), or a phone leg all keep the legacy RMS +
     STT-evidence gate byte-identical — silence-by-design, never a call failure.
     Acceptance: the child logs `silero vad active` (constructed) vs `silero vad unavailable`
     (degraded); unit tests pin suppress-when-not-speech / trip-when-speech / close-on-teardown.

- The FIRST silence fire speaks the DEDICATED nudge line
  (`TalkLoopConfig.SilencePromptLine` / `voice.silence_prompt`, default
  "Are you still there?"), NEVER the filler round-robin: dead air has no
  pending answer, so a filler promise ("give me a second on that") is
  wrong semantics (operator complaint, live call local-4dcfb291c2094ec9).
  Fallback chain: empty configured line -> `fillerLine()` (byte-compat for
  zero-config callers, INCLUDING consuming a filler index); empty filler
  list -> no line (the fire still latches and re-arms). Telemetry: the
  fire logs INFO "voice talk loop: silence prompt" with `line` +
  `after_s` (elapsed since the window was armed) so live logs answer
  "what triggered this" without guessing.
- Two windows, not one doubled: nudge fires at S =
  `voice.silence_prompt_seconds`, hangup fires after the caller stays
  quiet ANOTHER H = `voice.silence_hangup_after_seconds` (the post-nudge
  re-arm uses H, not the full window). H <= 0 reuses S (legacy double-
  window). Template defaults S=H=45 reproduce the legacy nudge@45 /
  hangup@90 endpoints exactly (opt-in-defaults rule); the operator's live
  config sets S=10 for nudge@10 / hangup@55. The hangup reason names the
  live window (max(H,0 else S)).
- Turn-aware suspension is unchanged: a dispatched prompt STOPS the timer
  (the silence path never reaps an in-flight turn) and the draft-drain
  branch re-arms a fresh S window; `silencePrompted` resets per dispatch.
  Test seam: `silenceWindowUnit` (package var, second scale in production,
  ms scale inside the silence pins) — tests mutating it must never run in
  parallel with each other.
- AUDIO-AWARE DEFERRAL (self-talk fix, live call local-53b347f028f0a43f,
  defects S1/S2): the TEXT lane drains seconds before the reply's AUDIO
  finishes pacing (measured pacer stall gaps 1.3-3.8 s), so the drain
  re-arm's window (S1) and the loop-start window over the greeting tail
  (S2) could fire the nudge ON TOP of the agent's own voice ("like two
  agents talking"). The fix is the OPTIONAL `TalkDeps.AudioActive(ccid,
  window)` seam: at fire time, when it reports the call's own audio
  active inside the expiring window, the window RE-ARMS (same duration)
  instead of speaking — telemetry `kind=silence_deferred`, reason = the
  consecutive-deferral count, INFO "silence window deferred". Bounded at
  `silenceAudioDeferralCap` (2) CONSECUTIVE deferrals: at the cap a
  still-active tail does NOT speak the nudge over the agent (call
  local-a7588dbe40dbe78c) — `silenceCapSkipsNudge` latches the nudge,
  re-arms the hangup window H, and leaves the streak AT the cap, so the
  following fire takes the existing hangup leg. A permanently-audible
  stream therefore cannot suppress the hangup forever, and never hears
  the nudge as a second voice — the link margin's bounded-re-arm posture
  mirrored onto the silence clock. The
  streak resets at every `rearmSilence` (fresh turn drained) and at every
  real fire. Daemon wiring arms it for LOCAL legs ONLY
  (`silenceAudioActiveFor(inst.Leg)` -> `LocalSpliceFor(ccid).TailActive`,
  nil-safe splice lookup); phone/signal/legacy/test callers keep it nil =
  byte-identical silence path (mirror rule of the link seams). Pinned by
  talkloop_silence_audio_test.go + TestSilenceAudioActiveForLegs.
- CALLER-SPEECH HOLD (live call local-a7588dbe40dbe78c): AudioActive
  only sees outbound agent audio. A caller who has started speaking
  does not stop the clock until a last:true final arrives, so the
  nudge spoke over them. OPTIONAL `TalkDeps.CallerActive(ccid, window)`
  holds the nudge and the hangup when inbound speech was loud inside
  the 1 s lookback. It re-arms a fresh S window and is NOT charged
  against silenceAudioDeferralCap. A separate cap
  (silenceCallerDeferralCap, 6) still lets a stuck-hot mic reach the
  nudge. Daemon wiring arms it for LOCAL legs only
  (`silenceCallerActiveFor` -> `LocalSpliceFor(ccid).CallerSpeaking`).
  The splice stamps micLastLoud when an inbound chunk's s16 RMS meets
  CallerSpeechFloor (barge_in_rms/4; 0 stamps nothing). Phone legs keep
  the seam nil. Pinned by TestTalkLoopSilenceHoldsWhileCallerSpeaking
  and TestSilenceCallerActiveForLegs.
- LIVE TRANSCRIPT: `say` emits event kind `said` with the full sentence
  (not a preview). `heard` also carries the full sentence. `voice local`
  prints speaker on at the first played burst and speaker off after a
  350 ms gap, a clear, or end of stream, and attaches queued `said`
  lines to that window.

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
  code. jargo is pinned at v0.6.1 (upgraded 2026-11-13; v0.2.0+ needs
  go 1.27, which the tree now declares). API drifts
  are documented in internal/voicechild/jargo.go.
- `internal/voiceframe` is dependency-free (stdlib only): both sides
  share the codec and neither may drag the other's world in through it.
- Spawn refuses a STALE child binary: `checkChildRevision` in
  `ChildProc.spawn` compares the daemon's `vcs.revision`
  (debug.ReadBuildInfo) against the child's (buildinfo.ReadFile, no
  exec) and returns `ChildRevisionMismatchError` when both are set and
  differ. An empty revision on either side (test stubs, no-vcs builds)
  is allowed. This kills the 2026-11-14 defect where a rebuilt daemon
  kept spawning the Nov 12 `meept-voice` whose resampler lacked the
  odd-read alignment fix and crackled every utterance.
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

- **The local caller is ALWAYS approved (step-1c caller gate inert).** The
  local leg's caller is the operator at this machine: there is no carrier,
  no E.164 address, and no unapproved-stranger threat model. At the single
  production turn-stamp site (`bindingTurnRunner.Run`,
  internal/daemon/voice_wiring.go:2181) a local-leg instance is stamped
  `callerid.Info{Approved:true, Known:true}` (the `Number` is preserved),
  so enforcement's step 1c sees `caller_approved=true` and is inert — full
  tool use works for local calls. Detect the leg via the explicit
  `Instance.Leg == voice.LegLocal` field; NEVER a `local:` target-prefix
  heuristic. The phone/signal legs keep their resolved contact identity
  verbatim: unknown numbers stay gated by `voice.unapproved_caller_allowed_tools`
  (the message lane + ledger). That config is the allow-list for *unapproved*
  callers, NOT a ceiling on the trusted local operator — do not route the
  local leg through it. Pinned by TestBindingTurnRunnerLocalCallerApproved,
  TestBindingTurnRunnerPhoneCallerUnchanged (daemon/caller_resolve_test.go)
  and TestCallerMatrix_LocalCallerFullTools (daemon/caller_matrix_test.go,
  drives a local turn through the real executor: shell_execute runs).
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
  post-cancel residue window (stamped only when playback was live at
  the cancel), are stale: the PHONE leg discards them at the pump
  pre-enqueue (they never join the queue), the LOCAL leg drops them at
  the pacer consume from that same carried verdict (itemStale ->
  ConsumeStale). The interrupted stream's buffered tail can neither
  re-arm the latch nor replay over the new answer. The verdict is bound
  to the READ (bughunt 2026-11-11 C-2): it is stamped onto the chunk
  (localReadItem.stale) and re-judged by NOTHING — the consume side
  never re-reads the gate's residue state, so a pacer backlog that
  outlives the 250 ms window cannot convert a stale verdict into a
  client write (~1.28 s replayed tail in the live capture).
  ConsumeStale stays GENERATION-ONLY for pre-cancel chunks. The gate's
  real clock is MONOTONIC (bughunt 2026-11-11 C-19: a lazily-anchored
  wall<->mono pair under a DEDICATED anchorMu — Bump calls clockNano
  while holding mu, so the anchor lock must never be the gate lock);
  the injectable nowNano seam is unchanged, and monoNow lets a test
  step the wall while mono advances normally
  (TestBargeGateResidueDeadlineSurvivesWallStep). The child interrupt
  frame is ALSO the local leg's media cancel (bughunt 2026-11-11 C-1):
  TalkLoop.handleInterrupt resolves LocalSpliceFor(ccid).HandleInterrupt
  (ccid-guarded, nil-safe, per-barge-in latched) before its own
  pipeline drop, so the frame path latches the pacer, bumps the gate,
  sends the child clear, and fires CancelDraft (the hook then runs
  loop.HandleBargeIn idempotently behind the latch); pinned by
  TestTalkLoopInterruptFrameDrivesLocalSplice, which drives the REAL
  frame path — a direct HandleInterrupt call is exactly how the old
  pins missed the dead wiring. The pendingInterrupt handshake keeps the
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

## Per-employee acoustic controls (voice-peragent leaf 01)

One immutable rule with two halves:

- **Row > global.** Every acoustic/noise knob resolves through
  `voice.ResolveAcoustics` (provider selection through
  `voice.ResolveProfile`, barge-in RMS through `voice.ResolveBargeInRms`)
  in `internal/voice/profile.go`, exactly like the FreshSession tri-state
  pattern: a `voice.employee_leg` row pointer that is nil inherits the
  global `[voice]` value; a set pointer (including an explicit 0/""/
  false) wins for that employee's calls. The daemon resolves ONCE per
  call at the keys-frame emit (gate knobs + turn_confirm to the child),
  the talk-loop construction (silence trio + filler grace), and the
  open-frame construction (barge_in_rms) — no consumer re-implements
  precedence, and the TalkLoopConfig/LocalSpliceConfig SHAPES never
  changed, only the VALUES fed them. The C5 lane pools extend the same
  ladder to LIST keys (row list > row scalar > global list > global
  scalar > default, a list counting as set only when NON-EMPTY) —
  resolved in `voice.VoiceLanes` (`internal/voice/lanes.go`), consumed
  by the talk loop via `voice.PickLine` and by the clip planner at
  reload time against the LIVE config snapshot (see "Filler clip
  cache, lane pools, farewell").
- **Zero-config = byte-identical legacy — the pin, not the hope.** A
  config with no employee_leg acoustic rows and no leaf-01 gate knobs
  lands every value on today's global read, and the child-side gate
  improvements are strictly inert while `min_turn_words` is 0, so an
  old daemon line or a zero-config deployment reproduces the pre-leaf
  behavior exactly. Any change that makes a defaulted call behave
  differently from its pre-leaf self is a bug, not a feature.

## Filler clip cache, lane pools, farewell (plan 20261113-filler-clips-and-context-warm phase 1; Contracts C1/C2/C3/C5)

Context: every filler-class line (greeting, working-turn filler, silence
nudge, fragment clarify, farewell) used to ride the LIVE TTS lane,
blocking 0.7-4.8 s per synthesis and drifting the 3-second-clone voice's
timbre on the shortest utterances. Phase 1 pre-renders every lane line
once per (voice, model, text, reference-audio) and serves cached PCM.

- **Clip key (C1, FROZEN):**
  `hex(sha256("v1" \x1f text \x1f tts_vendor \x1f tts_model \x1f tts_voice \x1f hex(sha256(refHash))))`,
  implemented once in `clipstore.ClipKey` (`internal/voice/clipstore/key.go`)
  — consumers copy the formula through the function, never re-derive it.
  `refHash` is the sha256 of the clone reference audio bytes (callers hash
  once, ClipKey re-hashes to hex per the formula); nil/empty = the
  preset/non-clone case (hash-of-zero-bytes inner hex). Any component
  change flips the key = automatic invalidation + regeneration. The keys
  frame carries `tts_ref_hash` (hex of the reference sha256; sent ALWAYS,
  no omitempty, per the keys-wire convention) and the child
  hex-decodes it back to the exact bytes the daemon planner fed ClipKey
  (`voicechild.decodeTTSRefHash` pins byte agreement). `voice.tts_ref_audio`
  feeds ONLY this identity — the qwen3 shim never carries reference audio
  in a request — so two deployments sharing model+voice NAME but
  different reference audio get different clip keys. Missing/unreadable
  file: WARN once, "" identity (non-clone), never a failed call.
- **Store (C2):** `$MEEPT_HOME/voice_clips/<clipKey>.pcm` (raw s16le at
  the CANONICAL 16000 Hz rate — child adjudication A1: renders build
  their ProviderConfig at 16k, never the call's negotiated rate, so a
  PCMU@8k call can never poison the shared store; playback resamples
  from `Meta.SampleRate`) + `<clipKey>.json`
  `{key, text, vendor, model, voice, sample_rate, bytes, created_at}`.
  Content-addressed, NO index file. The ONLY cache-hit condition is a
  valid `<key>.pcm` whose length matches meta `bytes`; a corrupt/short
  file = miss (delete, regenerate). Clip files carry audio only — no
  secrets, no key material (the keys frame stays the only place TTS
  material lives, in child memory).
- **Frames (C3, all additive + omitempty):** a `say` may carry
  `clip_id` (the 64-hex key for its Text); daemon -> child
  `{"event":"clip_render","call_id":...,"clips":[{"key","text"}]}` capped
  at 32 entries per frame (the planner shards larger sets); child ->
  daemon `{"event":"clip_ready","key":...,"ok":true|false}` with `ok`
  a `*bool` (explicit false distinguishable from absent, exactly like
  `last`). Back-compat is the contract: an old child ignores
  clip_id/clip_render/clip_ready and falls back to live synth (today's
  byte-identical behavior); an old daemon never sends them. NO PCM ever
  crosses C1 — clip frames carry hex keys + text only. The say frame's
  TEXT stays the transcript source (R6): a clip is an audio-only
  attribute of the existing `t.say()` lane, NEVER a second speech path
  (echo_roster's single-origin invariant preserved).
- **Talk loop:** `TalkLoopConfig.Identity` (`*ClipIdentity`) gates it.
  When set, every filler-class say (greeting, filler, silence nudge,
  fragment clarify, fallback, farewell) goes through `sayClip` with
  `clip_id = ClipKey(text, Vendor, Model, Voice, RefHash)`; live-content
  turns NEVER carry clip_id; empty text / nil identity = no clip_id
  (byte-identical frame). The INFO filler/nudge/clarify/farewell lines
  carry `lane` + `clip` (true iff Identity != nil). A child store HIT
  replaces the provider call ENTIRELY (zero Speak, adjudication A2) and
  rides the exact same playback lane — pacing, barge-in clear, and the
  `played` edge are untouched because the swap happens at the reader,
  upstream of playbackStream. A miss / invalid key / open error falls
  through to live `Speak(text)` byte-identically (no opportunistic
  backfill — deliberately out of scope). `clip_render` entries the store
  already has answer `clip_ready ok:true` with ZERO synth (idempotent
  warm); misses render in the BACKGROUND at bounded concurrency on a
  call-scoped ctx (the worker dies with pipeline teardown, never after).
  A `clip_render` arriving BEFORE the keys frame queues (cap 256,
  drop-oldest with a WARN) and drains once providers exist.
- **Daemon planner (R3/R4, `internal/daemon/voice_clip_planner.go`):**
  the diff-and-ship pass runs ONLY when the sidecar is actually live.
  Gate: (a) a child connection is live (the pass mounts on routeFrame's
  EventSetup branch, or reload re-plan iterates `Registry.Live()`);
  (b) the resolved TTS identity is COMPLETE (vendor+voice non-empty after
  `voice.ResolveProfile`; MODEL MAY BE EMPTY — `tts_model=""` is the qwen3
  "use the server's loaded default" sentinel, and the empty string stays
  the C1 key component so the pre-rendered key matches both the keys frame
  and the child's synthesis model byte-for-byte). Both gate sites (the
  planner pass and the talk-loop `clip_id` stamping) resolve the identity
  through the single shared `daemon.ResolveTTSIdentity` helper so the
  clip_render keys and the say clip_ids cannot drift;
  (c) a voice employee owns the call (empty `voice.default_employee`
  never gets a live registry row). With `voice.enabled=false` WireVoice
  never constructs the wiring and NOTHING is generated or written —
  `clipstore.NewStore` alone creates no dir; the store materializes only
  inside a child `Put`. The daemon path is read-only (`store.Has`) plus
  frame sends. The pass is fire-and-forget on the call's loop ctx: a
  slow store diff never delays the keys frame or the talk loop, and a
  failed pass WARNs and degrades the call to live synth — never a
  teardown.
- **Config reload:** `startClipReloadWatch` subscribes the planner to
  the `daemon.reloaded` bus topic (subscription name
  `voice-clip-reload`) — this is that topic's first production
  subscriber. The handler re-reads config, republishes the LIVE [voice]
  snapshot (`w.currentVoiceCfg()` — keys frame, clip identity, and lane
  pools all read it after reload), and re-plans every live call on
  existing connections. A failed re-read keeps the previous snapshot
  (the daemon's keep-existing posture). Never blocks the reload path.
- **Lane pools (C5, `internal/voice/lanes.go`):** every filler-class
  line belongs to exactly one enumerable lane — greeting / filler /
  silence / fragment / farewell — so the planner can pre-render ALL of
  them. `VoiceLanes(cfg, employeeID)` resolves the five pools per
  employee with the precedence ladder **row list > row scalar > global
  list > global scalar > shipped default**, where a list counts as SET
  only when non-empty (an empty list never masks a lower layer — that
  is what keeps zero-config byte-identical: `voice.silence_prompt_lines`
  / `voice.fragment_clarify_lines` ship empty so the existing scalars
  keep binding; only the NEW `voice.farewell` pool ships non-empty:
  `config.DefaultVoiceFarewellLines()` = ["Goodbye!", "Bye!", "Talk
  soon."]). `PickLine(lanes, lane, cursor)` round-robins within a lane
  with a per-lane cursor (the existing fillerIdx discipline); a nil
  cursor takes the first line; the greeting special case is preserved
  (empty greeting resolves to the first filler line; both empty = the
  lane stays empty, the greeting is filler-class and NEVER hardcoded).
  Finite configured pools only — every pool line is cacheable, there is
  no free-form generated filler.
- **Farewell rule (R5):** when the caller's FINAL utterance, whole-
  normalized (case/punctuation-insensitive), matches the builtin bye
  phrase set (`farewellPhrases`: bye, goodbye, see ya, see you, talk
  later, later, that's all — a phrase embedded in a longer sentence
  never matches; deliberately a cheap rule, no LLM), the loop speaks one
  farewell-lane line as a FINAL (last:true) say with clip_id, cancels
  any in-flight draft first (no LLM turn — the fragment-clarify skip
  shape), then ends the call with reason `caller farewell`. The
  last:true shape hands the child the turn-final flush + played-drain
  guarantee so the goodbye audio completes before teardown; the farewell
  say is written BEFORE the end frame in program order on the Run
  goroutine. Exactly-once per call (`farewellSpoken` latch — a repeat
  bye logs and never double-speaks; a failed send still latches). When
  NO farewell line resolves (`farewellPoolConfigured` false — hand-built
  callers with empty Lanes + FarewellLines), the rule NEVER fires and
  "bye" rides the normal turn path byte-identically.

## Partial context warm (plan 20261113-filler-clips-and-context-warm phase 2; Contracts C4/C6)

STT partials (`last:false` prompt frames) already reach the daemon; phase
2 spends them on planting the imminent turn's prefix in the provider
prompt cache (and the llama.cpp KV slot under the session-affinity
scope) so the final turn pays decode, not prefill.

- **Opt-in, default OFF:** `voice.context_warm` false = the warmer is
  never constructed and every entry point is a pure no-op (the partial
  hook costs one atomic load of `ContextWarmRouting()` before doing ANY
  per-frame work) — byte-identical dispatch. `voice.context_warm_min_tokens`
  (default 60; <=0 falls back to 60) is the rough-token floor (3
  chars/token heuristic over the rendered prefix) under which a partial
  is never warmed — warming a trivial prefix is pointless traffic.
  Global-only; no employee_leg override this wave.
- **Debounce (`internal/voice/contextwarm.go`, self-contained in
  package voice — NEVER imports internal/daemon):** per-call timer whose
  window is the global `voice.turn_confirm_ms` snapshot (no per-employee
  override this wave; 0/legacy mode = fixed 900 ms, the child's shipped
  default) plus a 150 ms per-session fire
  throttle (re-arms for the remainder instead of dropping the text).
  Latest partial wins (gen bump replaces the pending timer); at most one
  IN-FLIGHT warm per session — a new fire cancels the previous request
  ctx (replace-discard, the cheap client-side backstop; the llm lane is
  cap-1 replace-discard anyway). A final (last:true) prompt
  (`RouteFinal`) or call end (`RouteEnd`, from releaseCall) cancels the
  pending timer AND the in-flight request ctx — no zombie request ever
  outlives the superseding turn or the call. No I/O under the mutex
  (mutexio): BuildPrefix and Warm run after unlock, on the debounce
  goroutine's clock, never on the frame path. Partial text is NEVER
  logged above DEBUG (length-only emissions).
- **Routing seam:** the daemon registers its warmer via
  `voice.SetContextWarmRouting` and `VoiceWiring.routeFrame` calls the
  nil-safe package entry points `RoutePartial` / `RouteFinal` /
  `RouteEnd`. Stop unwinds by re-registering nil (only if it still owns
  the slot).
- **Prefix identity is the whole feature** (daemon closures in
  `internal/daemon/voice_wiring_contextwarm.go`): BuildPrefix renders
  the EXACT system+history the imminent turn sends (voice persona
  override + guided-intake extension + global rules + moment block,
  live conversation history, the partial as the trailing user row) with
  the SAME alias and the SAME session-affinity scope. The scope is the
  turn's ACCOUNTING id — the voice lane's conversation id (`w.sessionFor(inst).ID`,
  matching RunOnce's conversationID -> WithTaskScope -> prompt_cache_key
  + affinity header); a divergence on ANY field silently kills the cache
  hit, which is the entire feature, and costs only the hit — never the
  turn. Warm forwards to the concrete client the real turn rides
  (session agent-loop chatter unwrapped ContextFirewall ->
  ProviderManager primary -> `*llm.Client`), so the warm lands on the
  same endpoint + affinity lane. `warmDispatcher` is the lane every
  client class implements (Anthropic/Codex have explicit no-op twins)
  so the closure never type-switches providers.
- **`llm.Client.Warm` (C4, `internal/llm/warm.go`):** non-streaming,
  max_tokens=1, built through the SAME buildChatRequest serialization
  path a real turn uses (system-prompt cache boundary, message
  rendering, stable tool order, prompt_cache_key, affinity header).
  The completion is discarded and NEVER logged. R8 error posture: any
  failure — transport, `*QuotaResetError`, 4xx, timeout, panic — is
  DEBUG-logged and swallowed. Warm participates in NONE of the loop
  machinery: no short-retry budget, no recordAliasFailure, no endpoint/
  alias block arming, no budget-gate consumption, no quota parking, no
  semantic-cache put, no `llm_calls` row, no metrics.Store record, no
  tier signal, no WS relay, no session message. It NEVER starts tools
  or an employee turn and never returns an error by design. One
  dedicated in-flight lane per client (cap 1, replace-discard); it
  never queues behind the real turn's slot gate; a hung warm is bounded
  by an internal deadline so the lane always releases. The HTTP dispatch
  bypasses doRequest ON PURPOSE (doRequest owns the slot gate, pacer,
  Error-level logs, and metrics rows).
- **E2E:** hermetic suites `e2e/suites/voice_clips` and
  `e2e/suites/context_warm` (manifest-registered; gates
  `make e2e-fast-area AREA=voice_clips` / `AREA=context_warm`).

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
  The link coordinator's floor lifecycle log sites emit the SAME kinds
  to the ring through `LinkTurnCoordinator.SetEventSink` (bughunt
  2026-11-11 C-14; optional, nil-default = zero emissions,
  byte-identical logging): margin_rearmed / floor_release_deferred /
  floor_released / floor_watchdog, keyed at EMIT time by the side's
  bound loop's live ccid (TalkLoop.runCcid, stamped at Run start; the
  link id is the fallback when the slot is unbound) — never a stale
  Bind-time snapshot. Pinned by TestLinkCoordinatorFloorEventsReachTelemetryRing.
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
  a found=false answer at cursor > 0 ends THAT ccid's poller (one
  feed-ended line when it had consumed events; silent when never seen -
  an evicted/unknown id must not tick forever). C-20 REFINEMENT (live
  regression, call local-53b347f028f0a43f): the ring is created LAZILY by
  the first Append, so a fresh call's first ticks read found=false at
  cursor 0 - that is the ring-not-yet-created race, NOT the feed end: the
  LIVE poller keeps ticking on a cursor-0 found=false (treating it as
  feed-end permanently silenced the operator's live feed - the user saw
  only the "call open" line). The DRAIN terminates on any found=false
  (the live loop is over there; the drain must not spin). Each round-trip
  is bounded (5 s) so stop() cannot hang on the RPC client's multi-minute
  retry ladder, and an abandoned poll never prints after stop returns.
  `voice link` (auto-on, a/b prefixes) and `voice local` (auto-on) both
  drain the tail synchronously after close/end so the final seconds'
  events print before the transcripts. `voice local` additionally reads
  `voice.local.transcript` (the call store's EndedCall - CALL-scoped, so
  a shared session never leaks prior calls' history) at call end and
  prints role-tagged transcript lines in the printLinkTranscript shape;
  best-effort like the poller: an old daemon / unwired store / RPC error
  prints the honest "(no transcript lines)" and NEVER changes the run's
  exit.

