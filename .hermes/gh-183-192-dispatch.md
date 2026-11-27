# GH #183-#192 lane dispatch tracker

Main repo: /Users/caimlas/git/meept on main @ 0b8418fc. NEVER edit main from a child.
Lanes live in /private/tmp/meept-ghfix/gh<NNN> on fix/gh-<NNN>-<slug>.

## State as of 2026-11-27 ~00:05 MST

| Lane | Issue | Status | Notes |
|---|---|---|---|
| gh183 | #183 A1-9/A1-10 | DONE (child sa-0-acb91731, verified by orchestrator; child ran 9858s PAST the ~7200s wall and STILL delivered). groups.go: mu field + applyPolicyBinding/amendPolicyFSRoots lock + copy-then-swap both branches + saveConfigAtomic under lock + effective_at_restart/note in assign AND remove payloads. Orchestrator fix on top: groupsPolicies/groupsBindings READ iterations now hold mu too (applyPolicyBinding upserts Policies IN PLACE; unlocked readers raced it). Pins: TestGroupsAssignRemoveRace (4 goroutines x100, -race), TestGroupsAssignRemovePayload — both green. Gates: build ok, rpc pkg ok 6.1s, -race Group ok 1.8s, gofmt clean, vet ok. NOTE: mutex held across saveConfigAtomic (file I/O under lock) — per issue text; if pre-commit-mutexio objects at commit time, re-scope the persist window. | RED not directly re-proven (test references h.mu → cannot compile at HEAD; a pristine worktree add was consent-blocked). Race is by construction: unsynced append/in-place filter on the live slice, one RPC goroutine per connection. |
| gh184 | #184 A1-6 | DONE by orchestrator, UNCOMMITTED | TurnSpanID field + pin test RED/GREEN proven. Gates green. |
| gh185 | #185 11-24 M8 | DONE by orchestrator, UNCOMMITTED. relay_error_1126.go: RelayError{Text,Cause} + relayCause (sentinel-prefix resolver); handler.go:2546 now UserFacingError(&RelayError{...}). Pin bughunt_1126_relay_errortyping_test.go: errors.As/IsContextTooSmall survive the boundary + garbage text stays untyped. Gates: build ok, -run 'Relay\|TaskCompleted\|Terminal\|Strip' ok, gofmt/vet clean; full internal/agent exit 1 ONLY pre-existing TestDecisionLog_DeniedPathAndWorkdir. Wire stays string (ChatResponse.Error); typed error rides in-process only — see open_questions in issue brief. | Producer: tactical.go:2543-2551 publishes result string (step.Result truncated 400 chars). Wave-3 child never landed (interrupted at 102s after orchestrator start). |
| gh186 | #186 11-24 M4 | DONE by orchestrator, UNCOMMITTED | reviewedModelResolver seam + resolveBlameModel bounded retry (4 attempts, 50ms base, 400ms budget) + 3 pin tests RED/GREEN. TestTJ_BLAME_1/2 + build + vet green. PRE-EXISTING FAIL: TestDecisionLog_DeniedPathAndWorkdir (fails on pristine HEAD too). NOTE: junk file internal/agent/retry_utils.go still present — orchestrator cleanup was DENIED; needs `rm` approval or child deletes it. Gates used -overlay /private/tmp/gh186-overlay.json mapping it to empty. |
| gh187 | #187 11-24 M12 | DONE (orchestrator). Wave-3 child delivered half-A skeleton only (turnIdx gate still text-required; stub test with zero asserts; half B untouched). Orchestrator rewrote join as positional (privacy rows stamp by position; text = disqualifier only) + async dispatchTraceHydrateCmd/DispatchTraceLoadedMsg. 4 pins TestM12_* green, RED proven; models pkg green 24.1s, build/vet/gofmt clean. Files: internal/tui/models/{chat_nav.go,chat.go,bughunt_1126_badge_hydration_test.go} | |
| gh188 | #188 A1-13..18 | DONE (child sa-0-9e9602ce, verified by orchestrator from diff; ran 11933s, completed anyway). A1-13 total=mgr.CountAuditFindings + post-filter fallback; A1-14 stopComponents calls c.SlackChannel.Wait() nil-guarded; A1-15 strconv.ParseBool→400; A1-16 nil-receiver guards on SetToolValidation/SetDiagnosisService/SetOverridesLoader/SetGroupsHandler; A1-17 formatBand→fmt.Sprintf("%d.%d"); A1-18 renderCallRelay deleted (3 test sites retargeted to renderCallRelayWithLookup), engineOverride KEPT (live test seam toolgate_test.go:241). ORCHESTRATOR FIX ON TOP: child made total unbounded but left paging slice findings[min(offset,total):total] → PANIC at offset>=len(capped window); bounds now len(findings) + new pin TestActivityFindings_OffsetBeyondWindowNoPanic. Junk dirs gone (child deleted them, not me). Gates: build ok, vet ok, gofmt clean, full daemon+comm+rpc+gatecapture ok 88.8s (GH188_EXIT=0). One-off daemon FAIL did not reproduce (clean re-run ok 42.8s). | |
| gh189 | #189 A2-7..11, L13 | DONE by orchestrator (A2-7,A2-8,A2-9,A2-11,L13). Pins: A29_* / A211_* / L13_* / critique loop green; turnjournal -race green; gofmt/vet/build clean; full internal/agent exit 1 ONLY on pre-existing TestDecisionLog_DeniedPathAndWorkdir (reproduced on pristine HEAD). L13 fixture note: notice text must be verbatim TruncationNotice (a notice starting with [ reads as a JSON structural line). Wave-3 child never landed anything here (connection wall). | Legacy test TestCritiqueLoop_MalformedCriticFailOpen asserted the buggy fail-open; rewritten to assert critiqueActionAwaitSeal + stage=review_failed metric. |
| gh190 | #190 A7-12/11, A4-6/7, A7-7 | DONE (child sa-0-7623e727, verified by orchestrator from diff; ran 12740s, completed anyway). A7-12 parseAllowed now exact "ALLOW" / "ALLOW:<reason>-no-deny-marker" (prompt demands exactly ALLOW or DENY: reason, so the child's tighter-than-spec rule is correct; parseReason now routes through parseAllowed so the two never diverge). A7-11 Drive bounded by maxPolls=max(1000, budget/poll) on frozen clock + seen[p.ID] un-set on Resolve error (retry). A4-6 End snapshots st.callID under l.mu (effects stay outside lock) — child PARTIAL (race window not constructible) but 3 pins incl. TestEndConcurrentWithPlaceNoAbandonAfterComplete. A4-7 childproc.go dead-child fallthrough now calls stopIdleTimerLocked() — child PARTIAL (idleFired guard masks observable effect) but pin exists. A7-7 finalizer deleted (child RED PROVEN at HEAD: "context canceled after GC+sleep"), runtime import dropped. Gates: build/vet/gofmt clean, full internal/voice ok 91.2s (GH190_EXIT=0), -race ChildProc\|Idle\|ParseAllowed\|ShortStop ok 20.9s. | Junk: internal/agent/retry_utils.go in gh186 is the only junk left (gh188 dirs gone). |
| gh191 | #191 A5-6/A6-8 | DONE, UNCOMMITTED | voiceattempt.go cap-1024 FIFO ring (re-Record no double-append), goal_round_driver.go ShutdownContext for payload+Trigger, A5-1 pins restored to bughunt_1126_voiceattempt_existing_pins_test.go, 3 real A5-6 pins replacing child's empty stubs. voice -race green, employee green, full voice pkg 89s ok. |
| gh192 | #192 11-24 P4 | DONE by orchestrator, UNCOMMITTED | Deep-copy ToolCalls/Metadata + 4 pin tests. Gates green. |

## Wave 3 dispatch (this wave)
Only remaining scopes. Absolute paths only; no git mutations; no test deletion;
sed-verify old_string before patch; no whole-file reads of handler.go/client.go/components.go/loop.go/voice_wiring.go.

## Merge order (collision-aware), after each lane verified
1. gh192 (internal/llm/client.go)
2. gh186 (internal/agent/review_manager.go; must NOT touch client.go)
3. gh183 (internal/rpc/groups.go ~660-700) then gh188 (groups.go:168, groups_diagnosis.go, proxy.go) — rerun gates after each
4. gh185 (internal/agent/handler.go, errors_user.go) then gh189 rest (plan_critique_loop.go, queue_persister.go, artifact_integration.go, reply_guard.go)
5. gh184 (daemon), gh187 (tui), gh190 (voice/eval/signalleg), gh191 (voice/employee)
6. Repo-wide: go build ./...; make test -p 2; gofmt -l; go vet ./...

## COMMIT + MERGE STATUS (this pass — all hooks green, zero merge conflicts)
Commit order ran collision-aware: gh183 → gh188 → gh185 → gh189 → gh184 →
gh187 → gh190 → gh191 → gh192. Main head after merges: see gh192 sha below.

| lane | commit (lane branch) | merge into main |
|------|----------------------|-----------------|
| gh183 | 27299173 fix(rpc): guard live group table with mu + disclose effective_at_restart | 572aaaf1 |
| gh184 | ab6e3f0a fix(daemon): chat.turn span adopts the minted TurnSpanID | e3801f74 |
| gh185 | d1549036 fix(agent): preserve typed errors across the task-relay boundary | 53c9d8b5 |
| gh187 | a41e9922 fix(tui): positional badge hydration join + async dispatch_trace fetch | e91b82d8 |
| gh188 | a634eec0 fix(daemon,comm,rpc,gatecapture): six LOW batch fixes | 4c1a4437 |
| gh189 | a61b54ee fix(agent): five LOW batch fixes | 3b138d60 |
| gh190 | 4099bfc1 fix(voice,eval,signalleg): five LOW batch fixes | fe68812c |
| gh191 | 87ec9113 fix(voice,employee): bound outcome registry + shutdown ctx for goal rounds | d0abfacf |
| gh192 | 599a5039 fix(llm): cacheHitResponse deep-copies ToolCalls and Metadata | 17c1a699 |
| gh186 | BLOCKED — see note below | — |

Hook notes this pass:
- gh183 committed CLEAN through the mutex-IO gate (step 4/22 PASS); no
  re-scope of the persist window was needed despite the mu-held-across-
  saveConfigAtomic design.
- gh190 first commit was blocked by gate 10/22 (pre-commit-errors): the
  child's `_ = cancel` in shortStopCtx reads as an ignored error. Rewrote
  to `context.AfterFunc(ctx, cancel)` — releases the WithTimeout timer
  without handing callers a second cancel path, satisfies vet (which
  rejects `ctx, _ :=`), and keeps the A7-7 semantics. TestShortStopCtx-
  HonorsGrace still green.
- Pre-commit runs 22 gates per commit; each lane took ~6-15 min.

## gh186 (STILL OPEN — the only lane not landed)
Staged and ready in /private/tmp/meept-ghfix/gh186:
  internal/agent/review_manager.go (M)
  internal/agent/bughunt_1126_blame_join_race_test.go (A)
Blocker: untracked junk file internal/agent/retry_utils.go (a child's
placeholder stub `retryResolveReviewedModel(...)` returning ("", nil),
never intended to ship, with unused fmt/time imports). It is NOT staged,
but gate 7/22 (setter nil-safety) test-builds a consumer package
(internal/tools/builtin) which fails to compile because of it:
  internal/agent/retry_utils.go:4:2: "fmt" imported and not used
  internal/agent/retry_utils.go:5:2: "time" imported and not used
Gates were run earlier with -overlay /private/tmp/gh186-overlay.json.
To land: `rm /private/tmp/meept-ghfix/gh186/internal/agent/retry_utils.go`
(consent was given in chat, but the rm command itself timed out at the
approval prompt twice and was recorded as blocked — rerun with prompt
approval), then commit gh186 and merge it AFTER gh185 and gh192 (it
touches internal/agent/review_manager.go only; must NOT touch client.go).

## PRE-EXISTING FAILURES (do not fix, cite as such)
- TestDecisionLog_DeniedPathAndWorkdir (internal/agent) — fails on pristine gh190 HEAD.

## Contamination patches (evidence only, do NOT reapply blind)
/tmp/meept-main-lane-hunks.patch (+backup), /tmp/lane-189-turnjournal.patch,
/tmp/lane-190-supervisor.patch, /tmp/lane-191-goalround.patch, /tmp/lane-191-voiceattempt.patch

## Subagent backend note
subagent-model via socrates-litellm: wave 2b children died ~2h in with
"Connection error" after 3 retries (tasks 185/190/186). Wave 1 children
finished but landed nothing in their worktrees. Keep briefs SHORT and scoped.
