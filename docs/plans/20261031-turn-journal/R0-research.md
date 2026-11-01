# Durable-Execution Research Findings - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. This is a RESEARCH leaf: the deliverable is a
> pinned findings document produced from direct web research, not code. Do NOT
> commit - the orchestrator handles all git operations after review. Do NOT
> use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and
> stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Produce the pinned durable-execution research findings doc that
  every later leaf of this tree CITES.
- **Dependencies:** none (first leaf of the tree; FIRST per the tree charter)
- **Estimated Context:** 30K
- **Concurrency Group:** A
- **Audit references:** master.md Requirements R3, R10

## Goal

Pin the external evidence for this tree's design decisions in one citable doc:
why a DB-embedded journal (the DBOS / SQLite execution-log family) is the right
pattern for a single-node SQLite daemon, why the Temporal SDK / external
workflow server is rejected, and what the determinism and
quarantine-of-side-effects lessons imply for a turn-level step ledger. Later
leaves cite this doc by path when a finding drove a code decision.

## Context

- Research method: direct web_search NOW (this session), then write the doc
  from what the searches actually returned - every cited URL must come from a
  search result you saw, never recited from memory.
- The daemon is a single Go process over local SQLite stores (parks.db,
  effects.db): see internal/agent/park_store_sqlite.go and the parks.db
  construction in internal/daemon/components.go:2610.
- A prior Temporal proposal was REJECTED; this doc records WHY in citable
  terms so the rejection survives personnel changes.

## Verified Anchors

<!-- Exact, grep-confirmed at authoring time. No approximate pointers. -->

- docs/agents/opt-in-defaults.md - the opt-in-defaults rule the rollout
  implication (D3) must respect.
- internal/agent/park_store_sqlite.go:121-129 - the existing WAL DSN +
  best-effort-open conventions the research compares the journal against.
- internal/queue/queue.go:491,539 - CompleteAttempt / CompletionIsFresh: the
  existing dedup mechanism the idempotency findings (F4/F5) must relate to.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
docs/research/turn-journal-findings.md - the pinned findings doc with one
section per finding ID (F1-F6) and one per design implication (D1-D3), each
citing >= 1 source URL gathered this session. Later leaves cite it by path;
the IDs freeze once this leaf is REVIEWED.
```

### What This Leaf Consumes

- None (research leaf; consumes only web sources and ../master.md).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Survey the durable-execution landscape via web search (Temporal, Restate, DBOS, Inngest, Resonate, SQLite execution-log engines, event-sourced LLM-agent runtimes) and capture >= 6 distinct authoritative source URLs | none (notes in memory) | n/a (input task) | n/a | >= 6 distinct sources with URLs captured from live searches | Given the research question; When web searches run on durable execution + LLM agent journals; Then at least 6 distinct sources with URLs are captured |
| T2 | Write the findings doc: sections F1-F6 and D1-D3, each with evidence and >= 1 source URL | Create: docs/research/turn-journal-findings.md | `grep -c '^## F[1-6]' docs/research/turn-journal-findings.md` returns 6 AND `grep -c '^## D[1-3]' docs/research/turn-journal-findings.md` returns 3 | doc missing or sections fewer than required | 6 finding sections + 3 design-implication sections, each with sources | Given T1 sources; When the doc is written; Then each F/D section states the finding, the evidence, and >= 1 URL |
| T3 | ASCII + citation audit of the doc | docs/research/turn-journal-findings.md | `LC_ALL=C grep -nP '[^\x00-\x7F]' docs/research/turn-journal-findings.md` returns no matches | non-ASCII present (pre-commit gate) | pure ASCII | Given the written doc; When the non-ASCII scan runs; Then zero matches |

**Required content pins (a finding missing these is incomplete):**

- F1: external-server durable execution (Temporal/Restate) requires running
  and operating a separate server/store beside the app - mismatched for a
  single-node personal daemon over local SQLite.
- F2: DB-embedded durable execution (DBOS on Postgres; the SQLite
  execution-log pattern: flowId + step + status + parameters + return_value,
  PRIMARY KEY (flowId, step)) achieves the same resume guarantee with zero new
  infrastructure - the family this tree's turnjournal.db belongs to.
- F3: LLM calls are non-deterministic, slow, and costly, so completed steps
  must never be re-invoked on recovery - journaled results are reused.
- F4: quarantine of side effects: recorded side-effect results are replayed
  from the log; anything unrecorded (clock, RNG, an unwrapped effect) diverges
  replay - hence this tree's rule that recorded steps are SKIPPED, never
  re-executed, and completion dedup stays with the queue's claim tokens.
- F5: the crash window "executed but result not yet recorded" is unavoidable
  even with a journal; the mitigation is idempotency keys / duplicate-tolerant
  effects, not a narrower window.
- F6: journal growth needs a retention/garbage-collection story (closed runs
  pruned) - an append-only journal without GC grows unboundedly.
- D1: journal at STEP boundaries only (turn granularity), not full
  event-sourcing of every LLM call - sufficient for next-step resume, far
  less state.
- D2: recovery = deterministic control flow + journaled results; the resume
  path must consult the journal, never re-decide from live state alone.
- D3: rollout defaults map to meept's opt-in rule - the
  `daemon.turn_journal_enabled` default-false flag (docs/agents/
  opt-in-defaults.md: flipping a default is a product decision).

**Worked example (canonical row expansion for T2):**

1. Draft each section in the form: `## F1: <finding one-liner>`, 2-4
   sentences of evidence, then `Sources:` with 1-2 URLs from T1.
2. Verify: the T2 grep commands above. Expected: 6 and 3.
3. Cross-check every URL against your T1 capture list; drop anything you
   cannot tie to a search result seen this session.

## Self-Verification Checklist

- [ ] All tasks implemented (findings doc written, audits pass)
- [ ] Interface contract satisfied: doc at the exact path with F1-F6 + D1-D3
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Every finding cites >= 1 resolving URL gathered during THIS session's
      searches (no recited-from-memory URLs)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; verify commands pass
- [ ] Findings doc exists at docs/research/turn-journal-findings.md with
      F1-F6 and D1-D3 sections, each with sources
- [ ] All URLs were actually returned by web searches in this session
      (spot-check 2 at random)
- [ ] Content pins F1-F6 / D1-D3 all present and accurate
- [ ] No scope creep; no debug artifacts; pure ASCII

Output: APPROVED or specific gaps with file + line references.

## Notes

- The findings doc is PARENT-owned after review: leaves 01, 02, 03
  (rehydrate), and 04 cite it, so the finding IDs freeze at REVIEWED. If
  review changes an ID, the orchestrator updates every citation in the same
  commit.
- Keep the doc under ~150 lines; it is a citations-and-implications pin, not
  a survey essay.
