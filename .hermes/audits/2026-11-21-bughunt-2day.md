# Bughunt 2026-11-21 — last 2 days (cc614a38..f04fa5da) + fix wave

Repo: /Users/caimlas/git/meept, branch main. Request: review the last 2 days of commits
for bugs using subagents (bughunt) and correct them. Fixing authorized.

## Anchor

Prior report `.hermes/audits/2026-11-20-bughunt-7day.md` closed at its own fix commits
(1d52b8f8..cc614a38). This wave audits everything after: 24 commits (job-tools tree,
routing funnel, shell-guard seam deletion, determinism sweep), plus an adversarial
regression review of the prior wave's own fixes (never independently reviewed).

## Baseline

`go build ./...` green. `go test -p 2 -count=1` over the 9 touched packages: 9 ok, 0 FAIL.
Recorded before any auditor or fixer dispatch, so every later red is attributable.

## Auditors (4, read-only, disjoint scopes, 52 min)

1. Job-tools tree (scheduler result lane, shell guard, job tools, permissions).
2. Routing funnel (result-topic stamp, standing session, cron/webhook, projector).
3. Regression review of the prior wave's fix commits + merge resolutions.
4. Test-weakening sweep (determinism commit, voice pins, integration compile).

### Verified findings

HIGH (3, all parent-verified at HEAD before fixing):
- H1 job_start fence escape: the fence stage covered shell_execute only;
  job_start could run any working_dir outside a fenced project boundary.
  pkg/security/permissions.go:630-656.
- H2 cron triggers dead in production: installCronScheduler was called only
  from NewManager(bm); production builds via NewManagerFromConfig ->
  NewManagerWithStores, m.Cron nil. internal/employee/manager.go:777 vs 3776.
- H3 webhook route ran the full goal round synchronously under invokeMu;
  WriteTimeout is 0, so a POST blocked its connection for minutes.
  internal/employee/webhook_trigger.go:63 + server.go:1426.

MEDIUM (11 verified): park-resume bypasses the per-employee invoke mutex;
resumed episodes had empty speak session (notify drop); Cron.Stop/claim
release never ran (jobs fired after bot delete; StopAll->StartAll left the
scheduler dead); 413 path returned 200 empty; GetBot errors read as 404;
LastTurnTokens undercounted multi-call tool loops; acp lane dropped the
constitution prefix; GuardCommand trusted without validation; ErrWaitDelay
misreported success as failed/1; settled-job stop returned hardcoded 0;
spill files unbounded + crash-orphaned.

LOW (9): settled-job stopped=true nit, provider RiskMutating vs RiskPrivileged,
await job_status only in e2e, stale shell_guard comments (x4, fixed), park
resume EmployeeID overwrite edge, silent persist-failure fallback, GetBot body-
read order, process-global transport swap in shadow test, signal test 10s poll.

Regression-review verdicts: prior-wave fix commits 1d52b8f8, 272af4b5, 06bdd865,
be258f4b, cc614a38 CLEAN. 09dbaa7e ONE HOLE: the daemon payload stamp
(bot_trigger_handler.go:55 trigger_ref) had no failing revert — closed this wave.
452994f1 CLEAN except 4 stale comments (closed). Merge resolution 067a4a10 CLEAN.
Determinism sweep CLEAN — no weakened tests; every poll is bounded with Fatalf.

## Fixes (7 commits, all with pins; parent hunk-reviewed each diff)

| Commit | Scope |
|---|---|
| 19a7e2fd | job_start fenced in the permission checker fence stage (H1) |
| d332c2b1 | cron scheduler installed in production; fire-time re-check; unconditional Start; delete/pause/resume/update hooks release/resync claims (H2 + 2 MED) |
| fb0116e3 | webhook route async 202; explicit 413; store-error 500 vs 404 (H3 + 2 MED) |
| b85a41bd | LastTurnTokens reports the accumulated turn total; acp lane arms the constitution prefix (2 MED) |
| cbdcdb45 | park resume arms the standing speak session (MED; mutex half deferred, below) |
| e86b05b7 | guard source derived from the wrapper script; ErrWaitDelay rescued on live ctx; settled-job stop returns the real code; spill cap 64 MiB + boot sweep (4 MED + 1 LOW) |
| 9325aa8f | trigger_ref payload pin (non-vacuity proven by fail-then-restore probe); 4 stale shell_guard comments deleted |

Behavior changes users will see:
- Webhook trigger route returns 202 (was 200). docs/reference/http-api.md still
  documents 200 — needs a doc-only follow-up commit.
- job_start now honors the fence. An agent that previously escaped its project
  boundary through job_start will start getting denied. That is the fix working.
- cron triggers now actually fire in the daemon. This feature was dead since
  5895060f landed it; it is now live. First live-cron deploy should watch for
  surprise rounds from old enabled rows.

## Gate matrix (post-fix)

`go build ./...` green. `go test -p 2 -count=1`: pkg/security, internal/security,
internal/scheduler, internal/employee, internal/bot, internal/daemon,
internal/activity, internal/session all ok. internal/agent has 8 FAILs that are
RED at the wave anchor (proven in a detached worktree at 8b8eff0e^, before my
first commit): sibling usability-sweep RED gates (plan_failure_copy,
reply_guard, threshold, triviality) plus a nil-queue panic in
tactical.go reachable from TestTacticalScheduler_VacuousValidatorPassNotValidated.
Not this wave's regressions; owned by the sibling session's in-flight batch.

## Shared-tree incidents (disclosed in full)

- Mid-wave, a sibling session opened a git merge and stalled it for 3+ hours
  (MERGE_HEAD open; e2e/manifest.json + internal/llm/context_firewall.go in UU;
  ~200 files staged). Fixers were steered to no-git-state; commits were held.
- User chose "resolve it". I inspected both sides of each UU file: the sibling
  had already resolved the content (no markers on disk) but never staged it.
  e2e/manifest.json keeps both sides (tools-jobs + session-storage +
  dispatch-trace). context_firewall.go keeps both: the warm-seam
  BoundSystemMessages refactor plus the truncation disclosure counter/notice/
  idempotence marker, with the counter added in both bound sites so neither
  double-counts. Verified: JSON valid, build green, internal/llm suite ok.
  Merge committed as ae49730c, all 20 pre-commit gates green.
- Two sibling commits landed mid-commit-window (8b8eff0e LLM cost stamps,
  3c0b4790 docs reconcile). One of my commits hit a HEAD-moved race and was
  retried cleanly. Every fix commit's `git show --name-only` verified to
  contain exactly its declared paths — nothing absorbed.
- The sibling remains active post-wave (new dirty files in internal/agent,
  internal/daemon). Their in-flight batch sits uncommitted; the 8 red agent
  tests are theirs to close.

## Deferred / not done (ledger)

1. Park-resume invoke-mutex serialization: needs Manager.AcquireInvokeMutexForResume
   (manager.go) + wrapping the resume callback at components.go:6919-6921. Exact
   patch written by the fixer (in its report, task 1 of deleg_eb0b429f). Not applied:
   manager.go/components.go were outside every fixer's ownership and the sibling
   held staged edits in components.go. The interleave it kills: a quota resume
   landing mid-trigger runs two concurrent rounds for one employee and can
   interleave pairs in the emp-sess transcript.
2. Gate-resume same gap: resumeGoalGateFromDrain (components.go:6921 area) also
   re-enters without SetSpeakContext — same one-line pattern as the park fix.
3. docs/reference/http-api.md still documents the webhook route as synchronous 200.
4. toolprov provider risk class for the scheduler provider (RiskMutating ->
   RiskPrivileged) — posture-reporting only, LOW.
5. job_status await_seconds (server-side bounded wait) — the fix exists only in
   the e2e suite; agent-facing path still polls one turn at a time.
6. tool_job_stop.go:83-92 comment now slightly stale (says "already_done" only).
7. run_result.go TruncatedTo field comment stale (now used for cap + WaitDelay).
8. internal/agent 8 red tests + the tactical.go nil-queue panic: sibling's,
   pre-date this wave's commits, proven at the anchor.
9. Prior wave's NOT CLOSED items carry forward (daily cent cap live path;
   turn_tokens producer — the sibling's in-flight executor.go diff appears to be
   building exactly that producer).

## Verification method

Every HIGH parent-verified at HEAD before dispatch. Every fix commit hunk-read by
the parent, explicit-path commits only, `git show --name-only` checked per commit.
The trigger_ref pin's non-vacuity proven empirically (delete stamp -> FAIL ->
restore -> PASS -> byte-identical diff). The scheduler ErrWaitDelay semantics were
verified empirically against go1.27.3 os/exec before coding. Package gates run with
-count=1. The 8 internal/agent failures attributed to pre-existing state via a
detached-worktree probe at the pre-wave anchor, then the worktree removed.

No push. All work is on local main.
