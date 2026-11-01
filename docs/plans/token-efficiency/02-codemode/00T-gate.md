# Branch 02 Test Gate - E2E Suite Skeleton - Test Leaf

> **For the implementing agent:** Write e2e test cases ONLY (build tag `e2e`),
> no implementation. Cases must fail RED because the codemode package/tool
> does not exist yet. Do NOT commit.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** e2e/suites/codemode skeleton cases pinning C3 behavior + config gate
- **Dependencies:** 00S verdict (chosen runtime shapes only the sandbox-internal cases; suite-level cases are runtime-agnostic)
- **Estimated Context:** 45K
- **Concurrency Group:** solo

## Goal

Author hermetic e2e cases (no live model; scripts are fixed inputs): tool
registered iff codemode_enabled; script returning a value; timeout kill;
nested tool calls absent from LLM-visible context (assert on the harness's
captured request payloads).

## Context

- e2e harness: e2e/harness/ (see docs/workflows/e2e-testing.md and the
  meept-e2e-harness skill); manifest: e2e/manifest.json (path_map needs an
  internal/codemode entry - add in THIS leaf so the gate compiles as a suite).
- Existing suites as templates: e2e/suites/* (pick the closest shell/tool suite).

## Verified Anchors

- e2e/manifest.json - suite registry
- docs/workflows/e2e-testing.md - tier + manifest format

## Interface Contracts (From Parent)

### What This Leaf Exposes

Suite `e2e/suites/codemode/` with cases:
- TestCodemodeDisabledByDefault (tool absent from definitions when flag off)
- TestCodemodeToolRegisteredWhenEnabled
- TestCodemodeScriptReturnValue (fixed script `return 1+1;` -> Result.Value "2")
- TestCodemodeTimeoutKills (infinite loop + timeout_s=1 -> Err set, wall < 5s)
- TestCodemodeNestedCallsStayOutOfContext (script calls tools.echo twice;
  assert the recorded outbound LLM request contains only ONE codemode tool
  result, no echo tool messages)

### What This Leaf Consumes

- C3 Result fields (OK/Output/Value/Err) - asserted verbatim in JSON.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Suite skeleton + manifest entry | Create: e2e/suites/codemode/*.go; Modify: e2e/manifest.json | `go test -p 2 -tags e2e ./e2e/suites/codemode/ -run TestCodemodeDisabledByDefault` | compile-RED (package missing) is ACCEPTED for this gate (new-feature e2e; orchestrator records it) | cases compile+pass after leaf 02 | Given a default config; When tool definitions are listed; Then codemode is absent |
| T2 | Five pinned cases | Create: e2e/suites/codemode/codemode_test.go | `go vet -tags e2e ./e2e/suites/codemode/` | as above | as above | per-case GWT in Contracts section |

## Self-Verification Checklist

- [ ] Build tag `e2e` on every file; hermetic (no network, no live model)
- [ ] manifest path_map entry present
- [ ] No implementation code

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. vet/test output tails; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Cases assert C3 field names verbatim
- [ ] Nested-call case asserts on recorded LLM request payloads (real evidence, not logs)
