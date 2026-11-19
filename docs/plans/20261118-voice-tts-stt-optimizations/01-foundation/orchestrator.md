# Foundation - Implementation Orchestrator

> Dispatch, review in-session, re-dispatch on gaps, track. Do NOT implement
> code. Do NOT commit until a child passes review.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 documents
- **Scope:** Additive config + keys seam, and Smart Turn verdict decision (F1)

## Goal

Land the shared additive contract that every other branch consumes. Resolve
F1 by consuming the Smart Turn verdict behind an opt-in mode, with a
kill-switch that removes the dead per-call ONNX work.

## Architecture

Config -> `VoiceConfig` new fields -> `sendKeysFrame` -> `KeysFrame` new
omitempty fields -> child decode -> analyzer/factory selection. This branch
owns `schema.go`, `voice_keys.go`, `keys.go` exclusively until COMPLETE.
Other branches consume the fields but must not edit these files.

## Interface Contracts

### C1: KeysFrame additive selector block

```
SmartTurnMode int    // 0=off, 1=kill-switch(no model), 2=consume-verdict
// full definition in ../master.md C1
// Owner: 01-keys-contract.md
// Consumers: 02-smart-turn.md, sibling branches
```

### C2: VoiceConfig additive config block

```
SmartTurnMode int `json:"smart_turn_mode"` // 0 default
// full definition in ../master.md C2
// Owner: 01-keys-contract.md
// Consumers: 02-smart-turn.md, sibling branches
```

### C3: Smart Turn verdict seam

```
type TurnAnalyzer interface {
    AppendAudio(chunk []byte, isSpeech bool)
    AnalyzeEndOfTurn() (complete bool, probability float64, err error)
    Clear()
    Close() error
}
// existing in internal/voicechild/turn.go:22-44
// Owner: 02-smart-turn.md
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|--------------|-------------|
| 00T | 00T-foundation-tests.md | leaf (test) | none | 20K | A |
| 01 | 01-keys-contract.md | leaf | 00T | 45K | B |
| 02 | 02-smart-turn.md | leaf | 01 | 55K | C |
| 03 | 03-docs-agents.md | leaf | 01,02 | 20K | D |

Same-letter children dispatch together. 03 serializes with other branches'
doc leaves on `AGENTS.md`; foundation completes first, so no overlap.

## Dispatch Protocol

1. Phase 0: dispatch `00T-foundation-tests.md`; validate IN-SESSION; commit
   RED gate tests with `--no-verify` after manual gate record.
2. Phase 1: dispatch `01-keys-contract.md` (single agent). Review; commit.
3. Phase 2: dispatch `02-smart-turn.md`. Review; commit.
4. Phase 3: dispatch `03-docs-agents.md`. Review; commit.
5. Record statuses here. Report COMPLETE to root when all children COMPLETE.

Review in-session only; never a delegated reviewer. Max 3 re-dispatch cycles.

## Review Checklist

- [ ] All leaf tasks implemented
- [ ] C1/C2 contracts satisfied exactly (field names, defaults, omitempty)
- [ ] Gate tests pass GREEN and unmodified
- [ ] No I/O under playMu; no nested locks
- [ ] No scope creep or debug artifacts
- [ ] No line-number corruption

## Coding Conventions

- Go 1.27; gofmt before reporting.
- search_files/cat for exploration; no read_file on source; write once.
- Wrap errors with `%w`; no ignored errors; no `_ = ` in non-test code.
- Tests table-driven; hermetic e2e only.
- `go test -p 2 ./...`.
- ASCII only; UI text lowercase; no secret logging.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | "optimizations and refinements based on our skills" | F1 -> 02-smart-turn.md | 00T + leaf tests |
| R4 | "keep everything opt-in or byte-identical by default" | 01-keys-contract.md T1-T3 | additive wire test |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-foundation-tests | PENDING | 0 | |
| 01-keys-contract | PENDING | 0 | owns shared seam |
| 02-smart-turn | PENDING | 0 | F1 |
| 03-docs-agents | PENDING | 0 | |

## Integration Test Plan

```bash
go test -p 2 ./internal/config/... ./internal/daemon/... ./internal/voicechild/...
go test -tags e2e -p 2 ./e2e/suites/voice_tts_stt/...
```
Expected: green; absent selectors produce legacy wire; SmartTurnMode=0
byte-identical; mode=1 installs no analyzer.

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | Smart Turn mode 0/1/2 contracts | 02-smart-turn.md | `TestSmartTurnModes`, `TestAnalyzeEndOfTurnConsumed` | root additive wire | PENDING |
| K1 | Keys + config additive fields | 01-keys-contract.md | `TestKeysFrameNewFields`, `TestConfigNewDefaults` | root additive wire | PENDING |

## Open Questions

- Q1 resolved by root: mode 2 consumes verdict behind opt-in; mode 1 is
  immediate kill-switch. Mode 0 = today's behavior (analyzer fed, verdict
  discarded).

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dated dispatch addenda.

## Notes

- Root worktree branch: plan/voice-tts-stt-optimizations.
- F1 anchor: `internal/voicechild/pipeline.go:220-223,1506-1510`.
