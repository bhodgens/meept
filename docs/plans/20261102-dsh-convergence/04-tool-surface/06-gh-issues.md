# 06 GitHub Issues (image-offload, tmux/time-context) - Docs/Action Leaf

> **For the implementing agent:** You are the writer for this DOCS-ONLY leaf.
> NO CODE. You produce two GitHub issue body documents (markdown) describing
> items 11 (image offload) and 12 (tmux/time-context) from the 2026-11-02
> dsh-vs-meept audit closeout. The ORCHESTRATOR files these issues at branch
> Phase 2 (user decisions D2/D9, master Open Questions #1) — you do NOT open
> issues and you do NOT commit. There are no tests for this leaf; verification
> is that both bodies exist at the exact paths, are grounded in the audit, and
> contain no code changes. Do NOT use read_file on source beyond what the
> audit already provides; write each body once and stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Two GitHub issue bodies for item 11 (image offload) and item 12
  (tmux/time-context). Docs/action only; NO code, NO git, no issue creation.
- **Dependencies:** none
- **Estimated Context:** 10K
- **Concurrency Group:** A (may dispatch in Phase 0 alongside the test leaf)
- **Audit references:** master.md decisions D2/D9 + Open Questions #1; the
  dsh convergence root context (the items are engine gaps that the user
  explicitly declined to turn into implementation leaves, making them GitHub
  issues instead).

## Goal

Capture items 11 and 12 as actionable, well-formed GitHub issue bodies the
orchestrator can file verbatim. Each body states the gap, why it was deferred
to an issue (user decision), the desired behavior, and acceptance criteria —
grounded in the audit's honest picture of meept's current surface, NOT invented
requirements. Writing guide: GitHub-flavored markdown, a clear title, a
problem/why section, an expected-behavior section, and checkable acceptance
criteria. Keep both bodies focused; an issue body is 20-40 lines, not a design
doc.

## Context

- **Item 11 - image offload:** the dsh-vs-meept closeout listed image handling
  / multimodal media surface as a meept gap. Absent a shipped vision/model
  image-input surface, media uploads and images in prompts cannot reach a
  model in a user-shaped way. The issue captures the gap and the acceptance
  target without prescribing an engine — it is a backlog item, per user D2.
- **Item 12 - tmux/time-context:** session context lacks a persistent tmux-like
  terminal session context and a time-context affordance (current time /
  elapsed / schedule awareness) in the model-facing context. MeEPH's engine has
  a PTY manager (`internal/pty`, PTYSessionConfig in interface.go) and rich TUI
  but no model-facing tmux-style multi-window session tool and no
  explicit-time context block. The issue frames the gap + desired surface as a
  backlog item per user D2.
- Grounding: keep every claim here checkable against the repo (the audits at
  /tmp/dsh-audit/ are the read point). Do not assert capabilities meept does
  not have; the issue PREMISES are "this is absent today", which is exactly why
  it is a backlog issue.

## Verified Anchors

- /tmp/dsh-audit/runtime-safety.md:186-190 - computer-use / PTY-adjacent (per-session automation) precedent for the tmux framing
- internal/tools/interface.go:236-244 - PTYSessionConfig + PTYTool (the engine meept HAS, the ISSUE builds on for item 12)
- internal/tui/ - rich TUI with a PTY session surface (item 12 already-partially-covered caveat)
- master.md D2/D9 + Open Questions #1 - the "become GitHub issues, not leaves" decision

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// 04-tool-surface/06-gh-issues/issue-11-image-offload.md   (NEW)
// 04-tool-surface/06-gh-issues/issue-12-tmux-time-context.md (NEW)
// Both: GitHub-flavored markdown, title + problem + expected behavior +
// acceptance criteria. NO code diff, NO repo file changes beyond these two
// bodies.
```

### What This Leaf Consumes

- The audit's honest picture of meept's current surface (only to frame the
  gap as absent-today). Consumers: the orchestrator at branch Phase 2 files
  both issues (user decision D2/D9); master Open Questions #1 records the act.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|--------------|
| T1 | Issue body: item 11 image offload | Create 06-gh-issues/issue-11-image-offload.md | cross-ref check (see Self-VC step) | (n/a - no tests) | PASS: a valid GH-flavored body exists with title, problem, expected behavior, acceptance criteria | Given the gap item 11; When the body is written; Then it is complete, grounded, and copy-paste-ready for the orchestrator to file |
| T2 | Issue body: item 12 tmux/time-context | Create 06-gh-issues/issue-12-tmux-time-context.md | cross-ref check | (n/a - no tests) | PASS: body exists, grounded, frames the tmux-style session tool + time-context gap, and notes meept's existing PTY manager as a foundation | Given the gap item 12; When the body is written; Then it is complete, grounded, and copy-paste-ready for the orchestrator to file |

## Self-Verification Checklist

- [ ] Both issue bodies exist at the exact paths above
- [ ] Each body: title (# heading), Problem/Why, Expected Behavior, Acceptance Criteria sections
- [ ] Grounded - every premise matches the audit/repo picture (no invented capabilities, no claiming something is absent when meept has it)
- [ ] Item 12 body honestly notes meept's existing PTY manager (interface.go:236) as the reuse foundation
- [ ] NO code, no test files, no repo edits beyond the two bodies
- [ ] Cross-refs resolve (no dangling links to the two bodies elsewhere in this tree)

**DO NOT COMMIT.** The orchestrator files the issues and handles all git after review.

## Completion Report Format

1. **Files touched:** [the two issue-body paths]
2. **Verify step:** list the issue titles + a one-line summary of each body's acceptance criteria
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Both bodies present at exact paths; GH-flavored markdown; title + problem + expected behavior + acceptance criteria
- [ ] Grounded in the audit (file:line referenced where reasonable); no fabricated requirements
- [ ] Honest about meept's existing engine (PTY manager exists) - the issue frames a surface gap, not a "nothing exists" falsehood
- [ ] NO code; NO git commit; NO issue actually created (orchestrator does that at Phase 2)

Output: APPROVED or specific gaps with file + line references.

## Notes

- These are BACKLOG items, deliberately NOT implementation leaves (user D2).
  The bodies should make the eventual implementer's job obvious but must not
  enlarge scope - keep each issue to one product outcome.
- The orchestrator (main model) files them via the `gh` CLI at Phase 2, records
  the issue numbers in master's Completion Tracking Table, and updates the
  "gh-issues (11,12)" row to COMPLETE. This leaf stops at producing the bodies.
