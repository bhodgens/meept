# Bughunt wave 2026-11-19b — consolidated report

Repo: /Users/caimlas/git/meept. Window: unaudited tail (9aeadb0c..b37bc202, 79
commits) + three gap areas. READ-ONLY. HEAD at write: 202c0994 (a sibling fix
wave landed mid-audit; every finding below re-verified at that HEAD).

## Provenance warning (read first)
All three delegated auditors failed to produce usable independent coverage:
- sa-0 (activity lane): finished with a grep dump, zero findings, and a FALSE
  central claim (lane uses string topics — it uses typed bus.Topic[T];
  execution_projector.go:44-50).
- sa-1 (TUI/GUI): died on context overflow at 38 min; report survived.
- sa-2 (Rumble): hung in a self-echo grep loop for 45 min; stopped by parent.
Parent re-ran all three lanes. Every finding in this report is
PARENT-VERIFIED — this is NOT the independent-corroboration the wave rules
ask for. The three parent takeover files carry the detail:
.hermes/audits/wave-2026-11-19b/{04,05,06}-*.md. Child reports 01/02/03 are
superseded, kept for audit trail.

## Baseline gates
- go build ./... — clean.
- go test -p 2 -count=1 ./... — 158 ok, 2 FAIL (internal/signal
  TestSupervisorRestartOnCrash, internal/worker TestWorker_RetryWakesIdleWorker);
  both 3/3 green isolated → timing-fragile tests, not regressions.
- e2e manifest gate — GREEN at 202c0994 (fixed by 5e21013b; was RED at 7a5da96e).
- go test -p 2 ./internal/voicechild/providers/ — ok 10.99s (rumble native WS
  gates green).

## NEW findings this wave (all parent-verified at file:line)

### P1 (MED) internal/tui/events.go:133-147,306-320 — fast Stop->Start doubles the poll loop
Stop() cannot retract an already-armed one-shot tea.Tick. Start() within one
500 ms interval leaves the old tick pending; it then sees running==true and
re-arms alongside the new chain. Sidebar enter/exit (sidebar.go:285,:294)
toggles faster than one interval → poll rate multiplies per toggle cycle.
Fix: generation counter stamped by Start, carried in EventStreamTickMsg,
mismatched generations dropped. Untouched by the fix wave (last touched
d6327538).

### P2 (LOW-MED) internal/tui/events.go:222-272 — concurrent Polls duplicate rows
No in-flight guard in Poll, no dedup in addToBuffer (:274-280). Two chains (P1)
or a slow RPC plus a tick can deliver the same page twice → duplicated rows in
the sidebar recent-events ring. Falsifiable: two concurrent Polls over a mocked
slow RPC; assert no topic+seq duplicate pair.

### P3 (LOW) internal/tui/agents_panel.go:941-948 — live run row blinks on next poll
executionResultMsg prepends the live frame; executionRunsMsg replaces p.runs
with the server page. Projector lag → the row vanishes for one poll, reappears.
Cosmetic; fix by carrying rows newer than the server max Timestamp.

### R1 (LOW) docs/research/rumble2flash-review.md contradicts the shipped adapter
The review says Rumble-2-Flash cannot stream (true of HF card + stock vLLM).
The shipped adapter talks to a custom server whose partial/final/end protocol
was verified live (master.md:27,:74). Plan master.md itself flags the stale
text (:283-284) but the review doc was never amended. Doc fix only.

### A1 (LOW) internal/comm/http/execution_handlers.go:44,80 — client faults answered 500
Every RPC error maps to 500; a malformed ?since= should be 400 (RPC says so at
execution_runs_rpc.go:47-51). Same shape pre-exists in activity_handlers.go —
pattern-level.

### A2 (LOW) projector lag can drop history rows with no consumer-visible signal
Append (INSERT+DELETE) runs in the bus subscriber goroutine
(execution_projector.go:42-51). Bus drops on full channel are counted
(bus.go:52-57 DroppedCount) but never surfaced via agents.executionRuns.
Accepted posture at current volume; note the blind spot.

### O1 (observation, pattern) early-adapter-exit idles the pcm feeder
rumble/deepgram/whisper run() return on end/error while the feeder blocks
sending into pcm until ITS ctx cancels. Pre-existing across adapters; needs a
feeder-side trace before judging severity.

## Prior-wave HIGH re-adjudication at 202c0994
- H1 STILL OPEN: [voice.rumble_runtime] is parsed + normalized
  (schema.go:6487) but VoiceRumbleRuntimeConfig has ZERO production consumers;
  internal/daemon/rumble_wiring.go has no spawn call site. Daemon never
  supervises vLLM Rumble; the live socrates:8000 server is operator-started.
- H2 FIXED (1cee89d7): whisper uses --prompt (stt_whisper.go:413-418).
- H3 FIXED (1cee89d7): pseudostream FlushNow is flush-and-keep-streaming
  (pseudostream.go:263-272, cap 320 KB :53-58).
- H4 FIXED (1cee89d7): Transcript.Endpoint (pipeline.go:49-54),
  utterance_end_ms query (stt_deepgram.go:317-318), gate test present.
- H5 FIXED (202c0994): GUI rename persistence (sessions_list.dart + provider).
- H6 FIXED (5e21013b): manifest completeness test green (ran it).

## Dead/degraded-seam census (this wave's scope)
- stt_base_url: RESOLVED wired (keys.go:270 -> providers_wiring.go:227 ->
  newRumbleSTT gate stt_rumble.go:74-76).
- rumble_runtime config block: PARSED-NOT-CONSUMED (see H1).
- execution store/RPC/HTTP/TUI/GUI surface: fully wired end to end.
- TUI runs sub-view: wired (d6327538) with P1-P3 above.

## Recommended fix order
1. H1 — wire the sidecar spawn (or delete the config block + wiring file and
   re-scope honestly; a parsed-but-ignored GPU cap config is a trap).
2. P1 — generation guard in events.go Update/Start (small, closes P2's amplifier).
3. A1 — 400 vs 500 for client-fault RPC errors in the two HTTP bridges.
4. R1 — one dated addendum line in the research doc.
5. P3/A2/O1 — backlog; document or fix opportunistically.
