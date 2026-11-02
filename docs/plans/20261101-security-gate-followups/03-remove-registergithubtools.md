# Remove RegisterGitHubTools, Re-home Tests - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below. Do NOT commit
> - the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back. Prefix every go
> command with GOTOOLCHAIN=go1.26.5; run from /Users/caimlas/git/meept.

## Meta

- **Parent:** ../master.md
- **Scope:** Delete githubsvc.RegisterGitHubTools (zero production callers
  since the provider seam landed) and re-home its tests against the
  provider path (builtin.SetGitHubProvider + toolprov discovery).
- **Dependencies:** none
- **Estimated Context:** 50K
- **Concurrency Group:** A
- **Audit references:** tool-provider-seam tracking table follow-up

## Goal

One registration path for github_* tools: the provider. The dead function
goes away; the tests it carried move to exercise the real production route
so coverage is preserved, not lost.

## Context

- internal/githubsvc/register.go:95 - RegisterGitHubTools (the deletion
  target); its doc comments at :17 and :29 reference it (update those).
- internal/tools/builtin/github_register.go - the live path:
  init()-registered "github" provider factory + SetGitHubProvider binder.
- internal/tools/builtin/github_register_test.go +
  github_readonly_test.go - tests currently calling RegisterGitHubTools
  (re-home these).
- The provider's Discover() constructs the same eight tools with the same
  args - the re-homed tests should produce identical tool sets.

## Verified Anchors

- internal/githubsvc/register.go:95 - `func RegisterGitHubTools(...)`
- internal/tools/builtin/github_register.go - SetGitHubProvider + init
  factory (read for the binder signature)
- internal/toolprov - Names/RegisterAll for the discovery walk

## Interface Contracts (From Parent)

### What This Leaf Exposes

- internal/githubsvc: no RegisterGitHubTools symbol (build proves it).
- Re-homed test files asserting the same behaviors via the provider path.

### What This Leaf Consumes

- C2 (master.md).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Re-home tests FIRST (they go green against the provider path while the old function still exists) | Modify: internal/tools/builtin/github_register_test.go, github_readonly_test.go | `GOTOOLCHAIN=go1.26.5 go test -count=1 ./internal/tools/builtin/ -run TestGitHub` | n/a (re-homing is green immediately; RED proof is T2's build failure) | Same assertions as before, driven through SetGitHubProvider + toolprov.Names/RegisterAll discovery instead of RegisterGitHubTools | Given the provider bound with test opts; When discovered; Then the same eight tools with same names/categories |
| T2 | Delete the dead function + fix its doc comments | Modify: internal/githubsvc/register.go | `GOTOOLCHAIN=go1.26.5 go build ./... ; go test -count=1 ./internal/githubsvc/` | RED: any remaining reference is a build failure (this IS the verification) | Build green repo-wide; no RegisterGitHubTools symbol; doc comments updated; githubsvc suite green | Given the deletion; When the repo builds; Then zero references remain |

Check for stragglers after T2: `grep -rn RegisterGitHubTools internal/ cmd/
--include='*.go'` must return only historical doc references if any (none
expected - update those too).

## Self-Verification Checklist

- [ ] Re-homed tests preserve the original assertions (diff the assertion
      bodies before rewriting - read the OLD tests with cat BEFORE editing)
- [ ] Symbol gone; build green; both suites green
- [ ] gofmt clean; ASCII-only

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify output (last 20 lines)
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Original assertion coverage preserved (spot-check 2 assertions)
- [ ] Zero straggler references
- [ ] No unrelated githubsvc changes

Output: APPROVED or specific gaps.

## Notes

- githubsvc may NEVER import builtin (import cycle - the factory seam
  exists for exactly this). The re-homed tests live in builtin, which MAY
  import githubsvc.
