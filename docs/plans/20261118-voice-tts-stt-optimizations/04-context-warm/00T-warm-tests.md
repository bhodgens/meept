# Context warm contract tests - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** RED test for F7
- **Dependencies:** none
- **Estimated Context:** 18K
- **Concurrency Group:** A
- **Audit references:** F7,R4

## Goal

Cover the separate warm window as a RED test before implementation.

## Context

The warmer debounces with `cfg.Window` in `contextwarm.go:225`; RoutePartial is the entry at `:428`; OnFinal cancels at `:248`.

## Verified Anchors

- `internal/voice/contextwarm.go:225` - debounce window use
- `internal/voice/contextwarm.go:428` - RoutePartial
- `internal/voice/contextwarm.go:248` - OnFinal
- `internal/voice/contextwarm.go:53-56` - default 900 ms

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
TestContextWarmWindowOverride
```

### What This Leaf Consumes

- C1 ContextWarmWindowMs - 01-separate-window.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Test override and legacy window | Test: `internal/voice/contextwarm_window_test.go` | ``go test -p 2 ./internal/voice/ -run TestContextWarmWindowOverride`` | field or flag absent | PASS | Given override 300; When RoutePartial; Then timer fires at 300ms; Given 0; Then fires at resolved turn_confirm_ms (900 default) |

## Self-Verification Checklist

- [ ] All tasks implemented; verify commands pass
- [ ] Interface contracts satisfied exactly (field names, defaults, paths)
- [ ] All files at exact specified paths
- [ ] No scope creep; no debug artifacts, no TODOs, no line-number corruption
- [ ] Default values preserve pre-change behavior byte-identically when the new knob is unset

**DO NOT COMMIT.** Orchestrator handles git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + output (last 20 lines)
3. Deviations with rationale

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched
- [ ] Contracts match exactly (signature, type, path, default)
- [ ] No scope creep; no bugs; no secrets in logs

## Notes

- Tests only; no implementation files.
