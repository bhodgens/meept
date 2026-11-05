# Conversation-ID Attribute Restore - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Populate the llm.call span's conversation_id attribute
  (regressed to always-"" when leaf 06 of 20261103-wiring-gap-remediation
  removed the last-seen heuristic). Bounded trace->conversation map in
  the daemon wiring layer - same posture as the turnSpanID map.
- **Dependencies:** none.
- **Estimated Context:** 30K

## Goal

otlp_traces_wiring.go: the turn.terminal event carries conversation_id
(frozen set) but the wiring only kept trace->turnSpan. Extend the map
(bounded, cap 1024, same eviction) to trace->conversationID and stamp
the llm.call/tool.call spans' conversation_id attribute from it.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Extend the wiring map: trace->turnSpanID becomes a small struct {turnSpanID, conversationID} (same bounded cap + eviction). Fill both fields from ev in the terminal subscription. Stamp conversation_id attr on llm.call/tool.call spans when known. | Modify: internal/daemon/otlp_traces_wiring.go | `go test -p 2 ./internal/daemon/ -run Trace` | attr always "" | attr populated on traced turns | Given traced turn w/ conversation; When hop exported; Then conversation_id attr present |
| T2 | Unit: publish terminal w/ TraceID+conversation_id, record a hop, assert the hop's conversation_id attr == the event's value; unknown trace -> attr absent (not empty-string); bounded-map eviction still pins. Extend the existing wiring test. | Modify: internal/daemon/otlp_traces_wiring_test.go | same | attr regression unproven | pinned | Given wiring removed; When hop exports; Then attr missing = FAIL |
| T3 | e2e spot: e2e/suites/llm-hop-tracing still green (its assertions unchanged; if it asserts conversation_id empty anywhere, update to populated). | Modify: e2e/suites/llm-hop-tracing only if it asserts the old behavior | `go test -p 2 -tags e2e ./e2e/suites/llm-hop-tracing/` | suite red = regression | suite green | - |

## Self-Verification Checklist

- [ ] go build ./... green (scoped: daemon builds even with sibling WIP elsewhere)
- [ ] daemon Trace tests green
- [ ] llm-hop-tracing e2e green
- [ ] gofmt clean, ASCII only
- [ ] map bounded + nil-safe (same posture as turnSpanID map)

## Review Checklist (orchestrator)

- [ ] No loop.go changes (wiring layer only)
- [ ] Eviction cap matches the turnSpanID map discipline
- [ ] Unknown-trace -> attr absent (no empty-string attr)

## Do NOT commit. The orchestrator stages and commits after review.
