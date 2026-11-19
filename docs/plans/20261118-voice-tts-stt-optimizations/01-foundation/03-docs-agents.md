# Voice docs + AGENTS.md updates (foundation) - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Update docs only. No Go code changes.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Document new config knobs and Smart Turn modes
- **Dependencies:** 01-keys-contract.md, 02-smart-turn.md
- **Estimated Context:** 20K
- **Concurrency Group:** D
- **Audit references:** R1, R4

## Goal

Update user-facing docs and the package invariant doc for every changed
surface in this branch. Keep root AGENTS.md summary aligned if a new
cross-boundary contract appears.

## Context

Docs locations: `docs/workflows/voice.md:138` (config table),
`docs/workflows/voice-sidecar.md:1245` (turn confirm),
`internal/voice/AGENTS.md` (full contract), root `AGENTS.md` summary.
Every feature code change requires doc updates per repo policy.

## Verified Anchors

- `docs/workflows/voice.md:138` - config table
- `docs/workflows/voice-sidecar.md:1245` - turn_confirm_ms
- `internal/voice/AGENTS.md` - full voice contract
- root `AGENTS.md` - voice invariant summary

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Documentation only; no code signatures.
```

### What This Leaf Consumes

- C1/C2 contracts - root master.md
- Smart Turn mode behavior - 02-smart-turn.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Document new config knobs | Modify: `docs/workflows/voice.md`, `docs/workflows/voice-sidecar.md` | `grep -n 'smart_turn_mode' docs/workflows/voice.md` | undocumented | PASS | Given docs; When searched; Then each new knob has a row with default and effect |
| T2 | Update voice AGENTS.md contract | Modify: `internal/voice/AGENTS.md` | `grep -n 'SmartTurnMode' internal/voice/AGENTS.md` | absent | PASS | Given AGENTS.md; When read; Then mode 0/1/2 semantics and lock rule stated |
| T3 | Update root AGENTS.md summary if contract changed | Modify: `AGENTS.md` | `grep -n 'smart turn' AGENTS.md` | absent | PASS | Given new cross-boundary selector; When read; Then summary mentions it |

## Self-Verification Checklist

- [ ] All tasks done; grep commands pass
- [ ] Docs use lowercase UI text, ASCII hyphens
- [ ] No stale claims; no code changes

**DO NOT COMMIT.** Orchestrator handles git operations after review.

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Every new knob documented with default and legacy behavior
- [ ] AGENTS.md summary and full doc agree
- [ ] No code changes

## Notes

- Only edit root AGENTS.md if a new cross-boundary contract exists.
