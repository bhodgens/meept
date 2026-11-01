# Taint Taxonomy + Hook Redesign - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.
> Do NOT register the hook in internal/daemon/components.go - that is
> leaf 02's job; this leaf changes internal/agent + internal/security/taint
> only.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Redesign `checkMessageTaint` to per-handoff (segment) taint
  decisions and add `TaintUntrusted` to the `AgentMessageSink` taxonomy.
- **Dependencies:** R0-research.md findings (decision IDs D1/D2 cited in
  dispatch addenda)
- **Estimated Context:** 70K
- **Concurrency Group:** B
- **Audit references:** proposal 5 (master.md R1/R2)

## Goal

Make the taint hook safe to wire: a clean, legitimate handoff passes;
a handoff whose untrusted segments carry injection/control content is
blocked. Today checkMessageTaint (internal/agent/taint_hooks.go:156-198)
labels the ENTIRE message `TaintUntrusted` unconditionally (:169), so the
hook as-is would block all cross-agent messages once wired.

## Context

- internal/agent/taint_hooks.go - TaintBeforeToolCall: BeforeToolCallHook
  dispatching to checkShellTaint/checkNetworkTaint/checkMessageTaint.
  Constructor :30 returns nil on nil tracker (keep that guard).
- internal/security/taint/taint.go - Tracker (MarkUntrusted :234-239),
  AgentMessageSink :164-175 (blocks TaintSecret only), TaintUntrusted
  label :29-30, TaintedValue/NewTaintedValue, CheckSink on ExtendedTracker.
- internal/security/sanitizer_marker_order_test.go - pins sanitizer pass
  order (C0 strip first). Never touch sanitizer ordering.
- R0 findings doc: docs/research/agent-boundary-taint-findings.md
  (design decisions D1 segment-labeling, D2 deny inventory - honor them).

## Verified Anchors

- internal/agent/taint_hooks.go:30 - NewTaintBeforeToolCall (nil tracker -> nil)
- internal/agent/taint_hooks.go:156-198 - checkMessageTaint; :169 the
  unconditional whole-message `taint.NewTaintedValue(args.Content,
  []taint.TaintLabel{taint.TaintUntrusted}, "agent_message")`
- internal/security/taint/taint.go:164-175 - AgentMessageSink
- internal/security/taint/taint.go:234-239 - Tracker.MarkUntrusted
- internal/security/taint/parallel.go:76-81 - label priority ordering
  (TaintSecret > TaintUntrusted > ...)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C1-A: per-handoff decision (redesigned checkMessageTaint)
//  - consults tracker-recorded TaintUntrusted segments (recorded by leaf
//    02 via MarkUntrusted at handoff time); blocks only tainted segments
//    reaching a sink that blocks that label.
// C2-A: AgentMessageSink().BlockedLabels == [TaintSecret, TaintUntrusted]
// Expose also (leaf 02 consumes):
func SegmentLabelsFor(tracker *taint.ExtendedTracker, content string) []taint.TaintedValue
// Returns the tracker-recorded untrusted segments overlapping content.
```

### What This Leaf Consumes

- C-none from siblings (leaf 02 consumes this leaf). R0 decisions D1/D2
  shape behavior, not signatures.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Redesign checkMessageTaint: remove the unconditional whole-message label; consult tracker-recorded segments via SegmentLabelsFor; block only when a recorded TaintUntrusted segment reaches the sink; add a `mode` to TaintHookConfig (enforce|monitor, default per R0 D-decisions) so over-blocking is tunable | Modify: internal/agent/taint_hooks.go:156-198; Test: internal/agent/taint_hooks_message_test.go (new) | `go test -p 2 ./internal/agent/ -run TestCheckMessageTaint -v` | compile error: SegmentLabelsFor undefined; tests RED for missing behavior | whole-clean handoff passes; tracker-recorded tainted segment blocks; monitor mode logs without blocking | Given a tracker with no untrusted segments; When a `agent:` message passes; Then BlockResult{} (no block). Given a recorded untrusted segment; When the message contains it; Then Block=true with reason. Given monitor mode; When tainted; Then Block=false + Warn log |
| T2 | Add TaintUntrusted to AgentMessageSink.BlockedLabels | Modify: internal/security/taint/taint.go:164-175; Test: internal/security/taint/sink_untrusted_test.go (new) | `go test -p 2 ./internal/security/taint/ -run TestAgentMessageSink -v` | sink currently blocks only TaintSecret -> test RED | sink rejects values labeled TaintUntrusted or TaintSecret; TaintUserInput still passes | Given a TaintedValue labeled TaintUntrusted; When CheckSink against AgentMessageSink; Then violation returned |
| T3 | Segment-tracking API on the tracker: record/clear per-handoff untrusted segments keyed by source ("collab:<driver>:<role>"), with bounded size (map + mutex, no I/O under lock) | Modify: internal/security/taint/taint.go (or new internal/security/taint/segments.go); Test: internal/security/taint/segments_test.go (new) | `go test -p 2 ./internal/security/taint/ -run TestSegment -v` | undefined API -> compile RED | record/query/clear round-trips; concurrent-safe; unbounded growth impossible (cap + evict) | Given MarkUntrusted on a segment; When SegmentLabelsFor(content) runs; Then the overlapping TaintedValue returns. Given > cap segments; When recording more; Then oldest evicted |
| T4 | Regression: whole-message behavior gone - a CLEAN cross-agent message through a tracker with recorded segments NOT overlapping it still passes | Test: internal/agent/taint_hooks_message_test.go (add case) | `go test -p 2 ./internal/agent/ -run TestCheckMessageTaintWholeCleanPasses -v` | would fail on old code (whole-message label) | passes | Given recorded segments + a message without them; When checked; Then no block |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2 ./internal/agent/... ./internal/security/taint/...`)
- [ ] Interface contracts satisfied exactly (C1-A, C2-A, SegmentLabelsFor)
- [ ] gofmt clean; no sanitizer-order change (sanitizer_marker_order_test.go green)
- [ ] No debug artifacts; no scope creep into drivers/components.go (leaf 02)
- [ ] Line-number corruption check: `grep -rn "^\s*[0-9]+|" internal/agent/taint_hooks.go internal/security/taint/` returns nothing

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines of each verify run]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] checkMessageTaint no longer labels whole messages (:169 pattern gone)
- [ ] Contracts C1-A/C2-A match exactly; SegmentLabelsFor exported as specified
- [ ] mutexio clean (no I/O under lock); predid clean (no new IDs needed here)
- [ ] No sanitizer reordering; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Keep the :30 constructor nil-tracker guard - the wiring leaf relies on it
  as its typed-nil seam.
- Mode default: follow R0 decision (if research is silent, default
  `enforce` for TaintSecret-carrying segments, `monitor` for
  TaintUntrusted for one release - state the choice in the report).
- Do NOT touch the collaboration drivers or components.go in this leaf.
