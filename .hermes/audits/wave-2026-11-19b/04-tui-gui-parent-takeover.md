# TUI/GUI agents-runs lane — PARENT-VERIFIED takeover

Provenance: child sa-1-e196ecbc died at ~38 min on context overflow. Its report file
(02-tui-gui-runs.md, 7.5 KB) landed nearly complete. This file is the parent's
adjudication of its claims + the parent's own re-audit of the lane. PROVENANCE =
parent-verified, NOT independent (wave rule: a result the parent finished itself is
coverage with different provenance).

## Adjudication of child claims (parent re-read every cited line at HEAD 7a5da96e)

### REJECTED — F1 (HIGH, "leaked timer, never cancelled")
mechanism wrong. tea.Tick is ONE-SHOT, not self-perpetuating. The chain is:
tick fires -> executionRunsTickMsg -> `if p.subView != agentsViewRuns { return nil }`
(agents_panel.go:958-962) so the chain DIES when the user leaves the runs sub-view.
Re-arm lives in executionRunsMsg (guarded `if p.subView == agentsViewRuns`).
No leak. False alarm.

### REJECTED — F3 (HIGH, "Start() twice creates two polling loops")
Start() (events.go:133-138) guards: `if es.running { return nil }`. A second Start
while running arms nothing. Claim as written is impossible. The REAL variant of this
class is P1 below.

### REJECTED (demoted to nit) — F2/F7 (duplicates of each other, header wording)
TUI header says "polled every 5s; capped at 30 rows", GUI says "survives reconnect".
Text emphasis is not a capability gap; both surfaces show the same durable history.
Cosmetic, not a parity deviation under the AGENTS.md rule (rule covers features/keys).

### DEMOTED — F4 (MED, GUI timer "keeps running away from panel")
Design choice, not a leak. ExecutionRunsNotifier owns the timer at provider level;
dispose() sets _disposed (line 101, 196-197) and every async continuation checks it
(lines 108-162). Polling the daemon's SQLite at 5 s from a resident provider is
intended freshness. NOT a defect.

### DEMOTED — F5 (LOW) cursor conditional preservation at agents_panel.go:607-615
matches the convention (clamp-or-top on refresh). Cosmetic. Accepted as LOW/no-fix.

### CONFIRMED (child correct) — prior-wave M10 is FIXED at HEAD
renderDetail (agents_panel.go:1520-1529) now guards `detail == nil && p.err != nil`
-> renderDetailError(); the inline comment cites M10 explicitly. Child's checked-clean
list claim verified at file:line.

## Parent-added findings (this lane, at HEAD 7a5da96e)

### P1 (MED) internal/tui/events.go:133-147 + 306-327 — Stop->Start within one poll
interval permanently DOUBLES the poll loop (the real version of the child's F3 class).
Mechanism: Stop() sets running=false but cannot retract an already-armed one-shot
tea.Tick. If Start() is called again before that pending tick fires (interval 500 ms),
the old tick then sees running==TRUE, returns tea.Batch(Poll, schedulePoll), and
re-arms — while the new Start chain is also armed. Each fast Stop/Start cycle adds a
chain; chains only die on a running==false window longer than one interval.
Repro: Start(); Stop(); Start(); wait 3 intervals; count Poll RPCs (want 1/interval,
observe >=2). Trigger path exists in production: sidebar.go:285 Start / :294 Stop on
panel enter/exit — toggling the sidebar faster than 500 ms multiplies polls.
Fix shape: generation counter stamped by Start, carried in EventStreamTickMsg; Update
drops ticks whose generation != current.

### P2 (LOW-MED) internal/tui/events.go:222-272 — concurrent Polls duplicate buffer rows
Poll has no in-flight guard and addToBuffer (:274-280) has no dedup. Two chains (P1)
or a slow RPC plus a tick can run two polls with the SAME lastSeq; both append the
same events -> the sidebar recent-events list shows duplicates. Falsifiable test:
two concurrent es.Poll() cmds with a mocked slow RPC returning the same page; assert
RecentEvents has no topic+seq duplicate pair.

### P3 (LOW) internal/tui/agents_panel.go:941-948 — live frame blinks out on next poll
executionResultMsg prepends the live frame (dedup: none); executionRunsMsg REPLACES
p.runs with the server page. If the durable projector lags the 5 s poll, the just-
prepended run vanishes for one poll and reappears. Cosmetic flicker; self-heals.
Fix shape: union by (session_id, timestamp) when merging, or carry rows newer than
server max-Timestamp across polls.

## Checked clean (parent-verified this turn)
- tableutil: runs table goes through tableutil.SetRows (agents_panel.go:607).
- GUI lifecycle: provider dispose + _disposed guards; agents_tab.dart:101-105 cancels
  _quotaTimer; agent_activity_widgets.dart:173-176 cancels _phaseTimer.
- go build ./internal/tui/... + go vet clean (child ran; matches my own build ./...).
- WS classification unchanged by b37bc202 (ws_schema_drift_test.go landed with it).

## gates_run:
- Parent: go build ./... (clean, full tree), baseline full-suite already logged.
- No targeted -run tests executed for this lane (bubbletea cmd-chain repro needs an
  interactive harness; repro shape documented instead).
