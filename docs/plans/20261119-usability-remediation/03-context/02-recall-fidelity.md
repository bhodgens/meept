# Recall Fidelity - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T4/T5) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Follow-up answers reflect the session's actual work and state
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 70K
- **Concurrency Group:** A
- **Audit references:** W10; F-ORCH-16/24; F-NEW-7 (recall improved but
  decomposition prose still leaks); F-NEW-9 (missing-file read answered
  with a clarification question - orchestrator step lost the file path
  from the prompt; completed without answering); recall_answer.go;
  I5 scope check

## Goal

Fix the recall lane: explicit failed/cancelled/rejected state handling
(never "still in progress" for terminal failures); session task-set
coverage for plural artifact questions; fact questions route to
memory_search instead of task_get decomposition; verify memory scope on
the recall path (cross-session bleed check).

## Context

- internal/agent/recall_answer.go:41-92 - RecallAnswer; :49 digest
  (LastTask only); :86-90 state switch default = in-progress text.
- internal/agent/dispatcher.go buildSessionContextDigestExcluding ~3875 -
  digest builder; RecallAnswer call site ~3235-3257; platform_recall_
  arbitration ~1725.
- internal/memory - Manager search surfaces WithScopeFilter;
  EffectiveMemoryScope (agent-groups ceiling); memory_search tool.
- internal/agent/dispatcher.go shouldCreateTask gate ~2527 - fact questions
  currently create quickplan tasks that decompose into task_get.

## Verified Anchors

- internal/agent/recall_answer.go:86 - state switch
- internal/agent/recall_answer.go:49 - digest call
- internal/agent/dispatcher.go:3875 - digest builder (re-grep)

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Recall answers: terminal-failure states named plainly; task-set digest
  (bounded cap, e.g. last 10 tasks + artifact list); fact questions answered
  from memory_search with scope filter; C1 reply_source="recall".

### What This Leaf Consumes
- C4 (session identity for scope), C1 (reply_source).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | State switch fidelity | Modify: internal/agent/recall_answer.go:86-90 | `go test ./internal/agent/ -run TestRecallFailedState -count=1` | gate T4 RED (failed->in-progress) | GREEN | Given failed prior task; Then "failed: <reason>" plainly; completed/approved unchanged; non-terminal keeps in-progress text |
| T2 | Task-set digest | Modify: internal/agent/dispatcher.go digest builder; recall_answer.go rendering | `go test ./internal/agent/ -run TestRecallTaskSet -count=1` | gate T4 RED (last-task-only) | GREEN | Given session with 3 tasks incl. hello.txt creation; When "what files did you make"; Then answer lists the session's artifacts (bounded), not one unrelated task |
| T3 | Fact->memory routing + scope check + prompt fidelity | Modify: internal/agent/dispatcher.go recall arbitration (fact-shaped questions -> memory_search lane, no task creation); verify EffectiveMemoryScope applied; orchestrator step input carries the FULL user prompt (F-NEW-9: file path lost -> clarification question) | `go test ./internal/agent/ -run 'TestRecallFactToMemory|TestStepPromptFidelity' -count=1` | gate T5 RED (task_get decomposition); step lost file path | GREEN | Given planted ZEBRA-4417 in session A; When A asks codeword; Then memory_search answers; session B query does NOT return A's scoped fact (I5 evidence); given missing-file read prompt with explicit path; Then step receives the path, answers plainly (never a clarification question for info already in the prompt) |

## Self-Verification Checklist

- [ ] Gate T4/T5 GREEN; gate diff empty
- [ ] Live-gate: rig p1 T3/T4 + p2 A3/B probes - correct answers, no bleed
- [ ] I5 verdict recorded (scope leak confirmed/refuted with evidence)
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate + I5 evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Digest bounded (never unbounded history dump into prompt)
- [ ] Recall never fabricates artifacts not in the task set/memory
- [ ] Scope filter applied at the recall call site (not bypassed)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- If I5 proves the leak is in EffectiveMemoryScope wiring (not recall),
  STOP widening scope - report and open a follow-up leaf per branch
  Open Questions.
