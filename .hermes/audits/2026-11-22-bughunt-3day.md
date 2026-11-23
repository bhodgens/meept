# Bughunt 2026-11-22 — last 3 days of commits (audit only)

Repo: /Users/caimlas/git/meept, branch main.
Request: "do a bughunt on the last 3 days of commits and report" — REPORT ONLY.
No fixes authorized; no files in the repo were changed by this wave.

## Scope and anchor

The last 3 days contain 143 commits (2026-11-19..2026-11-22). Prior audit reports
already cover the tail through `e1de1df6`:

- `.hermes/audits/2026-11-21-bughunt-2day.md` (close-out at `e1de1df6`,
  2026-11-21 report commit + its fix wave)
- `.hermes/audits/2026-11-20-bughunt-7day.md`
- `.hermes/audits/wave-2026-11-19b/REPORT.md`

Per the branch-point/anchor rule, this wave audits the UNAUDITED TAIL
`e1de1df6..HEAD` (46 commits at dispatch, 47 at report — a sibling commit
`559050da` landed mid-audit). The tail is dominated by the
`20261121-interactive-usability` tree: 223 changed files, ~23.6k insertions,
11 merge commits, auto-seal, interview answers, plan lifecycle, CLI/TUI/GUI
surfaces, and the cost/token gating lanes. A regression review of prior-wave
fix commits and 11 merge resolutions is included.

HEAD at report: `559050da` (sibling commit touched only the explicitly-ignored
files; every finding below was re-verified at this HEAD).

## Baseline

- `go build ./...` — clean at dispatch HEAD.
- `go test -p 2 -count=1` over the five most-changed Go packages
  (`internal/agent`, `internal/plan`, `internal/task`, `internal/rpc`,
  `internal/comm/http`) — all ok, 0 FAIL.

## Method

Baseline gates → 6 parallel READ-ONLY auditors (disjoint scopes: agent/plan/task;
TUI/CLI; Flutter GUI; daemon/runtime/cost; HTTP/RPC/session/config;
adversarial regression + merge/test-integrity review) → parent source-verification
of every HIGH and the highest MEDs at HEAD. Two findings were reproduced with
executable probes in detached worktrees (removed afterward). Auditor reports are
self-reports; nothing below is taken on trust without parent verification.

## Verified findings

### HIGH (5 parent-verified at HEAD)

- **H1. TUI crashes on startup: nil `*SessionsModel` dereference** —
  `internal/tui/app.go:937` (`a.sessions.SetSize(...)`), introduced by
  `cd3c3962`. That commit deleted `sessions: models.NewSessionsModel(rpc),`
  from the `NewApp` struct literal but left all production uses. No production
  assignment exists (assignments occur only in test files). The first
  `tea.WindowSizeMsg` auto-sent at startup reaches `a.sessions.SetSize` with a
  nil receiver.
  Empirical proof in a detached worktree at HEAD: a test called
  `NewApp(...).Update(tea.WindowSizeMsg{...})` and panicked with
  `SIGSEGV ... (*SessionsModel).SetSize(0x0, ...) at
  internal/tui/models/sessions.go:152, App.Update at internal/tui/app.go:937`.
  Also unguarded at `app.go:2707`, `:3141`, `:3202`. Severity CRITICAL for
  interactive `meept chat`.

- **H2. Auto-seal bypasses the approval gate for negated "implement the plan"
  prompts** — `internal/agent/strategic.go:2397-2401`, introduced by
  `bacb4352`. `PlanAutoSealFromIntent` checks `autoSealExplicitMarker`
  (`implement|execute|build the plan/roadmap/blueprint`) and returns `true`
  BEFORE checking `autoSealNegatedExecute`
  (`don't/do not/never implement|execute|build ...`). The two patterns collide.
  Empirical proof in a detached worktree at HEAD: a test asserted that
  `"write a plan but don't implement the plan"`,
  `"create a plan, do not execute the plan"`, and
  `"draft the plan and never implement the plan"` must all return false —
  all three returned true and the test FAILED. The full user prompt is the
  detector input (`handler.go:2106`, `dispatcher.go:2851` sets
  `Task.Description` to the full input), so a user who explicitly withholds
  execution gets `AutoSeal=true`, `requiresApproval` returns false immediately,
  and the plan goes to `StateExecuting`/`orchestrator.schedule`. The 18-case
  detector table only exercised negation without the "the plan" object, so
  the collision was unpinned.

- **H3. Planner interview answers never resume for CLI/TUI-shaped sessions** —
  `internal/agent/planner_interview.go:421` (park) and
  `internal/agent/handler.go:997-1001` (answer lookup), introduced by
  `f2a92419`. The waiter is parked under the `PlanRequest.SessionID`, which
  `publishPlanRequest(result, conversationID)` always sets to the canonical
  conversation id (`conv-…`; `handler.go:1374,1421,2033`). On the answer turn,
  `DeliverInterviewAnswer` compares verbatim against `w.sessionID`
  (`planner_interview.go:281`), while the handler passes the raw
  `req.SessionID` (the primary `session-…` id from the store; distinct value,
  forwarded unchanged by `chat.submit`, never reassigned in `handleRequest`).
  The equality fails, the waiter is never consumed, and `resumeInterview` never
  runs. The plan stays parked in StatePlanning and the answer falls through to
  normal dispatch. The GUI's session-id-as-conversation-id shape matches by
  accident; CLI/TUI shapes fail. The unit gate uses one string for both sides.

- **H4. Stalled-executing sweep's `TaskLookup` seam is dead in production** —
  `internal/plan/manager.go:1048-1055,1085,1114-1124`, introduced by
  `d01cb475`. The sweep asserts `m.taskCreator.(TaskLookup)`, and
  `stalledExecutingReason` returns early when the assertion fails. Every real
  `taskCreatorAdapter` (`internal/daemon/daemon.go:3611-3653`,
  `components_wiki.go:279`) has CreateTask/SetGoalID/… but NO `GetTask`.
  The only `GetTask` is on `*agent.Dispatcher` (`dispatcher.go:5207`), which
  is not assigned as `taskCreator`. The assertion always returns nil, so the
  "parent task failed" and "parent task missing" arms never fire. Tests pass
  only via a hand-rolled `noopCreator` that declares `GetTask` — the
  test-double trap called out in the bughunt skill.

- **H5. ACP catalog `permission_mode` bypasses the tier-3 D3 guard** —
  `internal/acp/manager.go:352-355`, `internal/daemon/acp_employee_executor.go:278-294`,
  introduced by `83ef9aab`. `effectiveACPPermissionMode(spec, global)` reads
  ONLY the spec and the `[acp].permission_mode` global. The session manager
  resolves a THIRD source, `entry.PermissionMode` from `acp_agents.json5`,
  with precedence call option > catalog entry > global. A tier-3 employee
  configured with global `permission_mode="human"` but whose catalog entry
  says `permissive` passes the executor guard (`mode == "human"` → no refusal),
  then `GetOrCreatePersona` uses catalog `permissive` and
  `internal/acp/session.go:678` (`approved := mode == "permissive"`) auto-
  approves every external permission request — exactly the combination the
  guard exists to prevent. Escalatory only toward permissive.

### MEDIUM (verified, user-facing or safety-relevant)

- **M1. Per-turn token gate sees a per-call snapshot, not the turn total** —
  `internal/agent/loop.go:2099-2105,2128-2135,3125`,
  `internal/employee/enforcement.go:844-855`, sha `286cf53c`.
  `recordTurnProviderUsage` assigns `=` on each response; the accumulated sum
  is written by `recordTurnTotal` only at the terminal return (`:7005`).
  The executor injects `LastTurnTokens` into `checkPermission` during tool
  execution, before that terminal write. A 3-call × 8000-token turn with an
  8000-token cap sees 8000 on every call; the cap never trips. The cost twin
  uses `+=` correctly (`recordTurnCostUSD:2119`) — the asymmetry is the cause.

- **M2. ACP-backed employees report zero cost; daily budget never binds** —
  `internal/daemon/acp_employee_executor.go:125-155`, sha `8ce5daaa`.
  `RunOnce` → `runTurnProducer` → `runACPTurn` bypasses `reasoningCycle`,
  the sole production caller of `recordTurnCostUSD` (`loop.go:6064`).
  `LastTurnCostUSD()` (as read at `:134`) therefore always yields 0 and
  `ExecutionCostCents()` at `:155` is structurally always 0. A goal-loop
  round spending real provider dollars records no spend and never trips the
  daily-budget guard. The adjacent warning at `:302-317` still claims the
  cost signal does not exist.

- **M3. TUI plans-view cursor/selection off-by-one with gate rows** —
  `internal/tui/models/plans.go:495-516` vs `:605-680`, sha `57468e1d`.
  `updatePlansTable` prepends one synthetic row per pending task gate, but
  navigation and Enter map `m.table.Cursor()` straight into `filterPlans()`,
  which contains no gate rows. With one gate and one plan, cursor 0 (the gate
  row) selects the real plan; cursor 1 (the plan row) fails the bounds check.
  Since `a`/`v`/`c`/`d` act on `m.selected`, a user can act on the wrong plan.

- **M4. Advertised `x: reject` key has no handler in the TUI plans view** —
  `internal/tui/models/plans.go:755-764`, sha `57468e1d`. The banner says
  `x: reject (never executes)` but the `Update` switch (`:429-518`) has no
  `case "x"`; the only `x` key in `internal/tui` is in a different view.
  Pressing `x` does nothing. The hint on `:764` says `d: reject`, so the two
  hints also contradict each other.

- **M5. `--session-create` discards the resolved `--project`** —
  `cmd/meept/chat.go:425-437`, sha `cd3c3962`. `runChat` resolves
  `--project myapp` into real `projectID`/`projectPath` locals (`:98-109`),
  but the create path calls `projectIDForCreate()` (returns the raw project
  NAME, not the resolved ID) and `projectPathForCreate()` (returns `""`).
  The daemon looks up `project_id` by ID and fails; the session binds to the
  CWD instead. The comment falsely claims `chatCwd` carries the path.

- **M6. GUI verbosity filter is inert mid-session** —
  `ui/flutter_ui/lib/providers/chat_provider.dart:327-362,612,1239`,
  sha `2b32ff39`. `setVerbosityLevel` has no production caller (only 4 tests).
  The Ctrl+V toggles in `home_screen.dart:599` and
  `sidebar_home_screen.dart:428` cycle `verbosityProvider` and PATCH config,
  but `ChatNotifier` reads `_verbosityLevel` only at construction; it never
  watches the provider. The status label changes while progress filtering
  keeps the old level until session swap/restart. TUI applies immediately.

- **M7. GUI/TUI plan-card separator parity gap, pinned by tests** —
  `ui/flutter_ui/lib/features/chat/plan_notification_bubble.dart:107,111,159`,
  sha `05dc6f0f`. The GUI renders ASCII `-`
  (`'document proposal - no executable steps'`, `'$phaseStr - $stepStr'`,
  `'... - awaiting confirmation'`); the TUI renders middle dot `·`
  (`internal/tui/models/chat.go:3505,3515,3596`). Tests pin the divergent
  form (`plan_notification_bubble_test.dart:23,144,167`). No documented
  deviation (repo requires explicit justification for TUI/GUI divergence).

- **M8. Seatbelt confinement silently inert unless undocumented master switch
  is set** — `internal/daemon/components.go:3874`, `internal/config/schema.go:5583`,
  `docs/workflows/seatbelt-confinement.md:19-29`, sha `83ef9aab`.
  The manager is built only `if cfg.Runtime.Enabled`, which defaults false
  and has no `[runtime]` block in the shipped template. The doc's config
  snippet shows only `[runtime.seatbelt]`. An operator following the doc
  gets no manager, no `SetSeatbelt`, no path validation, and unconfined shell
  children with no warning. The e2e suite hides this by setting
  `runtime.enabled=true` in every overlay.

- **M9. Merge `5b519f8c` dropped a validation-revision pin** —
  `internal/agent/tactical_validation_gate_test.go:362`, sha `5b519f8c`.
  `git show --cc` shows P2's assertions that zero-evidence rejection creates
  a ready revision step (`task.IsRevisionStep`, `rev.State != task.StepReady`)
  were deleted and not replaced. `TestTacticalScheduler_VacuousValidation`
  no longer pins the bounded-retry revision invariant its comment documents.
  No current production failure; coverage gap only.

- **M10-M13. Four e2e suites have no manifest entry** —
  `e2e/suites/{approval-wait,bus-delivery,failure-paths,task-approval}/`,
  introduced by `6c4e17fc` (pre-range; surfaced by the integrity sweep).
  The committed `e2e/manifest.json` omits all four; they are invisible to
  `make e2e-affected` and the pre-push affected gate and run only under
  `e2e-fast`'s blanket scan. (`manifest` self-test dir is also missing;
  `gui-flows` is declared but has no dir — that is a legitimate Dart runner.)

### LOW (verified, no immediate user harm)

- **L1.** Dead duplicate triviality cap: the inline cap at
  `internal/agent/strategic.go:1202-1206` truncates trivial plans to one step
  before `capTrivialPlanSteps` (`:1217`, `plan_triviality_cap.go:28`) runs,
  so the helper's metric/log are unreachable. Both parents added the cap
  independently in merge `5b519f8c`. Behavior is correct; observability dead.
- **L2.** `internal/agent/planner_interview.go:430` `HasPendingInterview` has
  zero production callers (dead helper).
- **L3.** `internal/rpc/queue.go:171-179` (`b16b6082`): fabricated `gen = 1`
  is only written in the `q != nil` branch but read only in `q == nil`, so
  it is unreachable dead code with a misleading comment.
- **L4.** `internal/rpc/queue.go` + `internal/services/chat_service.go`
  (`b16b6082`): `resolveQueue` can return `ErrAmbiguousQueue` ("use the full
  thread id"), but both handlers discard the error and map to
  `ErrQueueNotFound`/404. Fails safe; misleading client message.
- **L5.** `internal/session/store_sqlite.go` (`f2a92419`): the new
  `metadata` column is written/read in `GetMessages` but omitted from
  `ForkSession`'s copy INSERT, `InsertCompaction`, and `GetMessagePath`'s
  SELECT. No current consumer depends on those paths.
- **L6.** `internal/task/scaffolding.go`, `envelope_prose.go` (`92d62097`):
  display-only stripping is substring-heuristic; legitimate content
  containing `## Your Task` or a `{"claims"…}` object is truncated in the
  RPC/HTTP display payload. Store retains raw text.
- **L7.** `internal/daemon/daemon.go:2617-2634` (`d01cb475`): the stalled
  sweep ticker uses `context.Background()` and is never cancelled; it
  outlives `Run` and keeps sweeping after graceful shutdown. Process exit
  reaps it in the shipped binary; embedders leak it.
- **L8.** Swept plans leave `plan.md` saying "executing": the sweep calls
  `transition` + `publishEvent("plan.failed")` but not
  `UpdatePlanStatus`, unlike canonical `failPlan:1835` (`d01cb475`).
- **L9.** Retry attempts' provider cost discarded: `executeBotWithRetry`
  overwrites `lastCostUSD` on each attempt and `recordSpend` reads once
  after the loop (`goal_loop.go:1178-1180`, sha `8ce5daaa`).
- **L10.** `internal/runtime/seatbelt.go:118-188`: security-critical profile
  builder has zero unit coverage, while the doc claims unit tests exist
  (sha `83ef9aab`).
- **L11.** Stale comment: `strategic.go:319-320` still says
  `ApprovalStepThreshold` defaults to 5 after `fe1438f3` moved the default
  to `config.DefaultConfig` (verified: daemon wiring at
  `components.go:4670` passes the config value, which defaults to 5).
- **L12.** Stale warning at `acp_employee_executor.go:302-317` claims no cost
  signal exists even though `ExecutionCostCents()` was added in the same
  commit (see M2).
- **L13.** GUI test comment `liveness_widget_test.dart:8` still describes
  the old stalled-turn string after `4e8b37b1` changed the shipped string
  and assertions. Assertions are current; comment only.
- **L14.** GUI capitalized fallback labels (`'Untitled Plan'`,
  `'Untitled Task'`, `'Unknown Agent'`) in `sidebar_home_screen.dart`
  violate the all-lowercase UI convention; pre-range.
- **L15.** GUI `status_bar.dart:81-84` drops the invariant `verbosity:`
  element for the 2.5s transient window; TUI keeps verbosity outside the
  branch. Pre-range, pinned by `status_bar_test.dart:110-111`.

## Regression-review verdicts (prior-wave fixes)

| Commit | Verdict |
|---|---|
| `0d62dadb` park-resume invoke mutex | CLEAN |
| `4f934dfb` job processor bus wiring | CLEAN |
| `68478b69` value-type ToolResult | CLEAN |
| `8a4faf72` gofmt bot wiring | CLEAN |
| `d4bfccbc` inline specialistTerminal | CLEAN |
| `fe1438f3` configurable approval threshold | CLEAN |
| `286cf53c` per-turn token gate | M1 (value semantics, not wiring) |
| `8ce5daaa` round cost cents | M2, L9 |

Merge resolutions: 10 of 11 merges CLEAN (nine byte-exact auto-merges;
`ca10e425`'s six manual hunks verified correct at HEAD). `5b519f8c` has
L1 and M9. No weakened pre-existing assertions found across 71 test files;
9 golden updates were legitimate expectation changes.

## Disclosure ledger (what was NOT done or NOT verified)

1. NO FIXES. The request was a report; all findings remain open.
2. Flutter tests were not executed (no `flutter test` run); GUI findings are
   source-verified only.
3. The full `go test ./...` sweep was not run. Only `go build ./...` plus
   five touched-package suites were run at baseline.
4. `-race` was not run.
5. H3 and H4 are verified by source trace, not by a live daemon reproduction;
   H1, H2 were reproduced empirically in detached worktrees.
6. Pre-existing (out-of-range) observations were found but NOT reported as
   findings: swallowed config write in `cmd/meept/cluster_cmd.go:322`
   (July), unreachable `config_migrate_reasoning` command (September),
   inert `compound_type` field (documented intentional, September),
   GUI quota-listener leak and `setState`-after-dispose in `agents_tab.dart`
   (pre-range), stale `memory_view.txt` `r refresh` golden hint.
7. HEAD advanced mid-audit from `bacb4352` to `559050da` (sibling session).
   All findings were re-verified at `559050da`; the new commit touched only
   the explicitly-ignored files.
8. The working tree had sibling-modified/untracked files at baseline
   (`docs/concepts/runtime.md`, `e2e/manifest.json`, two e2e suites, 07-bench
   docs). None were read as evidence or touched. Two throwaway probe
   worktrees and one probe artifact created by this wave were removed;
   `git status` matches baseline.
9. A probe command containing `git checkout --` was blocked by the runtime.
   No retry was made; the detached worktree holding the side effect was
   removed as a whole. No repository file was restored or overwritten.

## Recommended next actions (ranked)

1. Restore `sessions: models.NewSessionsModel(rpc)` in `NewApp` (H1) — the
   interactive TUI is broken for every launch; add a startup smoke test.
2. Reorder the auto-seal detector to check the negated-execute veto BEFORE
   the explicit marker, and add the four negated+explicit strings as pins
   (H2).
3. Make the interview answer scope resolve the same canonical conversation
   id used at park time (H3), with a two-shaped (session/conversation) pin.
4. Give `taskCreatorAdapter.GetTask` (or forward the dispatcher's) to arm
   the stalled-sweep arms (H4), and delete the `noopCreator.GetTask`
   crutch from the test.
5. Fix the ACP guard to read the catalog `permission_mode` too, or make the
   manager not apply it for tier-3 employees (H5).
