# exhaustive Lint Gate for WSClass - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Enable the `exhaustive` linter so the WSClass type switch from leaf 03 is coverage-enforced in CI, scoped honestly if repo-wide enablement trips pre-existing switches.
- **Dependencies:** 03-ws-classification.md (COMPLETE - the WSClass switch exists in internal/comm/http/server.go)
- **Estimated Context:** 25K
- **Concurrency Group:** D

## Goal

A new WS-visible payload type without a `WSClass()` method - or a new WSClass constant without a covering case - must fail `make lint-ci` instead of silently rendering as a blank chat bubble. This leaf wires that gate and proves it fires.

## Context

- `.golangci.yml` exists at repo root. Read it fully first: existing enabled linters, `issues.exclude-rules`, any `linters-settings`.
- `make lint-ci` (see Makefile) runs golangci-lint + the custom analyzers; the new linter joins that path via the yml.
- The switch to guard: `internal/comm/http/server.go`, the WSClass switch written by leaf 03 (`switch cls := ...` over `wsclass.WSClass`).
- golangci-lint version in use matters: `exhaustive` is built into golangci-lint (no separate install). Verify the version pinned by the repo/CI (`grep -rn golangci Makefile .github/ 2>/dev/null`) supports it; if the pinned version predates exhaustive support, report BLOCKED with the version found - do not upgrade toolchain versions in this leaf.

Key files:

- `.golangci.yml`
- `Makefile` (lint-ci target - read only)
- `internal/comm/http/server.go` (the switch being guarded - read only)
- `internal/comm/wsclass/wsclass.go` (read only)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```yaml
# .golangci.yml - added to the existing linters.enable list
linters:
  enable:
    - exhaustive
```

- If repo-wide enablement is clean (existing code passes), that is the preferred end state - enable it plainly.
- If pre-existing switches elsewhere trip it: scope via `linters-settings.exhaustive` (e.g. `explicit-exhaustive-switch: true` limits enforcement to switches on types with an explicit marker comment - then add the marker comment `//exhaustive:enforce` on the WSClass switch) or via `issues.exclude-rules` targeting the noisy legacy packages. Whichever mechanism: the WSClass switch MUST be covered, the scoping must be the narrowest that achieves a clean `make lint-ci`, and the final yml must contain a comment explaining the scoping choice. Do NOT add blanket `nolint` comments on the WSClass switch itself.
- Optional (do if clean): a `//exhaustive:enforce` comment above the WSClass switch makes the intent explicit when scoped mode is used.

### What This Leaf Consumes

- Leaf 03's WSClass switch (committed) - the thing being guarded.

## Tasks

### Task 1: Enable exhaustive and establish the baseline

**Objective:** Turn the linter on; find out what it says about the existing repo.

**Files:**
- Modify: `.golangci.yml`

**Step 1: Confirm old state**

- `terminal("grep -n 'linters' -A 20 .golangci.yml")` - current config
- `terminal("golangci-lint version")` - available version (and what Makefile/CI pins)
- `terminal("golangci-lint run --no-config --disable-all -E exhaustive ./internal/comm/... 2>&1 | head -50")` - what exhaustive says about the comm tree right now (post-leaf-03, this should be CLEAN - the switch covers both constants)

**Step 2: Write the "failing test" (the gate itself, exercised via temp break)**

The TDD loop here is config + a deliberate mutation test:

1. Edit `.golangci.yml` to enable `exhaustive` (start plain, repo-wide).
2. Run: `make lint-ci 2>&1 | tail -30` (or `golangci-lint run ./internal/... | head -50` if make lint-ci needs services unavailable in this environment - report which you ran).
3. Record the result as your baseline (see Step 3 branch).

**Step 2b: Verify failure fires (the RED step, in a THROWAWAY state)**

- Temporarily add a third WSClass constant WITHOUT extending the switch (edit `internal/comm/wsclass/wsclass.go` in your working tree only - this file gets REVERTED before you finish; do it via `git stash`-free direct edit + explicit `git checkout -- internal/comm/wsclass/wsclass.go` after the experiment, and say so in your report):
  ```go
  const (
      WSChatMessage WSClass = iota
      WSProgress
      WSTestOnly // TEMPORARY - lint experiment, reverted
  )
  ```
- Run the linter on comm again. Expected: exhaustive flags the WSClass switch in server.go as non-exhaustive. If it does NOT flag it, the scoping mechanism is wrong - fix the config until it does.
- REVERT the temporary constant: `git checkout -- internal/comm/wsclass/wsclass.go`. Verify clean: `git diff --stat internal/comm/wsclass/` empty.

This RED experiment is the test that the gate works. Report its exact output.

**Step 3: Write implementation (the final config state)**

- If Step 2's plain repo-wide run produced a clean baseline (no pre-existing exhaustive findings outside the deliberate mutation): keep plain repo-wide enablement.
- If it produced unrelated findings: scope narrowly (per contract) until `make lint-ci`-equivalent run is clean WITH the WSClass switch covered. Document exactly what was noisy and what scoping was chosen.

**Step 4: Verify pass**

- `golangci-lint run ./internal/...` (or the make target) - clean.
- The reverted wsclass.go compiles: `go build ./internal/comm/...`

### Task 2: CI wiring verification + report

**Objective:** Confirm the gate runs where CI runs it, and document.

**Files:**
- Modify: `.golangci.yml` (comments)
- Possibly modify: CI workflow file IF it pins a separate golangci config or version that needs the same yml (read `.github/workflows/code-quality.yml` if present; only touch it if the linter would NOT run there otherwise)

**Step 1: Read current state**

- Find where lint-ci runs in CI (Makefile target -> workflow). Confirm the workflow uses this `.golangci.yml` (same file, no override) and its golangci-lint version supports exhaustive.

**Step 2: Confirm old state**

Record: workflow file, the lint job's commands, version pins.

**Step 3: Write implementation**

- Add the explanatory comment in `.golangci.yml` next to the enable line: what exhaustive guards here (WSClass switch in internal/comm/http/server.go; new WS-visible payload types must implement WSClass and the switch must cover all constants).
- Touch the workflow ONLY if the linter would not otherwise run there; if you touch it, diff-explain in the report.

**Step 4: Verify**

- Local gate run clean (same command as Task 1 Step 3).
- `go build ./...` unaffected.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] `exhaustive` enabled in `.golangci.yml` (plain or scoped, with comment)
- [ ] RED experiment performed and reverted; `git diff --stat internal/comm/wsclass/` is empty; experiment output in report
- [ ] The gate demonstrably covers the WSClass switch (the RED experiment proved it fires)
- [ ] Local lint run clean
- [ ] CI path verified (workflow uses this config; version supports exhaustive) - or BLOCKED report with versions
- [ ] No `nolint` on the WSClass switch
- [ ] No other config changes (do not enable/disable unrelated linters)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] `.golangci.yml` diff is minimal and commented
- [ ] Scoping (if any) is the narrowest that works; WSClass switch covered either way
- [ ] RED experiment evidence in report; wsclass.go fully reverted
- [ ] No unrelated linter changes, no toolchain upgrades
- [ ] `make lint-ci` path (or its local equivalent) passes

Output: APPROVED or specific gaps with file + line references.

## Notes

- This leaf is small on purpose. Do not expand scope to other linters or fix unrelated findings that pre-date this tree - report them, orchestrator decides.
- If golangci-lint is not installed locally, install is OUT OF SCOPE without the user's explicit OK (standing rule: never install software unprompted). Instead verify via `go run github.com/golangci/golangci-lint/cmd/golangci-lint@<pinned-version> run ...` only if the pinned version is already in the module cache; otherwise report BLOCKED with findings-from-config-review and let the orchestrator decide (CI will exercise the gate regardless).
