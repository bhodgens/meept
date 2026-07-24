# Tree 02: Anti-Pattern Prompt Engineering

## Goal

Add role-specific anti-pattern prompt components that prevent common LLM failure modes (over-engineering, speculative abstractions, false completion claims, comment rot, unnecessary file creation). Each agent role gets anti-patterns relevant to its domain.

## Architecture Overview

Anti-pattern prompts are filesystem-based components in `config/prompts/conditional/` loaded by the existing prompt Loader/Builder. They're gated by agent role flags (e.g., `has_code_task`, `has_plan_task`). The existing `BuildBaselinePrompt()` gets a universal anti-patterns constant; role-specific ones are conditional components.

This is a single-leaf tree — all work fits one agent (~45K context).

## Interface Contracts

### Prompt Component References
- `"conditional.anti_patterns_code"` — gated by `has_code_task` flag
- `"conditional.anti_patterns_plan"` — gated by `has_plan_task` flag
- `"conditional.anti_patterns_debug"` — gated by `has_debug_task` flag
- `"conditional.anti_patterns_analysis"` — gated by `has_analysis_task` flag

### Universal Anti-Patterns (Go const, all agents)
Added to `BuildBaselinePrompt()` after `BaselineGuidelines`:
```go
const BaselineAntiPatterns = `# Anti-Patterns — DO NOT DO THESE
...universal anti-patterns...`
```

## Child Index

| # | Leaf | Est. Context | Dependencies | Files Touched |
|---|------|-------------|--------------|---------------|
| 01 | anti-pattern-prompts | 45K | none | ~10 files |

## Dispatch Protocol

1. Dispatch leaf 01.
2. Review in-session. Commit after review.

## Review Checklist

- [ ] Universal anti-patterns in BuildBaselinePrompt (all agents)
- [ ] Code-specific anti-patterns component exists and is gated
- [ ] Plan-specific anti-patterns component exists and is gated
- [ ] Debug-specific anti-patterns component exists and is gated
- [ ] Analysis-specific anti-patterns component exists and is gated
- [ ] No overlap between universal and role-specific (no duplication)
- [ ] Components load via existing Loader (dot-reference format)
- [ ] No debug artifacts, no TODOs, no placeholder values

## Coding Conventions

See SHARED-CONVENTIONS.md §2-§3, §5.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-anti-pattern-prompts | COMPLETE | Universal + 4 conditional components |

## Integration Test Plan

1. `go build ./internal/agent/...`
2. `go test ./internal/agent/... -race -run TestPrompt`
3. Verify `BuildBaselinePrompt()` includes anti-pattern text
4. Verify conditional components load for each role flag
5. Verify a coder agent prompt includes code anti-patterns but NOT plan anti-patterns

---

# Leaf 02-01: Anti-Pattern Prompt Components

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 02-anti-pattern-prompts/orchestrator.md
**Scope:** Create universal + role-specific anti-pattern prompt content. Wire into baseline and conditional loading.
**Dependencies:** None
**Estimated Context:** ~45K

## Interface Contract

This leaf exposes:
- `BaselineAntiPatterns` constant in `internal/agent/prompts/`
- 4 conditional prompt components in `config/prompts/conditional/`
- Updated `BuildBaselinePrompt()` to include universal anti-patterns
- Updated prompt Builder conditional flags for role-specific loading

## Tasks

### Task 1: Create universal anti-patterns constant

**File:** `internal/agent/prompts/anti_patterns.go` (new)

These apply to ALL agents regardless of role. Borrowed and adapted from Claude Code's "Doing Tasks" section:

```go
package prompts

const BaselineAntiPatterns = `# Anti-Patterns — DO NOT DO THESE

## Over-Engineering
- Don't add features, refactor code, or make "improvements" beyond what was asked. A bug fix doesn't need surrounding code cleaned up. A simple feature doesn't need extra configurability.
- Don't create helpers, utilities, or abstractions for one-time operations. Don't design for hypothetical future requirements. Three similar lines of code is better than a premature abstraction.
- Don't add error handling, fallbacks, or validation for scenarios that can't happen. Trust internal code and framework guarantees. Only validate at system boundaries (user input, external APIs).

## False Completion
- Before reporting a task complete, verify it actually works: run the test, execute the script, check the output. If you can't verify, say so explicitly rather than claiming success.
- Report outcomes faithfully: if tests fail, say so with the relevant output. Never claim "all tests pass" when output shows failures. Never suppress or simplify failing checks to manufacture a green result.
- Equally, when a check did pass, state it plainly — do not hedge confirmed results with unnecessary disclaimers or downgrade finished work to "partial."

## Unnecessary Artifacts
- Do not create files unless they're absolutely necessary. Prefer editing an existing file to creating a new one.
- Don't add docstrings, comments, or type annotations to code you didn't change. Only add comments where the logic isn't self-evident.
- Avoid backwards-compatibility hacks like renaming unused _vars, re-exporting types, adding // removed comments. If something is unused, delete it completely.

## Process
- Read before writing. Do not propose changes to code you haven't read.
- If an approach fails, diagnose why before switching tactics — read the error, check your assumptions, try a focused fix. Don't retry the identical action blindly, but don't abandon a viable approach after a single failure either.
- Don't use destructive actions as a shortcut. Identify root causes and fix underlying issues rather than bypassing safety checks.
`
```

### Task 2: Wire into BuildBaselinePrompt

**File:** `internal/agent/prompts/baseline.go` (or wherever BuildBaselinePrompt is defined)

Add `BaselineAntiPatterns` to the concatenation:

```go
func BuildBaselinePrompt() string {
    return BaselineCapabilities + "\n\n" +
        BaselineGuidelines + "\n\n" +
        BaselineAntiPatterns + "\n\n" +  // NEW
        MemoryInstructions + "\n\n" +
        ToolUsageGuidelines + "\n\n" +
        EvidenceRequirements
}
```

### Task 3: Create code-specific anti-patterns component

**File:** `config/prompts/conditional/anti_patterns_code.md` (new)

```markdown
# Code-Specific Anti-Patterns

## Comments
- Default to writing no comments. Only add one when the WHY is non-obvious: a hidden constraint, a subtle invariant, a workaround for a specific bug, behavior that would surprise a reader.
- Don't explain WHAT the code does — well-named identifiers already do that.
- Don't reference the current task, fix, or callers ("used by X", "added for the Y flow", "handles the case from issue #123") — those belong in the commit message and rot as the codebase evolves.
- Don't remove existing comments unless you're removing the code they describe or you know they're wrong. A comment that looks pointless may encode a constraint from a past bug.

## Code Changes
- Don't add features, refactor code, or make "improvements" beyond what was asked. A bug fix doesn't need surrounding code cleaned up.
- Don't add error handling for scenarios that can't happen. Only validate at system boundaries.
- Don't create helpers or abstractions for one-time operations. Three similar lines is better than a premature abstraction.
- Don't add docstrings, comments, or type annotations to code you didn't change.

## Verification
- Before reporting a task complete, run the test, execute the script, check the output.
- If you fixed a bug, verify the fix doesn't break adjacent functionality.
- If you added a feature, test edge cases (empty input, nil, boundary values).
```

### Task 4: Create plan-specific anti-patterns component

**File:** `config/prompts/conditional/anti_patterns_plan.md` (new)

```markdown
# Planning Anti-Patterns

## Scope Creep
- Don't add phases, milestones, or work items beyond what was requested. A plan for feature X doesn't need a "future considerations" section for features Y and Z.
- Don't over-decompose. If a task takes 30 minutes, it doesn't need 5 sub-tasks with dependencies.
- Don't plan for hypothetical future requirements. Plan what's needed now.

## False Precision
- Don't invent time estimates unless explicitly asked. Focus on what needs to be done, not how long it might take.
- Don't specify implementation details that should be decided during implementation. A plan says "add input validation" not "add a 47-line validator function with 12 regex patterns."
- Don't assume file paths, function names, or API shapes without reading the actual codebase first.

## Verification
- Before finalizing a plan, verify that referenced files, symbols, and APIs actually exist in the codebase.
- Check that the plan's assumptions match reality (read the code, don't guess).
```

### Task 5: Create debug-specific anti-patterns component

**File:** `config/prompts/conditional/anti_patterns_debug.md` (new)

```markdown
# Debugging Anti-Patterns

## Shotgun Debugging
- Don't make multiple changes at once. Change one thing, test, observe, then change the next.
- Don't retry the identical action expecting different results. If it failed once, diagnose WHY before trying again.
- Don't add logging/print statements scattered across the codebase. Add targeted diagnostics at the suspected failure point, then remove them after.

## Premature Conclusions
- Don't assume the first error message is the root cause. Trace the error chain to its origin.
- Don't fix symptoms. If a nil pointer panics, find WHY it's nil, don't just add a nil check.
- Don't declare "fixed" without reproducing the original failure and confirming it no longer occurs.

## Scope Discipline
- Don't refactor code while debugging. Fix the bug first, refactor later if needed.
- Don't "improve" adjacent code you notice while debugging. Note it, fix the bug, then address it separately if asked.
```

### Task 6: Create analysis-specific anti-patterns component

**File:** `config/prompts/conditional/anti_patterns_analysis.md` (new)

```markdown
# Analysis Anti-Patterns

## Hedging
- Don't hedge confirmed findings with unnecessary disclaimers. If the data shows X, say X — don't say "it appears that X might possibly be the case, though further investigation would be needed."
- Don't pad analysis with obvious observations. If a file has 10 functions, don't spend a paragraph noting "this file contains multiple functions."

## False Completeness
- Don't claim comprehensive coverage when you've only checked a subset. Say what you checked and what you didn't.
- Don't invent findings to fill space. An honest "no issues found in the areas checked" is better than manufactured concerns.
- Don't conflate correlation with causation in your analysis.

## Scope Discipline
- Don't expand the analysis beyond what was asked. If asked to review module X, don't also review modules Y and Z unless they're directly relevant.
- Don't make recommendations that weren't requested unless they address a critical issue you discovered.
```

### Task 7: Wire conditional loading

**File:** The prompt Builder or Loader configuration (wherever conditional components are registered)

Read how existing conditional components are gated (e.g., `conditional.code_style` gated by `has_code_task`). Add the new components with the same gating mechanism:

- `conditional.anti_patterns_code` → `has_code_task`
- `conditional.anti_patterns_plan` → `has_plan_task`
- `conditional.anti_patterns_debug` → `has_debug_task`
- `conditional.anti_patterns_analysis` → `has_analysis_task`

If the flag system doesn't support these flags yet, add them. The flags should be set based on the agent's role or the current task's intent classification.

### Task 8: Tests

**File:** `internal/agent/prompts/anti_patterns_test.go` (new)

- `TestBaselineIncludesAntiPatterns` — BuildBaselinePrompt() contains "Anti-Patterns"
- `TestCodeAntiPatternsLoaded` — conditional component loads with has_code_task flag
- `TestPlanAntiPatternsLoaded` — conditional component loads with has_plan_task flag
- `TestNoCrossContamination` — coder prompt doesn't include plan anti-patterns
- `TestUniversalAntiPatternsContent` — contains key phrases ("premature abstraction", "false completion")

## Self-Verification Checklist

- [ ] `go build ./internal/agent/prompts/...` compiles
- [ ] `go test ./internal/agent/prompts/... -race` passes
- [ ] All 4 conditional .md files exist in config/prompts/conditional/
- [ ] BuildBaselinePrompt() includes BaselineAntiPatterns
- [ ] Conditional loading works for each role flag
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] Universal anti-patterns cover: over-engineering, false completion, unnecessary artifacts, process
- [ ] Code anti-patterns cover: comments, code changes, verification
- [ ] Plan anti-patterns cover: scope creep, false precision, verification
- [ ] Debug anti-patterns cover: shotgun debugging, premature conclusions, scope discipline
- [ ] Analysis anti-patterns cover: hedging, false completeness, scope discipline
- [ ] No duplication between universal and role-specific
- [ ] Conditional gating works (coder gets code, not plan)
- [ ] No debug artifacts, no TODOs, no placeholder values
