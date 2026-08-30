# Docs + invariants consolidation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Do NOT commit — the orchestrator handles all
> git operations after review. This leaf is docs-plus-light-code: the only
  code is a connectivity-graph annotation if needed and the AGENTS.md
  invariant entry.

## Meta

- **Parent:** ../master.md
- **Scope:** Documentation for the feature across the required doc roots,
  the AGENTS.md invariant entry for the new bus topic + WS classification,
  the connectivity-graph annotation for agent.quota_wait, and config
  reference docs.
- **Dependencies:** all (10 consolidates what 01-09 built)
- **Estimated Context:** 30K
- **Concurrency Group:** G (last)

## Goal

The feature is documented where AGENTS.md requires it: feature docs map to
`docs/workflows/<pkg>.md`, config gets reference docs, and the new
cross-boundary contract (bus topic -> WS classification) lands in AGENTS.md
Critical Invariants so future agents don't break it.

## Context

AGENTS.md requires: every commit reviews it; new bus topics + WS event
classification are Critical Invariants; `internal/<pkg>` changes map to
`docs/workflows/<pkg>.md`; `docs/generated/` is auto-generated (never hand-
edit — `make graphs` regenerates; the generator's ANNOTATED_ORPHANS table in
`scripts/gen-connectivity-graph.py` documents external-only surfaces).

Key files to understand before implementing:

- `AGENTS.md` — Critical Invariants section (WS classification entry).
- `docs/workflows/` — existing per-package workflow docs.
- `docs/configuration/` — config reference docs.
- `scripts/gen-connectivity-graph.py` — how topics are documented/annotated.

## Tasks

### Task 1: Workflow docs

**Objective:** Feature docs in the required locations.

**Files:**
- Create or modify: `docs/workflows/llm.md` (quota recognition + broker
  blocking + direct-call wait: the internal/llm slice)
- Create or modify: `docs/workflows/agent.md` (agent states, episode
  tracker, dispatcher deferral/auto-resume: the internal/agent slice)

**Step 1: Read current content**

Read the existing docs (if they exist) and the implemented code (search_files
for QuotaResetError / QuotaEpisodeTracker / quota_blocked usage sites) to
document what IS, not what was planned.

**Step 2: Confirm old state**

Note what the docs said before (for the leaf report).

**Step 3: Write/rewrite content**

Concise sections: error classification table (429/402 -> QuotaResetError,
structured fields + headers only), block semantics (per-entry +
per-credential-key, MaxWait/DefaultEstimate), wait-once decorator for direct
calls, agent state machine (quota_wait/blocked + escalation ladder),
deferral/auto-resume flow, notification dedup. Include the config keys.

**Step 4: Verify cross-references resolve**

All file paths and config keys mentioned exist.

### Task 2: Config reference

**Objective:** llm.quota_retry documented.

**Files:**
- Create or modify: `docs/configuration/` file covering LLM options (find
  where budget/token-budget options are documented — token-budgets doc
  exists under docs/configuration/)

**Step 1: Read current content**

Find the LLM config reference doc.

**Step 2: Confirm old state**

**Step 3: Write/rewrite content**

Document the four keys: enabled (default true), max_wait (24h),
default_estimate (1h), defer_check_interval (10m). JSON5 quoted-key example.
State the default-retry posture explicitly: billing-shaped 402s retry too.

**Step 4: Verify cross-references resolve**

### Task 3: AGENTS.md invariant + topic annotation

**Objective:** The cross-boundary contract is invariant-documented.

**Files:**
- Modify: `AGENTS.md` (Critical Invariants: add an entry after the WS
  classification invariant: agent.quota_wait classifies as agent_progress;
  never chat_message — payload keys list)
- Modify (if needed): `scripts/gen-connectivity-graph.py` ANNOTATED_ORPHANS
  ONLY IF `make graphs` flags agent.quota_wait as an orphan after
  registration exists (check by running `make graphs-check`; if clean, do
  not touch the script)
- Run: `make graphs` and include regenerated `docs/generated/` files if the
  topic map changed

**Step 1: Read current content**

**Step 2: Confirm old state**

**Step 3: Write/rewrite content**

**Step 4: Verify cross-references resolve**

Run `make graphs-check` clean; grep AGENTS.md for the new invariant.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] Docs describe the IMPLEMENTED behavior (verified against code, not
      the plan)
- [ ] `make graphs-check` passes
- [ ] AGENTS.md invariant entry present and accurate

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Workflow docs exist for llm + agent slices
- [ ] Config reference covers all four keys with defaults
- [ ] AGENTS.md invariant entry matches the implemented classification
- [ ] Generated docs regenerated via make graphs (not hand-edited)
- [ ] No contradictions with implemented code

Output: APPROVED or list of specific gaps.

## Notes

- Read the code before writing docs. Where the implementation deviated from
  the plan (leaves record deviations), document the IMPLEMENTED behavior.
- Keep doc additions tight: this is reference documentation, not a tutorial.
