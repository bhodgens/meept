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
- **Dependencies:** 03-ws-classification.md (COMPLETE - the WSClass switch over six constants exists in internal/comm/http)
- **Estimated Context:** 25K
- **Concurrency Group:** D

## Goal

A new WS-visible payload type without a `WSClass()` method - or a new WSClass constant without a covering case - must fail `make lint-ci` instead of silently misclassifying. This leaf wires that gate and proves it fires with a RED experiment.

## Context

- `.golangci.yml` exists at repo root. Read it fully first: enabled linters, `linters-settings`, `issues.exclude-rules`.
- `make lint-ci` (Makefile) = golangci-lint + analyzers + audit scripts + fmt-check-gui. The new linter joins via the yml.
- The guarded switch: the WSClass -> wire-string switch leaf 03 added (in internal/comm/http), over six constants (WSChatMessage, WSProgress, WSMetricsUpdate, WSJobUpdate, WSPlanUpdate, WSEvent).
- `exhaustive` is built into golangci-lint. Check the pinned version (Makefile / .github/workflows/code-quality.yml). If the pinned version predates exhaustive support: report BLOCKED with the version - do NOT upgrade toolchains in this leaf (standing rule: no software installs/updates without explicit user OK).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```yaml
# .golangci.yml - added to the existing linters.enable list
linters:
  enable:
    - exhaustive
```

- Plain repo-wide enablement preferred if existing code passes.
- If pre-existing switches trip it: scope via `linters-settings.exhaustive` (e.g. `explicit-exhaustive-switch: true` + a `//exhaustive:enforce` comment on the WSClass switch) or narrow `issues.exclude-rules`. The WSClass switch MUST be covered; scoping must be the narrowest that yields a clean run; the final yml carries a comment explaining the choice. NEVER a `nolint` on the WSClass switch itself.

### What This Leaf Consumes

- Leaf 03's WSClass switch (committed).

## Tasks

### Task 1: Enable exhaustive, baseline, and RED experiment

**Objective:** Turn the linter on; establish the baseline; prove the gate fires via a deliberate, fully-reverted mutation.

**Files:**
- Modify: `.golangci.yml`
- TEMPORARILY modify then REVERT: `internal/comm/wsclass/wsclass.go`

**Step 1: Confirm old state**

- Current yml (grep linters section), golangci-lint version (binary on PATH and any CI pin).

**Step 2: RED experiment (do this BEFORE finalizing config)**

1. Enable `exhaustive` in .golangci.yml (plain, repo-wide).
2. TEMPORARILY add to wsclass.go:
   ```go
   WSTestOnly // TEMPORARY lint experiment - reverted before completion
   ```
   (iota appends after WSEvent; the leaf 03 switch now misses a case.)
3. Run the linter on the comm packages: expected - exhaustive flags the WSClass switch. If NOT flagged: fix the config mechanism (try `explicit-exhaustive-switch: true` + `//exhaustive:enforce` on the switch) until it DOES flag. Record the exact finding output.
4. REVERT the constant: `git checkout -- internal/comm/wsclass/wsclass.go`. Verify: `git diff --stat internal/comm/wsclass/` empty.
5. Run the linter again: clean (the real six-constant switch covers everything).

The RED experiment output (step 3) and clean re-run (step 5) are both required in your report.

**Step 3: Finalize config**

- If the plain repo-wide run (with the real code) is clean: keep it plain.
- If unrelated pre-existing switches trip: scope per the contract until clean, documenting what was noisy.
- Add the explanatory comment in the yml: what exhaustive guards (the WSClass switch; new WS-visible payload types implement WSClass(); switch coverage is CI-enforced).

**Step 4: Verify**

- Linter run clean on ./internal/... (or the narrowest scope that is clean).
- `go build ./...` unaffected; wsclass.go fully reverted.

### Task 2: CI path verification

**Objective:** Confirm the gate actually runs where CI runs it.

**Files:**
- Modify: `.github/workflows/code-quality.yml` ONLY if the linter would not otherwise run there

**Step 1: Read current state**

- Find the lint job: which workflow, which commands, which golangci-lint version, whether it uses the same .golangci.yml.

**Step 2: Confirm old state**

Record the job's commands + version pin in your report.

**Step 3: Write implementation**

- Touch the workflow only if the gate would not run there otherwise; explain any diff.

**Step 4: Verify**

- Local gate command clean; `go build ./...` fine.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] `exhaustive` enabled in .golangci.yml with explanatory comment
- [ ] RED experiment: firing output captured, constant fully reverted (`git diff --stat internal/comm/wsclass/` empty)
- [ ] Final linter run clean
- [ ] CI path verified (config used, version supports exhaustive) or BLOCKED report with versions
- [ ] No `nolint` on the WSClass switch; no unrelated linter changes; no toolchain upgrades

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] .golangci.yml diff minimal + commented; scoping (if any) narrowest-that-works
- [ ] RED experiment evidence present; wsclass.go byte-identical to committed state
- [ ] WSClass switch demonstrably covered
- [ ] CI wiring confirmed or honestly reported as unverifiable locally

Output: APPROVED or specific gaps with file + line references.

## Notes

- Small leaf by design. Do not fix unrelated lint findings, enable other linters, or touch toolchain versions. Report them; the orchestrator decides.
- If golangci-lint is not installed locally: do NOT install it (standing rule - no installs without explicit user OK). If a pinned version exists in the module cache you may `go run` it; otherwise verify config by inspection, report BLOCKED for the local-run portion, and note CI will exercise the gate.
