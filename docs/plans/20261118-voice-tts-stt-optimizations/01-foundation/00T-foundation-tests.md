# Foundation contract tests - Test Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Write tests ONLY. Do NOT write or modify implementation files. Tests must fail RED for missing-implementation reasons. Run them once to confirm.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Additive config/keys fields and Smart Turn mode behavior tests
- **Dependencies:** none
- **Estimated Context:** 20K
- **Concurrency Group:** A
- **Audit references:** F1, R4

## Goal

Cover C1/C2 new fields and F1 Smart Turn modes as RED tests before implementation.

## Context

`KeysFrame` decodes an extended C1 line. `VoiceConfig` lives in
`internal/config/schema.go:5622`. Smart Turn analyzer is wired in
`internal/voicechild/open.go:44-84` and consumed nowhere in production
(`pipeline.go:220-223`).

## Verified Anchors

- `internal/voicechild/keys.go:31` - KeysFrame
- `internal/config/schema.go:5622` - VoiceConfig
- `internal/voicechild/open.go:54-84` - analyzer init
- `internal/voicechild/turn.go:22` - TurnAnalyzer interface

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
TestKeysFrameSmartTurnModeDefault, TestKeysFrameSmartTurnModeExplicit,
TestVoiceConfigNewFieldsDefault, TestSmartTurnKillSwitch installs no analyzer,
TestSmartTurnVerdictShortensOrDefersDispatch (mode 2)
```

### What This Leaf Consumes

- C1 KeysFrame - `internal/voicechild/keys.go:31` - 01-keys-contract.md
- C2 VoiceConfig - `internal/config/schema.go:5622` - 01-keys-contract.md
- C3 TurnAnalyzer - `internal/voicechild/turn.go:22` - 02-smart-turn.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Test new KeysFrame fields absent/explicit decode | Test: `internal/voicechild/keys_smartturn_test.go` | `go test -p 2 ./internal/voicechild/ -run TestKeysFrameSmartTurn` | field does not exist | PASS | Given legacy keys line; When decoded; Then SmartTurnMode=0; Given explicit mode=2; When decoded; Then mode=2 |
| T2 | Test new VoiceConfig fields default to zero | Test: `internal/config/smartturn_config_test.go` | `go test -p 2 ./internal/config/ -run TestVoiceConfigSmartTurnDefault` | field does not exist | PASS | Given DefaultConfig(); When Voice read; Then SmartTurnMode=0 and all new fields zero |
| T3 | Test Smart Turn mode 1 installs no analyzer, mode 2 calls AnalyzeEndOfTurn | Test: `internal/voicechild/pipeline_smartturn_test.go` | `go test -p 2 ./internal/voicechild/ -run TestSmartTurnModes` | mode behavior absent | PASS | Given local open + mode=1; When analyzers init; Then turn analyzer nil; Given mode=2 + final; When confirm fires; Then AnalyzeEndOfTurn consulted |

## Self-Verification Checklist

- [ ] All tests written; fail RED for missing-implementation reasons
- [ ] No implementation files written
- [ ] No mask corruption (`grep -oE '\*4' | wc -l` = 0)
- [ ] Tests cover modes 0, 1, 2

**DO NOT COMMIT.** Orchestrator commits gate tests after validation.

## Completion Report Format

1. Files touched
2. Verify command + output (last 20 lines)
3. Deviations with rationale

## Review Checklist (For Review Agent)

- [ ] Tests assert behavior, not implementation details
- [ ] RED reason is missing implementation, not syntax error
- [ ] No implementation files touched

## Notes

- Gate commit may need `--no-verify`; manual gates recorded by orchestrator.
