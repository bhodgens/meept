# Bughunt 2026-11-20 — last 7 days, unaudited tail + regression review

Repo: /Users/caimlas/git/meept, branch main, HEAD at dispatch: ec67c334.
Request: bughunt the last 7 days and fix the issues found. Fixing is authorized.

## Anchor

Prior reports already covered 2026-11-13..415520c8 and landed fixes:
- .hermes/audits/2026-11-14-bughunt-week.md
- .hermes/audits/2026-11-15-bughunt-two-run.md
- .hermes/audits/2026-11-17-bughunt-wave.md
- .hermes/audits/2026-11-19-bughunt-20day.md (waves 1-6, close-out at 415520c8)
- .hermes/audits/wave-2026-11-19b/REPORT.md

This wave does NOT re-audit that closed ground. Scope:
1. Fresh audit of commits after the close-out ledger: 48655d53 (BotRunner deletion), ec67c334 (deadcode baseline growth gate).
2. Adversarial regression review of the Nov 19-20 fix commits (1cee89d7..415520c8) that have not had an independent regression pass. The 11-19b auditors failed; parent takeover is not independent corroboration.

Sibling worktrees are out of scope. Do not stash, checkout, or edit them.
M6 (multi-user execution-output scoping) stays an owner decision. Do not fix it.

## Auditor scopes (read-only)

1. Bot deletion + constitution trigger + retry (internal/bot, internal/employee, daemon bot wiring).
2. Voice STT/clip/warm/rumble fixes (internal/voice, internal/voicechild).
3. TUI/GUI runs, rename, agents tab (internal/tui, ui/flutter_ui).
4. LLM/agent/groups admission, budget, firewall, drift writer.
5. Activity store/projector/HTTP shutdown drain + e2e manifest.
6. Deadcode baseline growth gate (ec67c334).

## Baseline

First full suite (`go test -p 2 -count=1 ./...`, pid 32144) exited 1.
The saved log is a 200k tail (121 ok lines visible, start truncated), so
the package total is not a complete count. The only FAIL line in that
tail is internal/daemon:

- TestSendOpenFramePhoneLeg
- TestSendOpenFrameLocalLegUsesLocalConfig
- Error: fake child dial .../test-text.sock: connect: connection refused
- Same two tests pass alone (0.93s) and as TestSendOpenFrame (0.91s).

Cause: fakeChildAt in internal/daemon/voice_open_test.go treated
socket-file existence as a live listener. net.Listen creates the file
at bind, then calls listen. A dial in that gap is refused. The full
suite hits the gap.

Fix landed: 1d52b8f8. Helper retries the dial until accept or the
existing 5s deadline. Second full suite (pid 47574) exited 0. The saved log is 192 lines
and starts at the process header, not mid-line. Count: 160 ok
packages, 0 FAIL. This run overlapped the fixer dispatch, so it is
not proof the fixer diffs are green.


## Parent-verified findings (auditors returned, HEAD after 1d52b8f8)

HIGH, confirmed, fixers dispatched (deleg_a5eef4e7):

1. sessionStore assert never matches the real store.
   internal/agent/loop.go:1239 Get returns any; store_sqlite.go:638
   returns *Session. SetSessionStore (loop.go:10367) leaves the field
   nil. triggerTitleRefresh (loop.go:11207) never honors
   DescriptionUserSet. Daemon wires the store at components.go:3519.

2. PeekAliasForWarm returns the sticky pin with no posture check.
   internal/llm/resolver.go:1541. Production caller
   voice_wiring_contextwarm.go:149 then Warms the user partial to that
   model. ResolveForAliasDifficulty does call posturePass.

3. Deadcode growth hook exits 0 before the check.
   .githooks/pre-commit-deadcode:223 when no staged .go file.
   ci.yml:372 exits 0 on a zero before-sha. The script treats a missing
   working-tree baseline as empty and exits 0 (around line 83).

4. DailyBudgetCents never trips. enforcement.go:818 reads
   TodayCostCents. The only writer is CostTracker.RecordExecution
   (internal/bot/cost.go:60) and it has no production caller.

5. MaxTokensPerTurn guard reads details["turn_tokens"]
   (enforcement.go:845). Production never sets that key.

MED, confirmed, in the same fixer batch:

6. Drift writer treats Audit skip as score 0 (scheduler_jobs.go:295,
   enforcement.go:1700).
7. Bus rounds publish empty trigger_ref (goal_loop.go around 1146).
8. tests/bot_integration_test.go still calls deleted NewBotRunner.

Deferred (not in the fixer batch):

- sessionFor ConversationID stamp (voice_wiring.go:1428) is unread.
  binding.go:316 already passes the real conversation id, and the
  runner discards the handle. Dead stamp, not a live accounting bug.
- VoiceRumbleRuntimeConfig GPU cap is parsed and never read
  (schema.go:5644). Spawn was marked SUPERSEDED in 5494bf54. Owner
  decision: delete the unused fields, or keep them for a future spawn.
- M6 multi-user execution-output scoping. Owner decision.
- Warm unwrap skips the firewall system-message bound. Included in
  fixer 2, not deferred.

## Disposition (committed, not pushed)

| Item | Commit | Status |
|---|---|---|
| fake-child dial race | 1d52b8f8 | FIXED. Isolated TestSendOpenFrame green. |
| sessionStore assert miss | 272af4b5 | FIXED. Pin TestTitleRefreshPublishesUserDescription green. |
| warm sticky skips posture | 06bdd865 | FIXED. Pin went red when the guard was disabled, then restored, then green. |
| warm system-message bound | 06bdd865 | FIXED in the same commit. Warm error is logged at DEBUG, not discarded. |
| deadcode growth fail-open | be258f4b | FIXED. Hook calls the script before the no-.go return. CI no longer skips a zero before-sha. Missing working-tree baseline exits 2. |
| bus trigger_ref empty | 09dbaa7e | FIXED. Pin TestTriggerRef_BusRoundWithoutGoal green. |
| drift skip writes 0 | 09dbaa7e | FIXED. Pin TestDriftScore_SkipDoesNotClobber green. |
| invocation cap on Trigger | 09dbaa7e | FIXED for rounds that start. A constitution-less trigger does not record. |
| BotRunner integration compile | cc614a38 + 8b8eff0e | FIXED. 8b8eff0e removed the dead agent.ExportedRefusalSeams call; -tags integration compiles; all five refusal e2e pins pass. |
| daily cent cap | 8b8eff0e + 286cf53c + 8ce5daaa | CLOSED. llm.Response.CostUSD carries provider dollars (9 stamp sites). AgentLoop.LastTurnCostUSD accumulates per turn; both daemon executor adapters expose ExecutionCostCents; goal_loop forwards to RecordRoundSpend -> TodayCostCents -> enforcement.go:818. Pins TestChatResponseCostUSD, TestLastTurnCostUSD_SumsMultiCallToolLoop, TestExecutionCostCents_RoundsDollarsToCents green. |
| turn_tokens guard | 286cf53c | CLOSED. Executor.SetTurnTokensProvider injects details["turn_tokens"] before CheckPermission; wired at loop construction to loop.LastTurnTokens. Pin TestCheckPermission_TurnTokensInjection green. |

Post-fix full suite (pid 98587, HEAD 8ce5daaa): exit 1, 150 ok packages,
5 FAIL packages. Attribution:
- internal/agent (TestTacticalScheduler_Vacuous, TestHeuristicReview,
  TestThresholdConfig, TestTrivialityEscape*): the sibling session's
  commit f326aef8 landed tests-first RED gates for the
  20261119-usability-remediation plan; its tactical.go WIP is uncommitted
  in the tree. Owner: the usability-remediation lane, not this wave.
- internal/comm/http TestEveryWSClassifiedHasDecoderEntry: FIXED here,
  commit d4bfccbc. specialistTerminal embedded TurnTerminalEvent, which
  promoted its WSClass() method, so the module-wide sweep read the
  private decode struct as a new WS wire payload. The fix inlines the
  seven fields the projector reads; JSON decode behavior is unchanged.
  internal/comm/http and internal/activity both green after.
- internal/rpc TestTaskApprove_RPC: resolved by the sibling lane's
  own commit (the reject wiring landed; the failure no longer
  reproduces at HEAD; TestTaskApprove_RPC passes 5/5 repeated).
- internal/agent + internal/tui RED gates: the usability-remediation
  lane landed its features (fe1438f3, 4aa45fae) and internal/agent is
  green. internal/tui golden-view failures come from that lane's
  UNCOMMITTED sidebar/layout WIP (9 files, 666 insertions); the goldens
  expect the old layout. Owner: the usability lane, not this wave.
- internal/security/ssrf (port exhaustion): FIXED here, commit d83c0647.
  The two redirect pins retry once on the exact "can't assign requested
  address" error under a 50ms backoff; any other error returns unchanged.
  Pins pass 3/3 repeated; the package passes.

## Final gate

Full suite at HEAD d83c0647: exit 0, 160 ok packages, 0 FAIL
(proc_c509831601f3). All wave items closed. No push.

No push.





