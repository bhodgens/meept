# L08 — Spill Retention Sweep (P7)

**Proposal:** P7. **Depends on:** L07 (spill service + config keys). **Files (2):**
- Create: `internal/llm/spill_retention.go` (sweep) — or add to spill.go if it stays <100 lines
- Test: `internal/llm/spill_retention_test.go`

**Problem:** L07 spills raw tool outputs to `$MEEPT_HOME/spill/<session>/`.
Without retention, this directory grows unbounded. Owner requirement: **30-day
default retention, set in meept** (config `spill.retention_days`, defined in L07).

**Contract:** sweep deletes spill FILES older than retention_days (mtime-based;
dirs removed when empty). Runs on daemon startup + every 24h. Must never
 traverse above `$MEEPT_HOME/spill`. Disabled entirely when
`spill.retention_days <= 0` (0/negative = keep forever).

### Tasks

**Task 1 — failing tests:** (i) `TestRetentionSweepDeletesOldFiles`: create
spill files with mtimes 40d and 1d ago, retention 30 → only the 40d file gone,
empty parent dir removed, 1d file intact. (ii) `TestRetentionDisabledKeepsAll`:
retention 0 → nothing deleted. (iii) `TestRetentionNeverEscapesSpillRoot`:
populate a decoy dir outside the spill root with an old file → untouched.
Run: `go test -p 2 ./internal/llm/ -run TestRetention` — FAIL.

**Task 2 — implement:** `SweepSpill(ctx, cfg) (removed int, err error)`; walk
`$MEEPT_HOME/spill` only; os.Stat mtime; remove file then empty parent dirs.
Wire invocation: daemon startup path (same place other periodic sweeps run —
search_files for the startup sweep pattern in internal/daemon, e.g. orphan
sweep) + a 24h ticker. Follow the repo's background-processor patterns
(hierarchical-planning references/go-background-processor-leaves.md): context
shutdown stops the ticker.

**Task 3 — full package:** `go test -p 2 ./internal/llm/ ./internal/daemon/` green.

**Task 4 — report.** Do NOT commit.

## Self-Verification Checklist
- [ ] Never deletes outside $MEEPT_HOME/spill (decoy test proves it)
- [ ] retention_days<=0 disables; default 30 from schema
- [ ] Ticker stops on context cancel; no goroutine leak

## Review Checklist (for orchestrator)
- [ ] Startup sweep cannot block daemon readiness (runs async or after healthy)
- [ ] Removals logged at debug with counts
