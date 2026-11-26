# Auditor 4: voice (range 2026-11-06..11-11)

Scope: voice code changed in 7b510ed4..ea3d7082 — `internal/voice`, `internal/voicechild`, `internal/voiceframe`, `internal/signalleg`, `cmd/meept-voice`, and the `internal/daemon/voice*.go` wiring — read at HEAD and graded only if the defect survives at HEAD.
Audit point: e43a7cf3 (as briefed). Re-verification point: HEAD 997d6d02 (one commit past the audit point, `fix(scheduler)` — no voice file changed between the two; every finding below was read directly at HEAD). Working tree note: `internal/voicechild/pipeline.go` is dirty in the shared tree (another session); all pipeline reads used `git show HEAD:`.

The plain `**` pathspec in the brief under-counts the scope: with glob magic the range touches **46 non-test files / +14,119 lines** (not 15). Files read: 30 of 46 (full or hot-path). The method was: list -> diff-stat -> read HEAD version -> trace callers via `git grep` -> confirm at HEAD.

## Findings

### A4-1 — MEDIUM — `pipeline.close()` runs synchronously on the child's single C1 dispatch loop; every `end` frame blocks all other calls' frames for up to 5 s (unbounded on a hung ONNX factory)

`internal/voicechild/child.go:495-503` (`handleEnd`) and `:439-453` (`failCall`), both called from `handleLine` inside `serve`'s single `for { select { case line := <-lineCh: c.handleLine(readCtx, line) } }` loop (`child.go:387-391`). The wait is in `internal/voicechild/pipeline.go:2937-2941` and `:2919`:

```go
// child.go serve(): ONE goroutine dispatches every frame for every call
case line := <-lineCh:
    c.handleLine(readCtx, line)
...
func (c *Child) handleEnd(frame voiceframe.Frame) {
    p, ok := c.dropPipeline(frame.CallID)
    ...
    p.close(context.Background())          // synchronous
}
// pipeline.go close():
p.analyzerInit.Wait()                       // UNBOUNDED: waits on the ONNX factory goroutines
...
select {
case <-p.trStart:
case <-time.After(5 * time.Second):         // 5 s bound when the transcript consumer is slow to exit
```

Trigger: any call hangup (`end` frame) or any per-frame handler failure (`failCall`) while another call is live on the same child. Effect: the other call's `say`/`clear`/`end` frames sit in `lineCh` (cap 16) for the duration of `close()` — up to 5 s on a slow consumer exit, with no upper bound if `vadFactory`/`turnFactory` (ONNX session load) hangs. This is the exact class `internal/voice/AGENTS.md` forbids for the analyzer init: *"the ONNX dlopen/session load must never block the single C1 frame dispatch loop (it delayed `setup` and queued every other call's say/clear/end behind the load)"* — the init was moved off the loop (commit 82400e5b) but the teardown that WAITS on it was left on the loop. A barge-in `clear` delayed by 5 s defeats the 375 ms recall bound on the other call.
Caller evidence: `serve` -> `handleLine` -> `handleEnd`/`failCall` (child.go, quoted). Later commits: `cba79bb4`, `fe8fea32`, `1cee89d7` touched child.go/pipeline.go after the range but not this path (read at HEAD). Confidence: confirmed by reading the full path.

### A4-2 — MEDIUM — Synchronous LLM call-brief (≤5 s) + DB reads run inside the Telnyx hangup webhook handler via `ReleaseHook`

`internal/daemon/voice_employee.go:1132-1180` (`ReleaseHook`) -> `:1172` `w.runCallBrief(ctx, ccid)` -> `internal/daemon/voice_brief.go:200` `briefForCall(ctx, w.llm, lines)` -> `:150-172` `callBriefLLM` (real LLM, `components.go:5803` wires `BriefLLM = ledgerBriefLLMFunc(c.LLMClient)`). The call chain that puts it on the carrier webhook goroutine:

```go
// internal/comm/http/voice.go:261
if err := v.mgr.HandleCallEvent(r.Context(), ev); err != nil { ... http.Error(w, ..., 500) }
// internal/voice/manager.go:506-517 (call.hangup)
if fire { m.fireRelease(ev.CallControlID, OutcomeHangup, "call.hangup") }   // hook runs synchronously
// internal/daemon/voice_wiring.go releaseCall -> w.Employee.ReleaseHook(ccid, outcome)
// voice_employee.go ReleaseHook: OnCallEnd (≤5 s ctx) -> ReleaseForCall -> runCallBrief(ctx, ccid) -> notifyCallRelay
```

`runCallBrief` inherits `ReleaseHook`'s 5 s ctx, so the LLM brief is bounded by min(10 s, 5 s) = 5 s, then `notifyCallRelay` opens its own 5 s DB read. Worst case the webhook handler blocks ≈ 2 s (end-frame write deadline) + 5 s (OnCallEnd) + 5 s (brief) + 5 s (relay) before answering Telnyx. Telnyx's webhook timeout is a few seconds; on timeout it redelivers `call.hangup` (idempotent here, so no double-release, but a redelivery storm per the repo's own posture note).
Rule quoted — `internal/voice/AGENTS.md` Release fan-out: *"the inbound spawn seam must NEVER run `holdPhoneCall` synchronously — ... stalling `HandleCallEvent` pins the carrier webhook handler until Telnyx times the POST out and redelivers"*. The brief is the same shape on the hangup edge. `voice_brief.go:170-176` claims the brief *"NEVER fails or delays the teardown it rides on"* — it is itself inside the teardown and is what delays it.
Caller evidence: `comm/http/voice.go:261` -> `Manager.HandleCallEvent` -> `fireRelease` -> hook = `VoiceWiring.releaseCall` (`voice_wiring.go:1754`) -> `Employee.ReleaseHook` (`:1755`). Also reached from `LocalLeg.End` and `drainLiveCalls`. Later commits: `77b87bcc` (introduced), `223dd85c`/`dff75a6e` (lint/other, path unchanged at HEAD). Confidence: confirmed by reading the full path; the live impact depends on LLM latency (typically 1-3 s, i.e. already past a typical carrier timeout).

### A4-3 — MEDIUM — `TextServer.Send`/`SendRaw` are documented as serialized but take no write lock; concurrent writers can interleave a partial frame on the single child conn

`internal/voice/socket.go:322-361`:

```go
// Send ... "the single write is mutex-serialized so JSON lines never interleave.
// The mutex here exists to serialize connection access ... and is released before the write."
func (s *TextServer) Send(f Frame) error {
    ...
    s.mu.Lock()
    conn := s.child          // snapshot only
    s.mu.Unlock()
    ...
    return s.writeChild(conn, b, f.CallID, f.Event)
}
func (s *TextServer) writeChild(conn net.Conn, b []byte, callID, event string) error {
    ...
    if _, err := conn.Write(b); err != nil {     // no lock: two goroutines may be inside Write at once
```

The comment at `:337-339` states *"Send/SendRaw serialize on the snapshot mutex by necessity (whole JSON lines must not interleave)"* — false: the mutex only guards the pointer read. Production writers on distinct goroutines: the talk loop's `say` frames (talk-loop goroutine via `textFrameSender`, `voice_wiring.go:2297`), `releaseCall`'s `end` frame (`voice_wiring.go:1766`, manager/webhook goroutine), `sendKeysFrame` via `SendRaw` (`voice_keys.go:294`, the C1 read-loop goroutine), and the clip planner's `clip_render` (planner goroutine). A `net.Conn.Write` on a non-blocking unix stream socket returns a partial count when the send buffer is short on space and Go's `Write` then loops — the second goroutine's `Write` can land between the partial and the remainder. On macOS (the operator platform) the unix stream buffer is small — the repo's own comment in `pipeline.go:471-478` says *"macOS's local-stream socket space is small: writes block until the daemon drains"*. A `clip_render` frame (up to 32 entries × (64-hex key + text)) is several KB.
Effect: the child's `bufio.Scanner` (`child.go:342`) yields two undecodable lines -> `handleLine` logs "malformed frame dropped" -> a `say`/`end`/`keys` frame is silently lost. Later commits: `8a3ca015`, `4d537280` touched socket.go after the range (the write deadline), not the serialization. Confidence: likely (requires a partial write; not reproduced live).

### A4-4 — MEDIUM — signal-leg resampler drops the un-consumed tail of every chunk (2 of 320 samples on the 48k path): per-chunk discontinuity + 0.6% rate drift

`internal/signalleg/audio.go:200-233` (`resampler.Write`):

```go
step := int64(r.srcRate) << 32 / int64(r.dstRate)
total := int64(inSamples) << 32
for r.pos+step <= total { idx := int(r.pos >> 32); ... out = append(out, ...); r.pos += step }
consumed := int(r.pos >> 32)
if consumed > 0 { r.pos -= int64(consumed) << 32 }   // samples [consumed, inSamples) are never seen again
return out
```

For 48k->16k (step 3), a 640-byte read = 320 src samples produces outputs at idx 0,3,...,315 (106 outputs = 318 samples consumed); `consumed=318` resets `pos` to 0 and the next `Write` starts at ITS sample 0, so src samples 318-319 are discarded every chunk. The comment says the resampler "keeps the last sample value as interpolation context only" — no such `prev` field exists; the carry is only the odd-byte carry. Net: 320 in -> 106 out (should be 106.67): a 0.625% speed-up and a phase jump every 20 ms (audible click/periodic artifact) on the 48k socket path. The 16k<->16k path is `passthrough()` (byte-exact) and unaffected. Reachability: `startAudioAdapter` only runs when `st.socket != nil` (socket-audio transport wired) and `srcRate != 16000`; the research note says the I/O leg is 16k/48k. Later commits: `f8ee0727` ("resampler odd-byte carry") fixed the BYTE carry, not the sample-tail; `223dd85c`/`d526f6c5` lint only. Confidence: confirmed by arithmetic on the code (not reproduced with audio).

### A4-5 — LOW — `routeFrame`: the raw/normalized `From`/`Text` swap for FINAL prompts is unreachable dead code (and inverted relative to its comment)

`internal/daemon/voice_wiring.go:1354-1400` vs `:1401-1407`:

```go
if f.Event == voice.EventPrompt && f.Last != nil && *f.Last {
    ...
    select { case loop.Frames() <- f: ... ; default: ... }
    return                                   // every FINAL prompt returns here, frame UNMODIFIED
}
// Leaf 03 (T3 raw-capture): a FINAL prompt's From field carries the RAW
// transcript. The talk loop must dispatch the NORMALIZED Text ..., so the
// frame is rewritten here with raw text in From, normalized in Text ...
if f.Event == voice.EventPrompt && f.Last != nil && *f.Last && f.From != "" && f.From != f.Text {
    f.Text, f.From = f.From, f.Text          // unreachable for finals; also swaps the OPPOSITE way vs its comment
}
```

No runtime effect today (the child already sends normalized `Text` + raw `From`, and `talkloop.go` dispatches `f.Text` only — `git grep '\.From' internal/voice/talkloop.go` returns nothing), but the guard's check order makes it dead and the comment misdescribes the swap direction, so a future refactor that "fixes" the ordering would invert Text/From. Introduced by `80937176` (in range). Confidence: confirmed by reading.

### A4-6 — LOW — `signalleg.Place`: `End` reads `st.callID` outside `l.mu` (data race with the post-`StartCall` store) and can `Abandon` a reservation `Place` then `Complete`s

`internal/signalleg/leg.go:940` reads `st.callID == ""` with no lock; `Place` stores it under `l.mu` at `:813-815` only after `StartCall` returns and `Effects.Complete` runs at `:806`. Window: carrier `ENDED` arrives between `StartCall` returning and the store -> `onEnded` -> `End` reads the stale `""` -> `Effects.Abandon(...)`, then `Place` continues to `Effects.Complete(...)` on the same key (abandon-then-complete on one reservation) and `armRingingWatchdog` (which self-cancels via the `!ok` arm). `git log --oneline -3 -- internal/signalleg/leg.go`: `f8ee0727` (call-state leak fix) is the last touch; this read is unchanged. Confidence: likely (narrow window; `-race` would flag it).

### A4-7 — LOW — `ChildProc.EnsureRunning`: the dead-child respawn path leaves a stale idle timer armed; its fire can stop the NEW child early

`internal/voice/childproc.go:294-305`: when state is `stateDraining` and `p.rp.IsRunning()` is false, the code does `p.rp = nil; p.state = stateStopped; fallthrough` WITHOUT `p.stopIdleTimerLocked()` (the running branch at `:296` does stop it). The orphaned timer later fires `idleFired` (`:569-608`): if by then a new child is up and a call has ended (`stateDraining`, `calls` empty, `live()==0`), it stops the fresh child after the OLD window's remaining time instead of `IdleStop`. Also at `:575-580`, when `idleFired` sees `p.live() > 0` it flips to `stateRunning` with no timer ever re-armed until the next `NoteCallEnd` on this ChildProc. Effect: premature idle stop (child respawns on the next call — a latency blip, not a leak) or a child that never idles out while a registry-only call is live. Last touch `b9ad7639` (revision check) — unchanged. Confidence: confirmed by reading; low impact.

## Already fixed by a later wave

Candidates from the range diff that HEAD no longer has:

- `internal/voice/localsplice.go` pump pre-accept `ReadFromChild` "not connected" treated as terminal (link-72a04aebac963cdb half-death) — HEAD polls with a 5 ms sleep (`outboundLoop`, `isNotConnected` branch). Fixed in `8a3ca015`/`4d537280` era; verified at HEAD.
- `internal/voice/localsplice.go` barge-in backlog paced out one 20 ms slot at a time — HEAD has `drainStalePump` + `drainSeq` carry reset + read-time `stale` tag (`itemStale`). Verified at HEAD.
- `internal/voice/localsplice.go` `carry==0` drain gate (never fired in production) — HEAD excludes `audioCarry` from the predicate (`runDrainWatch`, comment cites link-6eb490abd8349958). Verified.
- `internal/voice/localsplice.go` wall-clock residue deadline (NTP step) — HEAD's `bargeGate.clockNano` is an anchored monotonic clock (`barge_gate.go`, C-19). Verified.
- `internal/voice/socket.go` unbounded daemon->child text write and unbounded `WriteToClient`/`ReadFromMic` accept wait — HEAD has `textWriteTimeout`/`clientWriteTimeout`/`clientAcceptTimeout` (A4/A7, `4d537280`). Verified.
- `internal/voice/childproc.go` stale sweep deleting live client-pair files — HEAD's `sweepChildPairStale` skips `.c.in`/`.c.out` (A9). Verified.
- `internal/voicechild/pipeline.go` `rmsEnergy` negative-sample wrap (`-u mod 2^64`) — HEAD takes `|x|` in int32 before squaring (`8a3ca015`). Verified.
- `internal/voicechild/pipeline.go` `playGen` not compared on the write path — HEAD re-checks `dropQueued || playGen != gen` after `pcm.Read` and before `audioOut.Write` (`playbackStream`). Verified.
- `internal/voicechild/pipeline.go` ONNX analyzer init blocking the C1 loop — HEAD builds on goroutines (`initAnalyzersAsync`, `82400e5b`). Verified (the teardown-side wait is A4-1 above).
- `internal/voicechild/pipeline.go` `lastDispatchAt` only stamped on successful emit (one failed emit disabled RMS barge-in for the call) — HEAD stamps on ATTEMPT (defect 2026-11-12). Verified.
- `internal/daemon/voice_wiring.go` setup check-then-insert zombie loop (A10) — HEAD does check+insert+liveness-recheck under one `loopsMu` section. Verified.
- `internal/daemon/voice_wiring.go` `voice.loopback_plain_addr` dead knob — HEAD boots it from `Start` (`voice_plain_boot.go`, validation-only pre-check, no double bind). Verified.
- `internal/signalleg/leg.go` StartCall/AcceptCall failure leaving the ccid in `l.calls` (A5 call-state leak) — HEAD reaps + latches under `l.mu` (`f8ee0727`). Verified.
- `internal/signalleg/audio.go` 200 ms sleep per chunk throttling the pump (A2) and the EOF tight spin (A11) — HEAD sleeps only on `n == 0`/EOF (`f8ee0727`). Verified.
- `internal/voice/callstore.go` transcript append dying with the turn ctx ("context canceled" lost rows) — HEAD uses `context.WithoutCancel` + 5 s bound. Verified.

Count: 15.

## Verified clean

- `internal/voice/callstore.go:300-322` `AppendTranscript` — atomic `json_insert`, ctx-detached, bounded; `:217-236` sweep is idempotent; `:327-370` `rows.Close` deferred; `today` filter is a string compare on uniform `RFC3339Nano` UTC text (only the first second after midnight can mis-compare — negligible).
- `internal/voice/childproc.go:614-644` `Stop` vs `:569-608` `idleFired` double `rp.Stop` — `RuntimeProcess.stopProcess` tolerates a second call (nil cmd -> pid-file recovery -> absent -> nil). `:476-478` `p.rp` install cannot race `Stop` (Stop waits on `startDone`, closed in the epilogue).
- `internal/voice/socket.go:206-227` `authenticate` — constant-time compare, no token in logs, byte-at-a-time read leaves frame bytes for `readLoop`; `:255-268` `claimChild` is the authoritative TOCTOU check. `:754-787` `AudioPair.Close` vs accept goroutines — `closed` checked under lock before install.
- `internal/voice/localsplice.go` `s16leRMSOver` (`:1500-1517`) — `sum >= thr*thr*n`, max thr*thr*n ≈ 3.4e11, no overflow. `paceOne` anchor/lag logic — re-anchors at ≥1 interval lag, never bursts.
- `internal/voicechild/pipeline.go:2570-2600` echo-guard cross-multiply `windowTotal*echoSamples*100 <= echoSum*windowSamples*130` — bounds: LHS ≤ 3.8e12×320×100 ≈ 1.2e17, RHS ≈ 1.6e17, both < 1.8e19. `rmsEnergy`/`rmsChunkOver` overflow-free. `:2045-2081` `tryLaunchPrefetch` + `playbackWorker` head handling preserve FIFO (synthJobs is always a suffix-order of sayQueue). `awaitSynth`/`retireSynth`/`drainSynthQueue` — exactly one receiver per `job.done`. `initAnalyzersAsync` turn branch — inner `analyzerInit.Add(1)` happens while the outer count is still held, so no Add-after-Wait-at-zero. `pumpAudioIn` — sole sender of `pcmCh`; `close(p.pcmCh)` only on the read-error path; the STT provider ends `trans` on ctx.
- `internal/voicechild/providers_wiring.go:70-104` `constructProviders` — no typed-nil reaches `p.stt`/`p.tts`: every constructor returns an untyped `nil` on error, `newPseudoStreamSTT` returns `nil, nil`. `NewSTT`/`NewTTS`/`newBaseSTT`/`newWhisperSTT`/`newRumbleSTT`/`newPiperTTS`/`newQwen3TTS` all return `nil, Err…` on missing config.
- `internal/voicechild/providers/tts_piper.go:69-103,142-200` — `exec.CommandContext` + single `finish()`/`Wait` under single-owner discipline; `werr` buffered(1) so the feed goroutine never blocks `finish`.
- `internal/voiceframe/frame.go:148-157` `ChunkBytes` — exact 640/160; `internal/voiceframe/cadence.go:84-104` `Report` — `sorted[(n*95)/100] ≤ n-1`, no OOB; `Mark` is single-goroutine.
- `internal/voice/codec.go` — `mulaw8ToPCM16` (4n-2 documented), `pcm16ToMulaw8` pair average fits int16, `SilenceFor` arithmetic bounded.
- `internal/voice/local.go:695-710` `Live()` — `st.ev.Path()` is nil-receiver-safe (`client_events.go:143-148`); `NewLocalLeg` typed-nil guards on Registry/TextServer/Manager; `rawTextSender{srv: l.deps.TextServer}` is non-nil by construction.
- `internal/voice/manager.go:291-300` `fireRelease` runs the hook OUTSIDE `m.mu` (so A4-2 is not a manager-mutex hold); `releaseOnce` tombstone bounded by `outboundDoneCap`.
- `internal/daemon/voice_employee.go:1082-1099` `AcquireCallHold` — `holdMu` released before the blocking per-employee `mu.Lock()`; `ReleaseForCall` map-delete once-guard. (The brief's position inside the hook is A4-2, not a lock issue.)
- `internal/daemon/voice_plain_boot.go:44-49` pre-validation is validation-only (`voice == nil` -> `ensureLoopbackAddr`, no bind) so the real listener does not hit "address in use".
- `internal/voice/talkloop.go:1012-1116` silence/turn/max-call timers — fresh-timer-per-arm discipline, `defer stopSilence()`/`stopTurn()`/`timer.Stop()`, `fillerTimer` stopped at `:358`/`:1616`.
- `internal/voice/link.go:1536-1620` `newMonitorListener`/`monitorTap` — accept goroutine ends on listener close, extras closed fast, writes deadline-bounded and drop-never-block; `close()` idempotent.

## Not audited

- `internal/voice/client_events.go` — created by 082eb032 (AFTER ea3d7082; not in the range). Only `Path()`/register/unregister were read for the `Live()` check.
- `internal/voice/link.go` `LinkTurnCoordinator` (`:366-1260`, `:1330` `Shutdown`, `marginFire*`, `releaseFloor`) and `LinkManager.Open`/`pumpStream` (`:1752-2110`) — only the monitor-listener/tap section and the function index were read; the floor state machine and the 4-conn dial/unwind were not traced.
- `internal/voice/dial.go` (+316), `internal/voice/daybrief.go` (+69), `internal/voice/binding.go` (+74), `internal/voice/callleg*.go`, `internal/voice/voiceattempt.go`, `internal/voice/splice.go` (phone reorder/pacing), `internal/voice/profile.go` — not read (time budget; dial.go's no-answer timer and splice.go's reorder window are the higher-value leftovers).
- `internal/voice/talkloop.go` beyond the timer block (`:1130-2154`: dispatch, filler, farewell, fragment clarify, barge-in latch) — not read.
- `internal/voicechild/keys.go`, `open.go`, `clips.go`/clip render worker (`pipeline.go:404-445` fields only), `stt_whisper.go`/`stt_deepgram.go`/`tts_openai.go` bodies (only constructors), `pseudostream.go`, `resample.go` (only the constructor), `echo_window.go`, `vad.go`/`turn.go` — not read.
- `internal/signalleg/inbound.go`, `msgchannel.go`, `voiceattempt.go` — not read.
- `cmd/meept-voice/main.go` — only the factory wiring lines; `run()` body not read.
- `internal/daemon/voice_keys.go`, `voice_link_rpc.go`, `voice_local_rpc.go` — not read (keys.go matters for A4-3's exposure: it is the `SendRaw` writer).
- `internal/voiceframe/frame.go` beyond `ChunkBytes` (decode/encode) — not read.
