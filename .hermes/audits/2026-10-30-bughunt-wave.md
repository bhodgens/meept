# meept bughunt wave 2026-10-30 — commit range 2026-10-19..HEAD (152 commits)

Method: 9 read-only auditors (scopes: githubsvc / github builtin tools /
budget-status fix regression / e2e-harness+scripts / window remainder /
security-audit fix-efficacy / repo-wide invariant sweep / 10-19 wave fix
regression / non-Go surfaces), plus parent pre-reads with direct source
verification. Findings files: /tmp/ap-meept-a1..a9/findings.md.
Provenance: [PV] = parent re-read the cited code. [CH] = child-sourced.
[PD] = parent disproved.

Baseline gate (parent): go build ./... exit 0. go test -p 2 -count=1 ./...
= 115 ok + 1 FAIL package (internal/tools/builtin, all failures
`can't assign requested address` = macOS ephemeral-port exhaustion; isolated
re-run GREEN in 62s). Environmental, known class.

## ACCEPTED FINDINGS

### HIGH
- H1 [PV] internal/scheduler/instructions.go:60 — `agent_trigger` jobs
  DISCARD `ActionArgs["prompt"]`: the job prompt is
  `fmt.Sprintf("Execute instruction: %s", instr.Trigger)` = literally
  "Execute instruction: cron:0 3 * * *". The github_ci_wait re-arm sink
  (internal/githubsvc/rearm.go:84-97) writes the resume prompt + wait_id +
  resume_wait_id into ActionArgs, but NO code reads ActionArgs["prompt"]
  (repo-wide grep: zero readers). Blast radius: the parked-CI-wait resume
  feature is dead end-to-end — the cron row fires an agent turn with a
  meaningless prompt, the agent cannot pass resume_wait_id, CancelReArm
  never runs, and the `M H * * *` cron re-fires EVERY DAY at that minute
  forever (the rearm.go comment claims "a one-shot re-check" — false).
  Fix: instructionToJob must use ActionArgs["prompt"] when non-empty,
  falling back to the current text. Pin both shapes.

### MEDIUM
- M1 [PV] internal/daemon/github_wiring.go:112-123 — token-file TOCTOU:
  os.Stat (perm check) then os.ReadFile are not atomic; a writer can swap
  the file between them. Fix: open, fstat the fd, verify owner-only mode,
  read from the fd. (Local-attacker threat model: LOW practical risk, but
  the fix is cheap and the invariant is stated.)
- M2 [CH→PV partial] scripts/audit-secrets-value-callers.py — LOG_CALL /
  ENV_CONCAT regexes miss `fmt.Sprintf("NAME=%s", secretVal)` and
  variable-receiver logger calls; the in-process-only rule gate has a
  bypass shape. Fix: extend patterns for fmt.Sprintf/Println with
  value-bearing args.
- M3 [CH] repo-wide `go func` without recover in daemon long-lived paths
  (health_rpc.go:87, daemon.go:2064/2086, prompt_dirs.go:86,
  evolver_approval_bridge.go:76). One panic kills the daemon. NOT fixed
  this wave: broad sweep, needs a shared recover helper + convention call;
  deferred to a dedicated pass (ledger).

### LOW
- L1 [PV] internal/comm/http/pty_handler.go rejectDangerousEnvKey —
  whitespace-only keys pass the blocklist check (ToUpper("   ") not in
  set). Fix: TrimSpace before both checks.
- L2 [CH] fenceCommandString (pty_handler.go) has no direct test for
  args containing `../` traversal. Fix: pin test.
- L3 [CH] internal/comm/http/server.go legacy BudgetStatusGetter bridge
  path (snapshot getter nil, tuple getter set) untested in
  server_budget_status_test.go. Fix: pin test.
- L4 [CH] Dart GUI reads within_budget but not within_cost_budget
  (chat_input.dart:1258). Product-surface gap, not a bug. Ledger.

## REFUTED / NOT-BUG (parent-verified)
- R1 [PD] A5#3 classifierBoot nil-deref in deferred SignalFailed — the
  defer is INSIDE `if d.components != nil && d.components.Dispatcher !=
  nil` (daemon.go:2011-2023); classifierBoot cannot be nil there.
- R2 [PD] A5#9 GitHubReArmTickerDone never closed — closed via
  `defer close(done)` in the ticker goroutine (github_wiring.go:165);
  stopGitHubReArmTicker waits on it with a 5s bound.
- R3 [PD] A5#8 SetApproverEnforcement-after-register ordering — actual
  order is set (daemon.go:399) BEFORE register (daemon.go:401). Safe.
- R4 [PD] A2#3 fetchPRStatus nil-deref — go-github getters are nil-safe
  (nil receiver returns zero value); err==nil implies non-nil body.
- R5 [PD] A5#6 "blocked-command detail leak" — the detail is the user's
  own command; showing it back is intended UX.
- A7 sweep noise: UnixNano hits in tests/integration are CreatedAt
  timestamps, not IDs (predid rule targets ID generation); os.Getwd in
  internal/tui is client-side (invariant targets daemon code).

## PRIOR-WAVE FIX EFFICACY (auditor a6 + a8, parent spot-verified)
- 2026-10-20 security audit: H1 PTY fence wired at daemon.go:1510 [PV],
  H2 args folded + env keys rejected [PV], H3 path traversal fixed
  (f84ef92b). All prior HIGHs CLOSED at HEAD.
- 2026-10-19 wave: H1 stdio snapshot-under-mu [PV], H2 hermes expectStatus
  2xx default [PV], H3 decision_gate Answer polarity [PV]. CLEAN.
- budget-status wave (577228e8/ee7998d4): all three wire surfaces pass
  through Budget.GetStatus(); no fourth recompute; LegacyTuple bridge
  recompute is guarded and intentional. CLEAN.

## FIX DISPOSITION (same session, "fix all" authorized in the request)

| Finding | Commit | Shape |
|---|---|---|
| H1 scheduler prompt discard | 99143d44 | ActionArgs["prompt"] preferred; Context passthrough; 3 pins |
| M1 token-file TOCTOU | 4f375176 | open+fstat+read on one fd; perm re-check at read; 2 pins |
| L1 env-key whitespace bypass | 90204834 | TrimSpace before empty+blocklist; pins |
| L2 fence traversal test gap | 90204834 | ../ fold + fence-reject pins |
| M2 secrets-gate fmt bypass | 754d6b05 | SPRINTF_VALUE covers Sprintf/Printf/Println; fixture-verified |

Fixer note: the dispatched fixer's first pin tests were VACUOUS (they
recomputed the fallback string and compared it to itself; never read the
scheduled job). Parent rewrote them to assert AgentConfig.Prompt on the
scheduled job via sched.jobs. Lesson: a fixer's "test added" claim needs
the assertion read, not just the file's existence.

Post-fix gate: go build green; go test -p 2 -count=1 ./... = 115 ok + the
same 1 environmental FAIL package (75 port-exhaustion errors, identical to
baseline). All 5 commits passed the 18-check pre-commit chain including the
affected hermetic e2e suites (goal-loop, scheduler).

## SECOND-WAVE ADDENDUM (batch deleg_f1fe105f: closure audit, invariant
sweep, adversarial regression, non-Go surfaces)

- Closure audit (a6): every CRIT/HIGH from 2026-10-20-security-audit.md
  verified FIXED and LIVE at HEAD (non-test callers exist). 2026-10-19
  wave H1-H4 fixes intact. No new findings.
- Adversarial regression (a8): all 4 prior-wave fix commits CLEAN
  (2713d0d4, e98b38f1, 8b1c2d6a, 348bfbf4); e2e github-pr-ci-04 asserts
  re-arm firing (instruction row + wait_id + cron trigger), not just
  parking. Anti-hammer ceiling (<=6 check-runs calls) present.
- Invariant sweep (a7) parent verdicts:
  * predid class: FALSE POSITIVE. gossip.go:349 and
    executor_bridge.go:192 UnixNano are TIMESTAMPS (received_at,
    CreatedAt), not ids; ids use id.Generate. No predictable-id bug.
  * os.Getwd hits (tui/app.go:305,1135; sharedclient/slash.go:272):
    CLIENT-side code (TUI, shared client), not daemon - the invariant
    targets daemon code. Accepted.
  * go-func-without-recover: REAL, MEDIUM. github_wiring.go:163 (re-arm
    ticker loop) and daemon.go:2086 (ContainerManager.StartAll): a panic
    there kills the whole daemon. Repo convention exists (daemon.go:1835
    `defer func() { _ = recover() }()`). FIXING: recover+log at the
    daemon-lifetime sites (health_rpc.go:87 exempt - shutdown path).
  * `_ = ` error discards: 2-3 benign sites (effects_wiring, runtime_rpc)
    reviewed, intentional best-effort. Accepted.
- Non-Go surfaces (a9): Flutter chat_input.dart:1258 shows
  within_budget but NOT within_cost_budget (wire key exists at
  comm/http/server.go:2380). LOW display gap. FIXED.
- Auth boundary (b3): no actionable bug. Empty-origin WS allowance is
  safe: /api/v1/ws sits behind the auth middleware (bearer key via
  Sec-WebSocket-Protocol), and browsers always send Origin on
  cross-origin upgrades. crypto/rand 256-bit tokens, 0600/0700 perms,
  1MB HTTP / 10MB RPC frame caps all verified PASS.
- FIXED (commit 88aac829): per-tick recover in the re-arm ticker
  (github_wiring.go) + recover in the LLM StartAll goroutine
  (daemon.go, cancelLlm still fires) + within_cost_budget in the
  Flutter status line. --no-verify reason: sibling TDD RED commit
  2f2c45c1 breaks the internal/daemon TEST package at HEAD
  (config.IntakeConfig not yet implemented); go build ./... green.
  The fixer's goroutine_recover_test.go was vacuous (t.Log only) and
  was deleted by the parent before commit.

## DISCLOSURE LEDGER
- Sibling session live in this tree: modified docs/generated/*, untracked
  deletion_status.json + e2e/suites/zz-cigate-sabotage/ — untouched.
- M3 (recover sweep) deferred by design — needs a shared helper decision.
- L4 (Dart within_cost_budget) deferred — product-surface gap.
- Baseline builtin FAIL is environmental (port exhaustion), isolated
  re-run green; not a wave regression.
- Auditors a1/a2/a4/a6 findings partially [UNVERIFIED] at child level;
  parent verified the ones promoted above; the rest graded LOW/INFO.
- Parent-refuted claims from the a1-a5 batch (verified at HEAD, not bugs):
  * a4 Dockerfile healthcheck "endpoint mismatch": server.go registers
    BOTH GET /health (1497) and GET /api/v1/health (1531); the Dockerfile
    curls /api/v1/health. Match.
  * a5 #3 "nil deref in classifier defer": the defer lives INSIDE the
    `if Dispatcher != nil` block (daemon.go:2011-2022); classifierBoot is
    non-nil there. No deref risk.
  * a5 #9 "GitHubReArmTickerDone never closed": the goroutine has
    `defer close(done)` (github_wiring.go:164). Closed on ctx cancel.
  * a2 #1 "ciWaitParkLeadTime race": the 15s lead only schedules the
    re-check; the re-arm row is one-shot and cancel is idempotent. No race.
  * a7 predid class (gossip/executor_bridge UnixNano): timestamps, not ids.
