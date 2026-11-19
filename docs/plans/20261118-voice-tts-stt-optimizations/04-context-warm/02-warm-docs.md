# Context warm docs update - Implementation Leaf

> **For the implementing agent:** Do NOT commit. The orchestrator handles all git operations after review. Do NOT use read_file on existing source files; explore with search_files or terminal cat. After writing a file, do NOT read it back. Gate tests exist and are committed; make them pass. Do NOT delete, weaken, skip, or rewrite a gate test. Report a suspected-wrong gate test to the orchestrator.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Document separate warm window and Whisper-only limit
- **Dependencies:** 01-separate-window.md
- **Estimated Context:** 18K
- **Concurrency Group:** C
- **Audit references:** R1,R4

## Goal

Document the new knob and the honest limit that Whisper emits no partials.

## Context

Docs: `docs/workflows/voice.md`, `internal/voice/AGENTS.md` context-warm section.

## Verified Anchors

- `docs/workflows/voice.md:138` - config table
- `internal/voice/AGENTS.md` - context warm section

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Documentation only; no code signatures.
```

### What This Leaf Consumes

- C1 ContextWarmWindowMs - branch orchestrator

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Document warm window knob and limit | Modify: `docs/workflows/voice.md`, `internal/voice/AGENTS.md` | ``grep -n "context_warm_window_ms" docs/workflows/voice.md`` | undocumented | PASS | Given docs; When read; Then knob default and Whisper no-partial limit stated |

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

- None.
