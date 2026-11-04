# TurnSpanID Parent Linkage - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Additive turn_span_id on TurnTerminalEvent (CLOSED contract
  - repeat the TraceID extension protocol exactly), receiver fills
  lastTurn.turnSpan; trace->turnspan map not last-seen-terminal.
- **Dependencies:** none. Wave C.
- **Estimated Context:** 60K

## Goal

See parent master.md traceability table (this leaf's Req row).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Additive field: TurnTerminalEvent.TurnSpanID string json:"turn_span_id,omitempty" (handler.go:264-286) + extension note in docs/plans/20260916-turn-lifecycle-events/master.md SAME change (TraceID precedent handler.go:278-285). Fill centrally in publishTurnTerminal (handler.go:1698)/EmitTurnTerminal (:2366) via turnRegistry.TraceForTurn (returns both ids); AUDIT all 9 literal sites (handler.go:738,767,1324,1888,2490,2520,2920,2951 + helpers) - not all flow through helpers. | Modify: internal/agent/handler.go, events if separate, master.md extension note | `go test -p 2 ./internal/agent/ -run Terminal` | field absent | field populated on traced turns; omitempty keeps untraced bytes identical | Given traced turn; When terminal published; Then turn_span_id present |
| T2 | Receiver: otlp_traces_wiring.go:109-161 sets lastTurn.turnSpan = ev.TurnSpanID; DESIGN CAVEAT: mid-turn hops arrive BEFORE their terminal - implement a trace->turnspan map keyed on ev.TraceID, not a conversation last-seen heuristic. | Modify: internal/daemon/otlp_traces_wiring.go + test | `go test -p 2 ./internal/daemon/ -run Trace` | ParentSpanID always empty | hop spans carry parent | Given llm.call hop; When exported; Then ParentSpanID == turnSpanID |
| T3 | Wiring unit: publish terminal with TurnSpanID, record an LLM-call hop, assert LLMCallSpanSpec.ParentSpanID == turnSpanID; delete field -> empty parent = FAIL. Consumer audit: TUI/GUI/bench decode unchanged (additive). | Modify: otlp wiring tests + consumer audit notes | same | parent empty | assertion passes; consumers confirmed | Given the wiring removed; When hop exports; Then ParentSpanID empty = test fails |

## Self-Verification Checklist

- [ ] go build ./... green
- [ ] scoped tests green (-p 2)
- [ ] gofmt clean, ASCII only
- [ ] wiring-failure proof demonstrated
- [ ] sibling files untouched

## Review Checklist (orchestrator)

- [ ] Wiring point matches the survey table
- [ ] Proof test fails with wiring deleted (witnessed)
- [ ] No silent scope expansion
- [ ] Staged-set audit before commit

## Do NOT commit. The orchestrator stages and commits after review.
