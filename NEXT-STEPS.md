# NEXT-STEPS — plan-compiler live validation (updated 2026-09-07, run 3)

## Status: revision loop VERIFIED live; two residual bugs filed below

Run 3 (task-20260907193218.799477000-0001) with both fixes deployed:

- Rejection now STICKS: transitions log shows `reviewing → rejected` with no
  bounce-back (commit 00551f63 verified).
- Revision chains SCHEDULE: `-1001-*` revision steps went pending → ready →
  scheduled → approved through PromoteReadySteps' rejected-dep exception
  (commit 2ac27b1f verified).
- No stranded rows: 30 approved, 1 completed, 1 failed across 32 steps —
  every rejection spawned a revision that ran and finished.

## Run 4 (2026-09-07 20:22) — counters verified fixed

task-20260907202216.088999000-0001 (commit 02e952f9 deployed):
- Finalized with truthful counters: 6 total / 5 completed / 1 failed,
  83% progress. The 200%-drift bug is gone (RecountJobs + atomic
  increments working).
- The single failure was an Agnes 429 quota step — correct honest
  completion, no counter lies.

## Residual bugs (both still open)

## Residual bug A — task finalizes `failed` despite all steps resolving

"Task finalized ... status=failed" fired while 30/32 approved + 1 completed
+ 1 failed (that one failed step's revision ran and was APPROVED). The
finalization sweep (tactical.go ~:1420) sees `hasFailures`/failed rows but
does not consult the revision chain — AreAllCompleted (step.go:892) already
encodes "rejected resolves via successful revision", but the failed-step
analog ("failed resolves when its approved revision exists") is missing, and
the failed row from BEFORE a revision was created is never revisited.
Observed: state=failed, progress 200% (2/1 jobs) — counter drift too.

## Residual bug B — Agnes HTTP 400 "missing field content" (upstream)

Every run loses 1-2 steps to Agnes's intermittent 400
`messages[2]: missing field 'content' at line 1 column 15182` on handoff
continuations. Upstream (models.json5 agnes/agnes-2.5-flash); workaround =
paid tier / stronger planner model, already documented.

## Remaining observability note

Steps executed by the `chat` agent ignored the sealed plan's phase work and
answered "ask me to do something specific" (agent=chat per job log). The
planner model never produced the actual haiku. Likely the same Agnes-400
degradation, but worth a dedicated look at step→agent routing for sealed
plans (should follow tool_hint, not default to chat).

## Fixes deployed this session (both committed)

1. 00551f63 — review rejection persists rejected state (one-write); no
   revisions for execution-failed steps.
2. 2ac27b1f — PromoteReadySteps lets revision steps pass rejected deps.

## Cleanup

- /tmp/draft/*.md scratch files can be deleted.

