# Voice regression audit — commits after 2026-11-06 bughunt wave

- Scope: voice portion of `6b42213b..7b510ed4` (bdbe43f5, 157ab585, 8a586451, 3bf5431c, 38a7d682, ad014475, 4c52c179, 032fde86, 7b510ed4)
- HEAD audited: `7b510ed4ffb2374d8609ade9ce569ef0e17ba49f` (branch main)
- Audit window: started 2026-11-07T00:05:48Z, finalized 2026-11-07T00:48Z (UTC)
- Method: every quoted line verified against committed state via `git show HEAD:<path>` (working tree carries sibling WIP; only paths confirmed clean-vs-HEAD were read directly). `go build`/`go vet` clean on `./internal/voice/... ./cmd/meept-voice/`; `go test -count=1 ./internal/voice/` → ok (33.4s).

## Findings

### F-1 (HIGH) — Barge-in latch never resets: the agent is permanently muted on a call after its first interrupt
- `internal/voice/splice.go:690` and `:732` set the latch; `:892` and `:941` gate all outbound writes on it. No `s.interrupt = false` exists anywhere in the package.

  Evidence (HEAD):
  ```go
  // splice.go:689-690 (bargeIn)
      s.cancelSent = true
      s.interrupt = true
  // splice.go:891-898 (paceOne)
      s.mu.Lock()
      stopped := s.interrupt
      s.mu.Unlock()
      if stopped {
          // Barge-in active: idle-wait instead of writing ...
  ```
  `grep -rn 'interrupt = false' internal/voice/ internal/daemon/ --include='*.go'` (non-test) → zero hits. `cancelSent` is likewise latched once per splice (`:139` "fired exactly once per interrupt" — but it is never re-armed, so it is once per CALL).
  The child-side twin has the same defect: `internal/voicechild/pipeline.go:223` `p.dropQueued = true` (set by `clear()`), read at `:144` (enqueueSay silent drop) and `:167` (playback early return); `grep 'dropQueued = false'` → zero hits.
- Why it's a bug: after the first barge-in, the splice's outbound loop idle-waits forever and the child silently drops every subsequent say, so the caller never hears another word for the rest of the call while the conversation logic keeps running (prompts, turns, supervisor checks). This violates the AGENTS.md/docs contract "Barge-in is PER CALL: clear stops playback ... for THAT call_id only" + "Barge-in is kept end to end" (`docs/workflows/voice-sidecar.md:222`): stopping playback must be per barge-in, not per call. The e2e suite only asserts the `clear` frame is observed (`e2e/suites/voice-bargein/voice_bargein_test.go:50`) and never drives a post-barge-in say, which is why it ships green.
- Fix relation: the audited commits reworked exactly these functions — `4c52c179` edited `bargeIn()` (m1 error logging) and `032fde86` edited the barge-in/draft-cancel wiring — without addressing the never-resumed latch (incomplete-fix class; the latch itself predates the range from c265636d).

### F-2 (MEDIUM) — H1 answered-hook fix is incomplete: `timer.Stop()` cannot un-arm a no-answer fire already in flight, and the fired path strands an answered live call
- `internal/voice/dial.go:443-447` (timer branch of `armNoAnswerTimer`) vs `:462-471` (`NotifyAnswered`):

  Evidence (HEAD):
  ```go
  // dial.go:444-447
        case <-timer.C:
            if d.mgr != nil {
                d.mgr.FailOutcome(ccid, OutcomeNoAnswer, fmt.Sprintf("no answer within %ds", secs))
            }
  // dial.go:466-471 (NotifyAnswered)
        d.mu.Lock()
        timer := d.timers[ccid]
        d.mu.Unlock()
        if timer != nil {
            timer.Stop()
        }
  ```
  There is no answered-state flag guarded inside the `case <-timer.C` branch (unlike the M5 silence-timer fix in the same wave, which re-armed fresh timers to dodge exactly this stale-fire shape).
- Why it's a bug: `Timer.Stop` returns false when the value was already buffered; the fire then wins the select and `FailOutcome(OutcomeNoAnswer)` tears down a call that was answered in the same instant. The teardown path (releaseCall → loop cancel, registry release) has no carrier `HangupCall` POST for the no-answer case, so the answered caller is left on a dead bridge (registry slot already freed, one-live-call invariant effectively broken while the leg is still up). The window is narrow (answered-event webhook racing the expiry tick) and `3bf5431c` M8 deliberately aligned the local timer and the carrier `timeout_secs` on the same value, which maximizes the simultaneity that this Stop-only cancellation cannot cover. Commit `3bf5431c` H1 claims an answered call "keeps its slot instead of eating a late OutcomeNoAnswer release" — true only outside the fire-in-flight race.

### F-3 (MEDIUM) — `CodecWire.SamplingRate` is threaded through the 032fde86 wiring but consumed nowhere; the transcode path hardcodes rates per codec name
- `internal/daemon/voice_wiring.go:610-613` (the only writer):

  Evidence (HEAD):
  ```go
        if start.Encoding != "" {
            cfg.CodecWire.BidirectionalCodec = start.Encoding
            cfg.CodecWire.SamplingRate = start.SampleRate
        }
  ```
  `grep -rn 'CodecWire.SamplingRate\|cfg.CodecWire' internal/voice/ internal/daemon/ --include='*.go' | grep -v _test`: reads exist only for `.BidirectionalCodec` (splice.go:104, `wireCodecOf`); the sampled-rate field has zero readers. `internal/voice/codec.go:118,128` select the transcode path on the codec string alone (`if codec == "PCMU"`), with `mulaw8ToPCM16` (8k→16k, codec.go:71) and `silenceRate() = 16000` (splice.go:676) hardcoded.
- Why it's a bug: the commit's own docblock makes the start frame "the authority for what was negotiated" (AGENTS.md C2), yet an `L16@8000` negotiation — a valid Telnyx media_format (streaming is requested at 16000, but the code never checks the echo) — passes byte-identical through `transcodeInbound`/`transcodeOutbound` and gets chunked/paced as if it were 16 kHz: 640-byte "20 ms" chunks are 40 ms of real audio, double-speed playout outbound, and reorder-window silence fill sized for the wrong rate. A threaded-but-dead config field is precisely the "fix commits one seam, misses the consumer" shape; at minimum the mismatched-rate case must be detected and refused, not silently mis-resampled.

### F-4 (LOW) — The 4c52c179/032fde86 codec separation left AGENTS.md's C2 contract text contradicting the code
- `internal/voice/AGENTS.md` (Sockets C2 section): "Chunk size is codec-derived, 20 ms: L16@16k = 640 bytes ..., PCMU@8k = 160 bytes" and (Media splice): "chunk size follows the negotiated codec, never a fixed constant" — vs `internal/voice/splice.go:187`:
  ```go
  chunk, err := ChunkBytes("L16", 16000)
  ```
  (child chunks are now ALWAYS 640 by design; `4c52c179` commit message itself calls the codec-derived child sizing the H2 bug).
- Why it's a bug (governance, repo-binding): root AGENTS.md maintenance rule — "Update a rule and its summary (or its doc) in the same commit" — and both commits shipped no AGENTS.md/docs/workflows update (`git show --stat` on 4c52c179/032fde86: splice.go/tests, voice_wiring.go/components.go/tests only). The next agent following the invariant doc will "fix" the correct always-640 child sizing back to codec-derived and reintroduce the H2 corrupt-audio bug. The doc still states, as contract, the exact behavior the fix removed.

### F-5 (LOW) — 38a7d682's stray-`.staged` cleanup missed its sibling: a diverged 59-line stub of the voice-lifecycle fake is still tracked at HEAD
- `git ls-tree HEAD e2e/suites/voice-lifecycle/`:
  ```
  e2e/suites/voice-lifecycle/fake_telnyx_test.go.staged   (59 lines)
  e2e/suites/voice-lifecycle/fake_telnyx_test.go          (856 lines)
  ```
  `diff` of the two HEAD blobs shows they are NOT byte-identical (the .staged copy is a truncated remnant of the live file).
- Why it's a bug: 38a7d682 removed `registry_test.go.staged` "because it is residue" from the same c265636d re-graft, but this one survived. Unlike the registry case, this file diverges from its live twin — a future reader (or a tool globbing `fake_telnyx_test.go*`) can resurrect stale e2e semantics. Hygiene-only (the `.staged` suffix is outside all build/test globs), hence LOW.

### F-6 (LOW) — `routeFrame` overwrites an existing loop/cancel for a repeat setup frame, orphaning the old loop whose silence-timer hangup then kills the live call
- `internal/daemon/voice_wiring.go:419-428` (HEAD): on `voice.EventSetup` there is no `if _, exists := w.loops[f.CallID]; exists` guard; `w.loops[f.CallID] = loop` and `w.loopCancels[f.CallID] = loopCancel` silently replace the pair.
- Why it's a bug: the replaced loop keeps running (nobody cancels its ctx — its `loopCancel` closure is overwritten and lost), receives no further frames (router looks up by map), and after `SilencePromptSeconds` (default 45) its second silence fire calls `t.end → w.managerHangup(ccid, ...)` → `Manager.FailOutcome` on the CURRENT live call. The child's own `handleSetup` is explicitly idempotent ("setup for live call ignored", `internal/voicechild/child.go:278-282`) — the daemon-side twin lacks the same guard. 032fde86 introduced the second (cancel-bearing) map without adding the guard, extending a pre-existing overwrite hazard with a leaked cancel func. Trigger requires a duplicate setup on one ccid (child reconnect/replay), so exposure is narrow — LOW.

## Checked, no defect found (fixes verified sound)

- **157ab585 (0=unlimited sentinel)**: `dial.go:395` `cap > 0 && len(live) >= cap` and `registry.go`-half `Acquire` (`dial.go:551`) now agree; exhaustive grep of voice-path capacity comparisons (`registryCap`, `Cap()`, `max_concurrent`) found no remaining mis-handled sentinel. Callers of `Cap()`/`Live()` (childproc `Live` probe, `publishCallEnded`) are display/accounting only.
- **4c52c179 m2 (string-aware JSON split)**: `splice.go:429-464` traced against escaped-quote/backslash, quote-outside-object, stray-closer, and truncated-tail cases; byte-level scan is UTF-8 safe (ASCII delimiters cannot appear inside multi-byte sequences). Tests pin the cases. Correct.
- **ad014475 M5 (silence timer)**: fresh-timer-per-window re-arm (`talkloop.go:259-285`) orphans stale fires correctly; single-consumer select confirmed. M2 flush: `p.turnEnded` is only touched by the Run goroutine (both `playVerdict` and `flushTurnEnd` are called inline from the same select loop), so the unguarded field read is race-free; exactly-once `last:true` per draft holds on the denied-final, empty-reply, and fallback paths.
- **3bf5431c H3**: `failUntracked` (`manager.go:332-341`) + `releaseOnce` teardown is once-per-ccid on both interleavings (late carrier hangup webhook sees `fire=false`); spawn-segment registry slot acquired-then-failed is released via the hook; AnswerCall-failure path leaves no state to release. M7: `Acquire` normalizes at the boundary (`dial.go:545`) and gate scans compare canononically; `NormalizeE164` is idempotent on its own output. M8: `DialOut` signature change swept — all call sites updated (`dial.go:292`; remaining hits are test fakes).
- **032fde86 H4**: per-call cancel map is written under `loopsMu` in both arm (`voice_wiring.go:421-427`) and consume (`releaseCall`, `:522-544`) paths; double-cancel is idempotent; the Run-goroutine's `delete` after `releaseCall` already deleted is a no-op. M4 `spliceHangup`: carrier POST then `FailOutcome` — fan-out still exactly once via `releaseOnce` under either webhook ordering.
- **7b510ed4 (test race)**: the race was test-side only (slog.Logger serializes handler writes; the plain `bytes.Buffer` assertion was the sole unsynchronized reader). `lockedBuffer` (`childproc_testsupport_test.go:15-30`) mutexes Write/String; happens-before between log write and assertion is established via the mutexed `CancelDraftFn` counter (logged inside `bargeIn` before the callback). No production race hidden behind the test fix; no other unlocked `bytes.Buffer` log sinks remain in the package.
- **bdbe43f5 (restore wiring)**: `VoiceWiring.Start/Stop` present and symmetric (`components.go:5531-5546` start, `:6906-6915` stop in `stopComponents`); stop order child→text-server→store matches the documented dependency order; route arming restored (`daemon.go:1713-1724`). The rollback switch (`components.go:5355-5440`) lacks a `"voice"` case despite `startedHandlers` carrying `"voice"` (`:5545`) — verified non-exploiting: every rollback consumer (`daemon.go:2197-2201`) immediately calls `d.shutdown()` → `components.Stop` → the voice stop block, which cancels `stopCtx` and closes the listener, so the socket file + goroutine cannot outlive the failed boot. Noted here for completeness, not filed.
- **8a586451**: test-harness-only change (single-reader shape on `.out`), no production diff.
- Contract sweep (C1 byte-exact frames + call_id, last:true turn rule, supervisor gate before say, hangup fan-out, Telnyx token in-daemon, child keys over socket, jargo import pins): all held at HEAD; build/vet/`go test ./internal/voice/` green.

## Summary

| Severity | Count | Findings |
|----------|-------|----------|
| HIGH     | 1     | F-1 barge-in latch never resets (agent muted for call after first interrupt) |
| MEDIUM   | 2     | F-2 no-answer fire-in-flight strands answered call; F-3 CodecWire.SamplingRate threaded but unconsumed (rate-mismatch silent mis-resample) |
| LOW      | 3     | F-4 AGENTS.md C2 text contradicts post-fix splice code; F-5 stale diverged fake_telnyx_test.go.staged still tracked; F-6 repeat-setup orphans old loop + leaked cancel |

AUDIT COMPLETE — 6 findings. Every finding cites file:line verified against committed state at HEAD 7b510ed4 via `git show HEAD:<path>`.


## Closeout (2026-11-06, fix pass)
- F-1 CLOSED (HIGH): 6f8cc1a6 — splice re-arms on fresh child PCM (resumeBargeIn + ended latch); child clears dropQueued at each new say with playGen retirement. Pins: TestBargeInResumesOnNextUtterance, TestSayAfterClearPlaysAgain (both mutation-proven).
- F-2 CLOSED: 6f8cc1a6 — answered map guarded at fire time + arm-skip + finishCall cleanup. Pins: TestNoAnswerTimerRespectsAnsweredFlag (deterministic lock-hold race, mutation-proven), TestArmSkippedForAlreadyAnswered.
- F-3 CLOSED: 6f8cc1a6 — wire codec/rate mismatch refused at the splice-runner seam (L16!=16000, PCMU!=8000).
- F-4 CLOSED: 6f8cc1a6 — internal/voice/AGENTS.md C2/barge-in text now states the post-fix contract.
- F-5 CLOSED: 6f8cc1a6 — fake_telnyx_test.go.staged removed from git + disk.
- F-6 CLOSED: 6f8cc1a6 — routeFrame setup idempotence guard. Pin: TestRouteFrameDuplicateSetupIgnored (mutation-proven).
AUDIT CLOSED - 6/6.
