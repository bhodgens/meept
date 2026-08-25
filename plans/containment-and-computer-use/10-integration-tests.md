# Integration Tests - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Do NOT commit.

## Meta

- **Parent:** ../master.md
- **Scope:** Cross-leaf integration suite proving contracts hold end-to-end.
- **Dependencies:** 01-09 all
- **Estimated Context:** 70K
- **Concurrency Group:** E

## Goal

Prove the four workstreams compose: env isolation under real backend execution, placeholder round-trip through proxy, sandbox refusal, staging->accept->journal->revert chain, and surface parity. These tests guard the seams individual leaves cannot.

## Context

All leaf packages exist by dispatch time. Read master.md Interface Contracts first. Use t.TempDir everywhere; no daemon-process spawning required except where a test helper already exists in internal/daemon tests (reuse if present, else construct components directly).

## Interface Contracts (From Parent)

### Exposes

```
File: internal/integration/containment_test.go  (package _test build-tagged `//go:build integration` OR plain — match repo convention for cross-package tests if one exists)
Tests:
1. TestEnvStrippedThroughBackendExecution — LocalBackend w/ allowlist policy executes printenv sentinel -> empty; stripped names include sentinel.
2. TestSecretPlaceholderRoundTrip — Broker+Proxy vs httptest upstream matching host: body/header placeholders replaced with real formatted value; mismatched host untouched + metric>0; child env contains ONLY placeholder (assert via BuildChildEnv output).
3. TestSandboxRefusalFailsClosed — ResolverConfig{RequireSandbox:true} w/ probes returning unavailable -> ResolveBackend error is ErrSandboxRequired; ShellExecuteTool-level test asserting refusal error text surfaces.
4. TestStageAcceptJournalRevertChain — StageWrite -> drift mutate -> accept refused -> restore -> accept ok -> journal List has entry -> Revert restores original bytes -> second Revert idempotent.
5. TestSurfacesSeeSameState — create change via registry; GET endpoint returns it; TUI-facing API interface returns same count; accept via HTTP marks resolved for agent-side Get too.
```

### Consumes

Public APIs of leaves 01-07 only (no internals).

## Tasks

### Task 1: Tests 1-3 (containment trio)

Standard TDD per test; each must FAIL against pre-leaf behavior conceptually — since leaves landed, instead assert exact contract values (e.g., refusal message substring) to catch regressions. Run with -race.

### Task 2: Tests 4-5 (chain + surfaces)

Same discipline. For test 5 use httptest server from comm/http tests' established fixture pattern.

### Task 3: Suite wiring

Ensure `go test ./... -race` includes these without flakiness (no sleeps; channels/timeouts). Add package README comment describing scope.

## Self-Verification Checklist

- [ ] Five tests green -race; deterministic
- [ ] Only public APIs used
- [ ] No new deps

**DO NOT COMMIT.**
**Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Each orchestrator Integration Test Plan item covered
- [ ] Failure messages asserted where contract specifies wording
- [ ] Skips cleanly on darwin for bwrap-specific assertions

Output: APPROVED or gaps.

## Notes

- This leaf may adjust earlier-leaf code minimally IF a seam proves broken; report every adjustment as deviation for orchestrator review.
