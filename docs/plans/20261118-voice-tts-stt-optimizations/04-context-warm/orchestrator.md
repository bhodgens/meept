# Context warm refinement - Implementation Orchestrator

> Dispatch, review in-session, re-dispatch on gaps, track. Do NOT implement
> code. Do NOT commit until a child passes review.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 documents
- **Scope:** F7 separate context-warm debounce window; document Whisper-only gap

## Goal

Let context warm land before the confirm-window dispatch instead of racing
it. Document that the Whisper/local leg produces no partials, so warm is
phone/Deepgram-only.

## Architecture

`RoutePartial` debounces a warm request using `cfg.Window`, which today
equals resolved `turn_confirm_ms` (900 ms). Adding a separate shorter
window moves the warm request ahead of dispatch. `OnFinal` still cancels
a pending warm.

## Interface Contracts

### C1: Context warm window selector

```
ContextWarmWindowMs int // 0 = resolved turn_confirm_ms (legacy); >0 = override
// Owner: 01-separate-window.md
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|--------------|-------------|
| 00T | 00T-warm-tests.md | leaf (test) | none | 18K | A |
| 01 | 01-separate-window.md | leaf | 00T, foundation | 30K | B |
| 02 | 02-warm-docs.md | leaf | 01 | 18K | C |

## Dispatch Protocol

1. Phase 0: dispatch `00T-warm-tests.md`; validate IN-SESSION; commit RED
   tests after manual gate record.
2. Phase 1: dispatch `01-separate-window.md`. Review; commit.
3. Phase 2: dispatch `02-warm-docs.md`. Review; commit.
4. Record statuses; report COMPLETE to root.

Review IN-SESSION only. Max 3 re-dispatch cycles.

## Review Checklist

- [ ] All leaf tasks implemented
- [ ] C1 contract satisfied; default exact
- [ ] Gate tests pass GREEN and unmodified
- [ ] No scope creep or debug artifacts

## Coding Conventions

- Go 1.27; gofmt before reporting.
- search_files/cat for exploration; write once, do not read back.
- Wrap errors with %w; no ignored errors.
- Tests table-driven; hermetic e2e only; `go test -p 2 ./...`.
- ASCII only; no secret logging.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | "optimizations and refinements based on our skills" | F7 -> 01-separate-window.md | 00T + leaf tests |
| R4 | "keep everything opt-in or byte-identical by default" | C1 default | additive test |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-warm-tests | COMPLETE | 10 e0475e9d: gate committed, RED confirmed, now GREEN |
| 01-separate-window | COMPLETE | 10 463b4faf: F7 warm debounce override; OnFinal unchanged |
| 02-warm-docs | COMPLETE | 10 5561c999: warm knob + Whisper limit documented |

## Integration Test Plan

```bash
go test -p 2 ./internal/voice/... ./internal/daemon/...
make e2e-fast-area AREA=context_warm
```
Expected: green; window=0 reproduces 900 ms; OnFinal still cancels.

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F7 | Separate warm window | 01-separate-window.md | `TestContextWarmWindowOverride` | context_warm | VALIDATED |

## Open Questions

- Q5: benefit unmeasured; warm hit rate needs paired measurement before
  recommending a non-zero override.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dated dispatch addenda.

## Notes

- F7 fact: `stt_whisper.go:81-83` emits finals only; no partial to warm.
