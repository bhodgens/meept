# Claim-Pinning Documentation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> This is a COMMENT-ONLY leaf. Do NOT commit — the orchestrator handles
> git operations after review. Do NOT use read_file on existing source
> files — explore with terminal cat/sed -n. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Document that job.AgentID is a soft routing preference, not exclusivity.
- **Dependencies:** none
- **Estimated Context:** 10K
- **Concurrency Group:** A

## Goal

`job.AgentID` targeting communicates intent, not exclusivity: any unpinned
worker can claim a pinned job because the unpinned claim query has no agent
filter, and persona rides the job payload so this is safe. The ORDER BY at
store.go:408 (`CASE WHEN agent_id = ? THEN 0 ELSE 1 END`) suggests
exclusivity it does not have. Fix with two comment blocks. Zero code
change.

## Context

- internal/queue/store.go:380-423: ClaimNextForAgent. The `agentID != ""`
  branch filters `(agent_id IS NULL OR agent_id = '' OR agent_id = ?)` and
  orders pinned-first. The `else` branch has NO agent filter at all.
- internal/queue/job.go:120-123: WithAgentID setter.
- Do NOT touch any SQL, identifiers, or code. Comments only.

## Interface Contracts (From Parent)

None (no code surface).

## Tasks

### Task 1: Comment block above ClaimNextForAgent's query construction

**Files:**
- Modify: internal/queue/store.go (comments only, above the `// Build query with optional agent filtering` block)

Add (adjust wording to match surrounding voice):

```
// Claim semantics: job.AgentID is a SOFT PREFERENCE, not exclusivity.
// A claiming worker with a non-empty agentID prefers its own jobs (the
// ORDER BY pins them first) but can still claim unassigned jobs; a
// worker with an EMPTY agentID has no agent filter at all and may claim
// ANY pending job, including pinned ones. This is deliberate: pool
// workers are generic executors, the agent persona (model, tools,
// prompt) rides the job payload and is resolved by the job processor,
// so any worker can run any job. Do not "fix" the empty-agentID query
// to skip pinned jobs without also changing the pool's construction
// (internal/worker/pool.go AddWorker starts every worker with an empty
// agentID).
```

### Task 2: Shorter comment on WithAgentID

**Files:**
- Modify: internal/queue/job.go (comment only, on WithAgentID)

Extend/replace the existing comment to state: sets the target agent as a
soft scheduling preference — pinned jobs sort first for that agent's
workers, but any unpinned worker may still claim the job; see
ClaimNextForAgent in store.go.

## Self-Verification Checklist

- [ ] Comments only — `git diff` shows zero non-comment changes
- [ ] Both files gofmt-clean
- [ ] No reflowing of untouched lines

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] git diff is comment-only in the two files
- [ ] Claims in the comments match actual code behavior (verified above)

## Notes

- Keep each block under ~15 lines; dense over long.
