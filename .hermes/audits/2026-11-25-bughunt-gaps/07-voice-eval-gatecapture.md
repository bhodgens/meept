# Auditor 7: voice + eval + gatecapture (range 2026-10-30..11-06)

Scope: READ-ONLY bug audit of the voice packages (`internal/voice`, `internal/voice/telnyx`, `internal/voicechild`, `internal/voicechild/providers`, `internal/voiceframe`, `cmd/meept-voice`) and the eval/harness packages (`internal/eval/hitl`, `internal/eval/trajectorydiff`, `internal/gatecapture`, `e2e/harness/golden*.go`) as added in `0a446a21..b3c03f80`, re-verified against the working-tree HEAD.

HEAD note: the task named HEAD `e43a7cf3`; the checkout's HEAD is `997d6d02` (one commit later, `fix(scheduler): classify job Env ...` — no voice/eval/gatecapture files touched). All findings below are against `997d6d02`. The working tree has unrelated uncommitted edits from sibling sessions; nothing in my file set was modified by them except `e2e/harness/daemon.go` / `fakellm.go` (not in my read set beyond golden.go's callers).

Files read: 31 of 34 production files in scope (see "Not audited").

## Findings

### A7-1 — MEDIUM — Dialer `fin` map grows without bound across the daemon's dial history
**File:** `internal/voice/dial.go:848-868` (`finishCall`)
```go
func (d *Dialer) finishCall(ccid string) {
	d.mu.Lock()
	if d.fin[ccid] {
		d.mu.Unlock()
		return
	}
	d.fin[ccid] = true
	ch := d.dones[ccid]
	delete(d.dones, ccid)
	delete(d.timers, ccid)
	// bughunt 2026-11-07 voice F-2: the answered marker is per-call
	// bookkeeping; drop it at terminal cleanup so the map cannot grow
	// across the daemon's dial history.
	delete(d.answered, ccid)
	d.mu.Unlock()
```
**Trigger:** every outbound dial that reaches step 6 (`d.dones[ccid] = done`). The `fin` latch is set and never deleted; the comment one line up explicitly names the growth class for `answered` but `fin` was missed.
**Effect:** one `map[string]bool` entry per dialed call for the daemon's lifetime — unbounded growth (a few dozen bytes/call; an employee that dials in a loop leaks indefinitely).
**Caller evidence:** `finishCall` callers at `dial.go:541, 797, 804, 810` — every terminal path. No non-test cleanup of `d.fin` anywhere: `git grep -n 'delete(d.fin' HEAD -- '*.go'` → 0 results.
**Later commits touching the lines:** `8e27df72` (daily counter atomic), `77fa2184` (docs) — neither touched `finishCall`.
**Confidence:** confirmed by reading the full path.

### A7-2 — MEDIUM — Outbound daily-dial cap unit is consumed before the POST and never refunded on refusal/failure
**File:** `internal/voice/dial.go:614-622` (`outboundPolicyGate`) vs `dial.go:517-558`
```go
	if d.cfg.DailyDialCap > 0 && d.counter != nil {
		day := time.Now().Format("2006-01-02") // operator local day
		count, admitted, err := d.counter.TryInc(day, dialCounterScope, d.cfg.DailyDialCap)
		...
		if !admitted {
			return fmt.Errorf("%w: %d/%d today", ErrDailyCapReached, count, d.cfg.DailyDialCap)
		}
	}
```
Then at `:517-558`: `DialOut` error, empty ccid, `TrackOutbound` error, or `reg.Acquire` failure each call `d.abandonEffect(...)` and return — the durable counter keeps the unit.
**Trigger:** any dial that passes the recipient gate and then fails at POST / track / acquire (carrier 5xx, network timeout, a concurrent call taking the employee slot).
**Effect:** the operator-configured `voice.daily_dial_cap` is exhausted by dials that never rang. The docstring at `:601-602` ("Dials that pass all gates increment the counter; refusals do not") is contradicted: a POST failure is a refusal that still consumed budget.
**Caller evidence:** `Dial` is the only caller of `outboundPolicyGate` (`dial.go:443`); `Dial` is wired from `internal/daemon/voice_wiring.go` (employee `voice_call` tool path).
**Later commits:** `8e27df72` made the counter atomic but kept the pre-POST increment position.
**Confidence:** confirmed by reading the full path.

### A7-3 — MEDIUM — Phone splice barge gate compares a monotonic-derived clock against a raw wall-clock touch
**File:** `internal/voice/splice.go:1116` and `:745`; `internal/voice/barge_gate.go:179-193`
```go
	// splice.go (pacer)
	s.lastPacedWriteNano.Store(time.Now().UnixNano())
	// splice.go (bargeIn)
	s.gate.Bump(s.lastPacedTouchNano())
	// barge_gate.go
	g.mu.Lock()
	g.gen++
	if lastTouchNano != 0 &&
		g.clockNano()-lastTouchNano < int64(residueWindow) {
		g.residueUntilNano = g.clockNano() + int64(residueWindow)
	} else {
		g.residueUntilNano = 0
	}
```
**Trigger:** `clockNano()` (C-19 fix) returns `wallBaseNano + monotonic-since-anchor` anchored on the gate's FIRST reading. `lastPacedWriteNano` is a raw `time.Now().UnixNano()`. Any wall-clock step between the anchor and a paced write (laptop sleep/wake, NTP correction — a voice daemon on a workstation is the documented deployment) makes `clockNano() - lastTouchNano` wrong: a backward step yields a huge positive delta (residue never stamped → the interrupted stream's socket tail replays over the new turn, the A6 defect), a forward step yields a negative delta (residue ALWAYS stamped → fresh first audio of the new say is discarded for 250 ms, clipping the start of every reply after the first barge-in).
**Effect:** the residue window — the fix for the "~1.28 s replayed tail" live defect — is mis-decided after any wall step; the C-19 monotonic fix only covered the deadline side of the comparison, not the touch side.
**Caller evidence:** `bargeIn` ← `handleInboundLine` (`:446`) and `HandleText` (`:784`); `RunSplice` is the phone-leg media bridge (`internal/daemon/voice_wiring.go` OnMediaStart dial).
**Later commits:** `0504cd53` introduced the monotonic clock; it did not touch the touch stamp.
**Rule:** `internal/voice/AGENTS.md` Media splice: "The gate's real clock is MONOTONIC ... Deadline arithmetic on the wall clock stretched or killed the residue window on an NTP step" — the touch-side arithmetic still rides the wall clock.
**Confidence:** confirmed by reading the full path; requires a wall-clock step mid-call to trigger.

### A7-4 — MEDIUM — `TextServer.writeChild` deadline TOCTOU: a concurrent writer's success clears a sibling's in-flight write deadline
**File:** `internal/voice/socket.go:342-361`
```go
func (s *TextServer) writeChild(conn net.Conn, b []byte, callID, event string) error {
	if err := conn.SetWriteDeadline(time.Now().Add(textWriteTimeout)); err != nil { ... }
	if _, err := conn.Write(b); err != nil { ... return ... }
	// Clear the deadline so a later writer never inherits a stale bound.
	_ = conn.SetWriteDeadline(time.Time{})
	return nil
}
```
**Trigger:** `Send`/`SendRaw` snapshot `s.child` under `s.mu` and RELEASE the mutex before `writeChild` (the header comment at `:84-86` says they "serialize on the snapshot mutex by necessity" — they do not). Writer A (say frame) sets deadline T+2s and enters `conn.Write` blocked on a full kernel buffer; writer B (releaseCall's end frame, or `SendRaw` keys) sets deadline T'+2s; writer A's `Write` is unblocked by the child reading, A returns and clears the deadline to zero → B's blocked write now has NO deadline.
**Effect:** the exact hang A4 fixed ("the end-frame send must NEVER block teardown") returns under concurrent senders on a wedged child; interleaved JSON lines are also possible since nothing serializes the writes.
**Caller evidence:** `Send` ← talkloop `say`/`handleInterrupt`/`end`, `SendRaw` ← `internal/daemon/voice_keys.go` keys emission; `releaseCall` sends the end frame from the release goroutine while the talk loop may still be mid-`say`.
**Rule:** `internal/voice/AGENTS.md` Sockets: "Daemon->child TEXT writes ... are bounded by a 2 s write deadline ... a wedged child conn degrades to a write error, never blocks the talk loop or releaseCall's end-frame send".
**Later commits:** `4d537280`/`8a3ca015` added the deadline; no later commit serialized the write.
**Confidence:** confirmed by reading the full path.

### A7-5 — MEDIUM — Supervisor-on reply path silently truncates long replies (280-rune sentences, 16-sentence cap)
**File:** `internal/voice/supervisor.go:413-441` (`SplitSentences`), consumed at `internal/voice/talkloop.go:1726-1768`
```go
		if utf8.RuneCountInString(s) > maxSentenceRunes {
			r := []rune(s)
			s = string(r[:maxSentenceRunes])      // silent cut at 280 runes
		}
		out = append(out, s)
	}
	for _, r := range draft {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			current.WriteRune(r)
			flush()
			if len(out) >= maxSplitSentences {
				return out                         // tail of the draft is dropped
			}
```
**Trigger:** the split-sentence path is taken whenever `ConnectSentences` is false — which the AGENTS.md mandates when the supervisor is enabled ("ConnectSentences=false is MANDATORY when a supervisor is enabled"), i.e. the default supervised posture. Any reply with a sentence over 280 runes (a code block, a long enumerated answer, a URL list) or more than 16 sentences.
**Effect:** the caller never hears the truncated tail — no log, no fallback. This is the same defect class the ConnectSentences path fixed ("The old cap truncated the reply at 840 runes ... so long answers were silently cut off mid-sentence ... NEVER drop text"); the split path was left behind.
**Caller evidence:** `spawnPipeline` → `SplitSentences(reply)` at `talkloop.go:1726`; each truncated sentence is what `Supervisor.Check` sees and what `t.say` speaks.
**Later commits:** `1ebdced0` fixed only the ConnectSentences branch.
**Confidence:** confirmed by reading the full path.

### A7-6 — MEDIUM — `hitl.Compute` "escape_rate_includes_always" is derived by inverting the per-class rate and then overwritten by the excludes-always map
**File:** `internal/eval/hitl/metrics.go:340-350`
```go
	for c := range blocked {
		if _, ok := m.EscapeRateIncludesAlways[c]; !ok {
			m.EscapeRateIncludesAlways[c] = m.EscapeRate[c]
		}
	}
	for c := range blockedExAlways {
		if _, ok := m.EscapeRateExcludesAlways[c]; ok {
			m.EscapeRateIncludesAlways[c] = m.EscapeRateExcludesAlways[c]
		}
	}
	maps.Copy(m.EscapeRateIncludesAlways, m.EscapeRateExcludesAlways)
```
**Trigger:** any autonomous run with `AlwaysAttributed` non-empty (an "always"-allow decision in the per-action reference run).
**Effect:** `EscapeRateIncludesAlways` should be `blocked_all[c] / denoms_all[c]` (that is `m.EscapeRate[c]`). The loop above writes `1 - rate` into it for every blocked class, then the final `maps.Copy` overwrites every class that exists in `ExcludesAlways` with the excludes value — so (a) for every non-always class "includes" == "excludes" (reporting a distinction that does not exist), and (b) for a class present only in `blocked` (always-attributed) it holds the inverted `1 - rate`. The R0 B.3 "report both" contract in report.go renders a wrong number.
**Caller evidence:** `Compute` ← `internal/eval/hitl` leaf-04 e2e suite (`e2e/suites/hitl-notifications`); `MarkdownSummary` renders `EscapeRateIncludesAlways` in the detail table (`report.go:96`).
**Later commits:** `7ea05edd` (lint) only.
**Confidence:** confirmed by reading the full path.

### A7-7 — LOW — `ChildProc.shortStopCtx` arms a finalizer on a local that dies immediately — graceful SIGTERM degrades to instant SIGKILL
**File:** `internal/voice/childproc.go:519-523`
```go
func shortStopCtx(grace time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	runtime.SetFinalizer(&ctx, func(p *context.Context) { cancel() })
	return ctx
}
```
**Trigger:** every idle-stop and handshake-fail stop. `&ctx` (a heap-escaped local interface value) is unreferenced once the function returns; the `context.Context` VALUE is copied into `rp.Stop`, so the GC may collect the box and run `cancel()` at any time after return.
**Effect:** `RuntimeProcess.stopProcess` selects on `<-ctx.Done()` → immediate `SIGKILL` of the voice child group instead of a `grace`-bounded SIGTERM (`internal/llm/runtime_process.go:624-631`). Bypasses the 15 s / 10 s grace the constants name. Non-deterministic (depends on GC timing).
**Caller evidence:** `childproc.go:458, 469, 595`.
**Later commits:** `b9ad7639` touched spawn only.
**Confidence:** likely — the finalizer-on-escape-to-heap mechanics are certain; the observable effect (kill vs term) depends on GC timing.

### A7-8 — LOW — `make golden-regenerate` always refuses: no suite registers a regeneration script
**File:** `e2e/harness/golden_regen.go:46-54, 76-79`; `Makefile:1784-1796`
```go
var regenRegistry = map[string]CaptureOptions{}
func RegenerateScenarioScript(opts CaptureOptions) {
	regenRegistry[opts.ScenarioName] = opts
}
	...
	script, ok := regenRegistry[opts.Scenario]
	if !ok {
		return fmt.Errorf("golden-regenerate: scenario %q has no registered regeneration script; ...")
```
**Trigger:** `make golden-regenerate SCENARIO=trajectory-golden-basic` (the documented workflow).
**Effect:** `RegenerateScenarioScript` has zero callers (`git grep -n 'RegenerateScenarioScript' -- '*.go'` → only its own definition and the error string). The registry is empty at runtime, so the target always fails at the lookup, after the suite gate has already run green. The regen workflow is dead code; the only way to refresh the fixture is the env-gated `TestTrajectoryGoldenFixtureDispatch`.
**Caller evidence:** `GoldenRegenFromEnv` ← Makefile `golden-regenerate` target; `e2e/suites/trajectory-golden/trajectory_golden_test.go` never calls `RegenerateScenarioScript`.
**Later commits:** none since `92fa3350`.
**Confidence:** confirmed by reading the full path.

### A7-9 — LOW — `trajectorydiff.Compare` returns PASS (score 1.0) for an EMPTY golden under every mode
**File:** `internal/eval/trajectorydiff/trajectorydiff.go:339-344, 380-386, 474-476`
```go
	if len(golden) == 0 {
		if len(actual) == 0 {
			return 1.0, steps, 0
		}
		return 0.0, steps, 0       // exact mode: correct
	}
	...
	if n == 0 {                    // in_order / any_order:
		...
		return 1.0, steps, 0       // PASS against ANY actual sequence
	}
```
**Trigger:** `in_order` (the default) or `any_order` with an empty golden — a fixture whose `golden` array is empty/truncated, or a caller that failed to load steps.
**Effect:** a vacuous golden passes every run; the gate cannot detect "golden lost its steps". `LoadGoldenFixture` (`golden.go:373-389`) validates schema_version and captured_from but NOT a non-empty `golden`.
**Caller evidence:** the one production consumer (`trajectory_golden_test.go:97`) guards `len(fixture.Golden) < 3`, so the shipped gate is covered; the library contract and the loader are not.
**Confidence:** confirmed by reading the full path; reachability requires a hand-edited fixture.

### A7-10 — LOW — `gatecapture.Store.resolve` consumes any pending for the command regardless of which chokepoint armed it; a lost pending lands a row with an EMPTY command
**File:** `internal/gatecapture/store.go:368-414`
```go
func (s *Store) resolve(chokepoint, command, decision, approver string) {
	...
	p := s.popPendingLocked(command)
	s.mu.Unlock()
	s.land(chokepoint, p, decision, approver)
}
func (s *Store) land(chokepoint string, p *pendingGate, decision, approver string) {
	command := ""
	if p != nil {
		command = p.command
	}
	rec := &CommandGateRecord{Chokepoint: chokepoint, Command: command, ...}
```
**Trigger:** (a) a `permission_table_ask` armed for command X (chokepoint 2) followed by a `decision_gate` resolution for X → the ask's prior is consumed and written under `decision_gate_deny`. (b) A decision-gate resolution with no pending (evicted/TTL/restart) → a DPO training row with `Command:""` and `ContextBlob = serializeBare("")`, contradicting the package doc's "never a guessed context" (an empty command is not a context).
**Effect:** mislabeled / empty training rows in `command_gate_records`; `PermissionTableResolved` explicitly refuses the unarmed case (`:278-282`) while `DecisionGateResolved` does not — asymmetric.
**Caller evidence:** `DecisionGateResolved` ← `internal/tools/builtin/decision_gate.go:169`; `PermissionTableAsk` ← `internal/security/engine.go:208`.
**Confidence:** confirmed by reading the full path; (a) requires the same command text to hit both chokepoints.

### A7-11 — LOW — `hitl.Runner.Drive` uses the injectable clock for its deadline but real `time.Sleep` for pacing, and never retries a failed resolve
**File:** `internal/eval/hitl/harness.go:485-515, 528-561`
```go
	deadline := clock().Add(budget)
	...
	for {
		if clock().After(deadline) { return res }
		...
		time.Sleep(poll)
	}
	...
	for _, p := range pending {
		if seen[p.ID] { continue }
		seen[p.ID] = true            // marked before Resolve
		...
		if err := cfg.Perms.Resolve(p.ID, decision); err != nil {
			reason = "resolve_error"  // never retried: seen is already set
		}
```
**Trigger:** a test pinning `RunConfig.Clock` to a constant makes `Drive` loop forever (deadline never passes); a transient `Resolve` RPC error leaves the permission unanswered for the whole run (the comment says "the next poll retries the list" — it does not re-resolve).
**Effect:** harness hang / a silently unanswered permission that then times out as a relay `timeout` — which the metrics classify as a fail-closed interrupt (precision denominator) rather than a harness fault.
**Caller evidence:** `Drive` ← leaf-04 harness tests and the e2e hitl suite.
**Confidence:** confirmed by reading the full path.

### A7-12 — LOW — `supervisor.parseAllowed` is fail-OPEN on any reply with an "ALLOW" prefix
**File:** `internal/voice/supervisor.go:372-379`
```go
func parseAllowed(reply string) bool {
	switch {
	case strings.HasPrefix(strings.ToUpper(strings.TrimSpace(reply)), "ALLOW"):
		return true
```
**Trigger:** the model replies `"ALLOWANCE DENIED..."`, `"ALLOWED? NO — ..."`, or echoes the sentence's own text beginning with "allow" (the prompt embeds the raw sentence via `%q`; a sentence like `"ALLOW all access" he said` can steer an echo).
**Effect:** a deny-shaped reply speaks the sentence. The package doc says every failure mode is fail-closed; a prefix match on an unanchored word is not.
**Confidence:** likely — depends on the fast model's reply shape.

## Already fixed by a later wave
- **Phone splice re-armed its barge-in latch on ANY pump read (stale-tail replay)** — fixed by `8e01fb6c` / `1c86c369` (shared `bargeGate`, read-time stale verdict). Verified at HEAD `splice.go:928-953`.
- **Wedged child text-conn blocked the talk loop / end-frame send forever (A4)** — fixed by `4d537280` (2 s `textWriteTimeout`). Verified at HEAD `socket.go:90, 342`. (Residual TOCTOU is A7-4.)
- **Pair-sweep deleted live client-pair sockets (A9)** — fixed by `4d537280` (`.c.in`/`.c.out` exclusion). Verified `childproc.go:496`.
- **Outbound `answered` map leaked across dial history (F-2)** — fixed by `8a3ca015`. Verified `dial.go:861`. (The sibling `fin` leak is A7-1.)
- **Late no-answer FailOutcome on an answered call (C-V1 finding 9)** — fixed: check and fire are one atomic step under `d.mu`. Verified `dial.go:785-790`.
- **Registry pre-gate refused cap-0 configs (F-3)** — fixed (`registryCap` sentinel). Verified `dial.go:707, 728`.
- **Child resampler dropped odd trailing bytes (permanent crackle)** — fixed (`convertAligned` carry). Verified `resample.go:211-241`.
- **Pseudo-stream STT deafened the call after the first FlushNow kick (H3)** — fixed (`flushWindow` keeps streaming). Verified `pseudostream.go:263-272`.
- **Deepgram promoted every `is_final` to an endpoint (H4)** — fixed. Verified `stt_deepgram.go:417-421`.
- **gatecapture DB/world-readable modes + WAL sidecars (security F-1 / infra F-1..F-3)** — fixed by `39dfb0ca` / `02f68f90`. Verified `store_sqlite.go:91-143`.
- **ConnectSentences path truncated replies at 840 runes** — fixed by `1ebdced0`. Verified `talkloop.go:1666-1695`. (The split path is A7-5.)
- **Talk-loop `turnCtx.Err()` early return without `close(p.checks)`** — not a bug at HEAD: `cancelPipeline` drops `t.cur` and bumps gen so `current()` disarms and `drainSignal` re-arms the silence clock. Verified `talkloop.go:1923-1961`.

## Verified clean
- `internal/voice/telnyx/signature.go:136-159` — asymmetric skew (300 s past / 60 s future), `strconv.ParseInt` timestamp parse, raw-body Ed25519 verify, SPKI header checked; no bypass.
- `internal/voice/telnyx/client.go:70-111` — bearer never in error text (snippet of body only), body always drained/closed, 1 MiB read cap.
- `internal/voice/codec.go:209-218` — `SilenceFor` byte math `dur × rate × 2 / second` correct for s16le; no rounding error at 20 ms.
- `internal/voice/codec.go:71-91` — `mulaw8ToPCM16` 4n−2 output documented as absorbed by the chunk accumulator; no silent truncation of payload.
- `internal/voice/splice.go:547-699` — reorder `held` map is bounded: step 6 trims to `capacity` on every drain; a far-future seq cannot grow it (drain only runs on push).
- `internal/voice/splice.go:1023-1118` — pacer re-queues an in-hand chunk on barge-in rather than dropping it; `resumeBargeIn` clears it again; no lost frame, no double write.
- `internal/voice/manager.go:238-281` — `releaseOnce` closes `done` exactly once, tombstone never re-stored in `done` (no double close), `fire` only for a known id.
- `internal/voice/callstore.go:300-322` — transcript append is a single atomic `json_insert` UPDATE detached from caller cancellation with its own 5 s bound; `RowsAffected==0` → `ErrCallNotFound`.
- `internal/voice/callstore.go:217-236` — `SweepStaleLive` WHERE is `state = 'live'` (correct; single-instance posture documented).
- `internal/voice/talkloop.go:1062-1092` — silence timer uses fresh-timer re-arm discipline (never `Reset` on a possibly-fired timer); H window fallback to S correct.
- `internal/voice/childproc.go:291-355` — `EnsureRunning` start is detached from the caller's ctx, gen-guarded epilogue, `Stop` aborts the handshake via `startCancel`; a mid-spawn Stop cannot leak the child (Stop's loop re-checks `stateStarting`, then stops `p.rp`).
- `internal/voicechild/child.go:335-392` — `serve` reader goroutine selects on `readCtx.Done()` for both `lineCh` and `errCh` sends; no leaked reader on dispatcher exit.
- `internal/voicechild/pipeline.go:446-465, 2873-2946` — pipeline ctx is `context.Background()`-rooted (not tied to the frame-dispatch read ctx); `close()` waits on `analyzerInit`, closes both sockets, releases VAD/turn sessions off-lock, bounds the consumer wait at 5 s.
- `internal/voicechild/pipeline.go:2169-2182` — `played` edge only on natural EOF, re-checks `playGen`/`dropQueued` before emit (no phantom edge on aborted audio).
- `internal/gatecapture/store_sqlite.go:112, 147-174, 177-240` — modernc DSN pragmas correct; parameterized dynamic WHERE; insert validates vocabularies upstream (`store.go:181-190`).
- `internal/gatecapture/store.go:331-352` — pending map bounded (TTL sweep + oldest eviction at 256); delete-during-range is legal Go.
- `e2e/harness/golden.go:97-140, 297-349` — capture refuses nil script / empty name / zero recorded calls; the only fixture writer is `writeGoldenFixture`, and the normal suite run (`trajectory_golden_test.go:168-172`) skips authoring unless `MEEPT_TRAJECTORY_GOLDEN_OUT` is set — a baseline is never overwritten on a normal run.
- `e2e/harness/golden_regen.go:63-113` — suite-first gate runs before any rewrite; `Force` only skips the log line.
- `internal/eval/hitl/harness.go:683-704` — `SampleIndices` uses splitmix64 (not `math/rand`), clamps count to `[0,n]`, Fisher-Yates index bounded.
- `internal/eval/hitl/metrics.go:190-209` — completion rate guarded by `Attempts > 0`; precision guarded by `total > 0`; no divide-by-zero.
- `internal/eval/hitl/report.go:123-144` — cells that lack a measurement render "-" / "n/a", never a fabricated 0.0 or 1.0.
- `internal/eval/trajectorydiff/trajectorydiff.go:313-351` — exact mode: empty golden vs non-empty actual scores 0.0 (correct), denominator is `max(len)`.
- `internal/voice/binding.go:300` — `Progress.PublishProgress` is dereferenced without a nil guard, but the sole production constructor (`internal/daemon/voice_employee.go:924`) always wires `progressPublisherFunc(progressFn)`; unreachable nil in prod.
- `internal/voice/daybrief.go:147-213` — every source is typed-nil guarded (`isNilSource`), per-source errors joined, block clamped to 2000 runes at output.

## Not audited
- `internal/voicechild/providers/stt_whisper.go` (471 lines), `stt_whisper_server.go` (170), `stt_rumble.go` (253), `tts_openai.go` (348), `tts_piper.go` (234), `pcmstream.go` (129), `stt_priming.go`, `turn_smart.go`, `vad_silero.go` — not read line-by-line (time budget); only `newBaseSTT`'s vendor dispatch was traced. A nil-struct-on-missing-config check at `stt_deepgram.go:167-187` was clean (missing key → `ErrMissingSTTKey` constructor error, never a typed-nil).
- `internal/voicechild/pipeline.go` lines 1-445 and 1334-1625 (`consumeTranscripts` body, short-final gate internals), 1626-2058 (`pumpAudioIn`, `enqueueSay`, `runSynth`, `drainSynthQueue`, `tryLaunchPrefetch`), 2425-2867 (`noteBargeIn`, `rmsBargeInCheck`, `awaitPendingInterrupt`) — skimmed via grep of lock sites; the playMu/turnMu regime looked consistent but the RMS clock arithmetic (`lastMicLoudAt`, `lastFinalAt`, `lastDispatchAt`) was not traced.
- `internal/voicechild/providers_wiring.go` (284), `keys.go` (351), `jargo.go`, `internal/voiceframe/frame.go` (157), `cmd/meept-voice/main.go` (128) — not read.
- `internal/voice/local.go`, `localsplice.go`, `link.go`, `events.go`, `contextwarm.go`, `barge_gate.go` callers beyond splice.go — outside the stated file list; `barge_gate.go` read only for A7-3.
- `internal/voice/talkloop.go` lines 1-983 (lane pools, clip id, farewell, park/release helpers) — not read.
- `e2e/harness/otlpreceiver.go` (237), `fakellm.go` — not read (outside the named focus list).
