# MEEPT USABILITY RERUN - GUI WEB-BUILD FIX + FULL BATTERY RE-TEST
## HEAD ff19f10f vs audit base 1cee89d7

Date: 2026-11-19 (rerun window 17:54-18:30 MST)
Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-gui-fix
Branch: fix/usability-gui-openexternal (base ff19f10f, UNCOMMITTED per instructions)
Daemon under test: /tmp/usability-rig/meept-daemon rebuilt from ff19f10f
  (go version -m: v0.0.0-20261120004920-ff19f10f0b15+dirty; dirty = the
  one-line GUI fix + untracked probe/test files only, no Go changes)
Rig: /tmp/usability-rig (MEEPT_HOME=/tmp/usability-rig/home/.meept,
  HTTPS 127.0.0.1:57734 require_auth=false websocket=true,
  llm 57732 LFM2.5-8B-A1B Q4, router 57733 LFM2.5-Encoder-350M,
  plans.plan_compiler_enabled=false, orchestrator.max_plan_steps=4,
  llm.budget.enabled=false, quota_retry disabled, local-only models)
Bench: meept-bench 5ac2e509, suite usability-probe.json, fixture project2,
  scratch worktrees-gui, out bench-results-gui (fresh dirs; no branch
  collisions), --auto-approved, judge-local.py against the rig 8B.
Prior evidence compared against: REPORT.md, findings-orchestrator.md
  (F-ORCH-1..29), probes-prior/*.json (backed up before overwrite),
  bench-results-clean/, daemon-prior.log.

Evidence classes: LIVE (reproduced on the rebuilt rig daemon), BUILD
(build/test output), CODE (file:line at ff19f10f), REPRO (isolated
reproduction program).

--------------------------------------------------------------------------
## 1. THE FIX (F-ORCH-18 / REPORT.md W21)
--------------------------------------------------------------------------

Bug (BUILD-verified at both bases): `flutter build web --release` fails
with "Error: Method not found: 'openExternal'." The conditional export
lib/core/platform/platform_open_helpers.dart selects platform_open_web.dart
on web; that file defined openExternalWeb while the only caller
(platform_service.dart:84, via openhelpers.openExternal) and the native
half (platform_open_native.dart:15) use openExternal.

Caller audit before renaming: grep of the whole ui/flutter_ui tree found
exactly ONE occurrence of openExternalWeb (the definition itself,
platform_open_web.dart:9). deep_link.dart and its tests reference
openExternal (the platform_service method / injected callback), not the
web-half symbol. No other callers needed updating.

Diff (the entire code change):

  --- a/ui/flutter_ui/lib/core/platform/platform_open_web.dart
  +++ b/ui/flutter_ui/lib/core/platform/platform_open_web.dart
  @@ -6,7 +6,7 @@
   /// Open [url] in a new browser tab. window.open returning null (popup
   /// blocked) degrades to false; the deep-link path never throws.
  -Future<bool> openExternalWeb(String url) async {
  +Future<bool> openExternal(String url) async {
     final web.Window? win = web.window.open(url, '_blank');
     return win != null;
   }

Behavior identical: window.open(url, '_blank'), null -> false, never throws.

Build before (prior session, HEAD 1cee89d7): flutter-build3.log /
REPORT.md F-ORCH-18: "Error: Method not found: 'openExternal'." exit != 0.
Build after (ff19f10f + fix):
  flutter build web --release --dart-define=MEEPT_API_HOST=127.0.0.1
    --dart-define=MEEPT_API_PORT=57734
  -> "Compiling lib/main.dart for the Web... 29.4s / Built build/web"
  FLUTTER_BUILD_EXIT=0  (/tmp/usability-rig/flutter-build-rerun.log)

Test suite (flutter test, headless):
  First run at ff19f10f+fix: 794 passed, 0 failed, EXIT=0
    (/tmp/usability-rig/flutter-test-rerun.log)
  Added regression pin test/core/platform_open_contract_test.dart (2 tests:
  the conditional export must expose exactly openExternal with signature
  Future<bool> Function(String); never-throws contract on an unopenable
  URL). VM tests exercise the native half; the web half is pinned by the
  web build itself (dart:js_interop cannot run under flutter test on VM).
  Second full run: 796 passed, 0 failed, EXIT=0
    (/tmp/usability-rig/flutter-test-rerun2.log)

Bundle verification (GUI served at http://127.0.0.1:57735):
  main.dart.js 5,514,031 bytes, sha256
  064340462673f58252f2e969fb3e549e92d9ea7112901db35a2ac7a94df57d80
  (/tmp/usability-rig/gui-api-checks.json gui_main_js).
  Symbol-name grep is NOT a valid oracle on the release bundle: dart2js
  minifies private/top-level names, so neither openExternal nor _blank
  appears literally (0 hits each; xdg-open from the native half also 0,
  confirming the native half was correctly excluded). Deep-link module
  presence confirmed via surviving string literals: '/activity' x8 in
  main.dart.js (deep_link.dart route literal). The compile itself is the
  proof the renamed symbol resolved: before the fix the SAME build command
  fails at compile time; after, exit 0. A live click-through of
  window.open was not possible (browser tooling blocks loopback; see
  section 5).

--------------------------------------------------------------------------
## 2. TEST MATRIX (prior verdict vs rerun verdict)
--------------------------------------------------------------------------

### 2.1 Build/test gates

| Check                        | Prior (1cee89d7)        | Rerun (ff19f10f+fix)     | Evidence |
|------------------------------|-------------------------|--------------------------|----------|
| flutter build web --release  | FAIL openExternal       | PASS exit 0              | flutter-build-rerun.log |
| flutter test                 | not run (build broken)  | 796/796 pass             | flutter-test-rerun2.log |
| go build daemon+cli          | ok (prior rig)          | ok, rebuilt from ff19f10f| go version -m binary |
| daemon boot healthy          | ok                      | ok: 2x Runtime healthy + daemon: running 17:58:34 | daemon.log |

### 2.2 Bench suite (serial, fresh daemon, fresh scratch; prior clean run authoritative)

| Task                    | Prior clean | Rerun     | Wall (prior->new) | Evidence |
|-------------------------|-------------|-----------|-------------------|----------|
| u-context-two-step      | FAIL        | FAIL      | 54s -> 54.9s      | bench-results-gui/results.jsonl + transcripts/usability-u-context-two-step-a1.json |
| u-steer-midrun          | PASS        | FAIL      | 24s -> 21.5s      | transcripts/usability-u-steer-midrun-a1.json |
| u-route-research-write  | FAIL        | PASS      | 5s -> 9.6s        | transcripts/usability-u-route-research-write-a1.json |
| u-fail-nonexistent-read | PASS        | PASS      | 8s -> 11.2s       | results.jsonl (committer/git misroute persists, honest outcome) |
| u-plan-mode-request     | ERROR stall | ERROR stall| 302s -> 303.7s   | results.jsonl; daemon.log:958 |
| u-direct-explicit       | PASS        | PASS      | 11s -> 8.5s       | results.jsonl |

Score: 3 pass / 3 fail both runs, but TWO verdicts flipped in opposite
directions (details section 4). BENCH_EXIT=1 (bench-gui.log).

Task-level detail:
- u-context-two-step FAIL (both runs, DIFFERENT proximate cause):
  Prior: primary wrote counter.txt=3; follow-up file_read found it MISSING.
  Rerun: the PRIMARY write was security-DENIED because the model hallucinated
  the path /private/tmp/usability-u-context-two-step-a1/counter.txt (dropped
  the usability-rig/worktrees-gui prefix) while the job workdir was correct
  (daemon.log:496-497 "Path does not match any allowed path pattern",
  workdir=/private/tmp/usability-rig/worktrees-gui/usability-u-context-two-step-a1).
  The step still finalized "completed" with a result narrating the denial;
  the +45s follow-up ran as a NEW task (chat fallback) and no counter.txt
  ever existed. Cross-turn artifact loss class persists; path-hallucination
  + deny-without-retry is the new wrinkle (F-NEW-4).
- u-steer-midrun FAIL (was PASS): primary result.txt task completed 17:59:58;
  steer text submitted +5s (18:00:02) via chat fallback, accepted, dispatched
  as a NEW coder task (intent=code conf 0.968, daemon.log ~700-712), which
  wrote result2.txt (on disk in worktrees-gui/usability-u-steer-midrun-a1/,
  1 byte '7') and deleted result.txt. BUT the bench transcript captured NO
  terminal/reply for the second turn (tool_trace has only the primary's
  chat_message frames; final_reply is the PRIMARY's). The work happened;
  the user-visible completion of the steer turn was not observable in the
  transcript window. Prior PASS captured result2 work inside the reply.
  Verdict flip is a terminal-observability regression under the new HEAD,
  not a capability regression (F-NEW-5; relates to F-ORCH-23/26/27 class).
- u-route-research-write PASS (was FAIL): fetch+write now works end to end.
  Routed coder/tooluse (results.jsonl routing block), fetched.txt written
  with the fixture body ("hello world", 11 bytes), reply gives the full
  path. The prior "planner hint-emission gap" did not reproduce this run.
  NOTE: single-run stochastic classifier/planner behavior; not proof of a
  code fix (no routing commits in 1cee89d7..ff19f10f touch this lane).
- u-fail-nonexistent-read PASS (both): honest "does not exist" outcome,
  but STILL routed committer/git (results.jsonl routing: agent_id=committer
  intent=git) - F-ORCH-2 misroute class persists on read-shaped prompts.
- u-plan-mode-request ERROR stall (both): "Plan awaiting user approval
  steps=12" (daemon.log:958, 18:00:38) despite max_plan_steps=4 and
  --auto-approved; no terminal within the 300s liveness window; the turn
  was later FALSELY reaped by the watchdog at exactly 10m (daemon.log:1417,
  18:10:34, "turn reaped ... stale_for=10m0s", task still awaiting approval).
  F-ORCH-13/14/19 + watchdog-false-reap class fully reproduces (F-NEW-6).
- u-direct-explicit PASS (both): direct file write correct, 8.5s.

### 2.3 RPC probes (serial p1..p6; prior probes-prior/ vs probes-gui/)

| Probe/turn            | Prior verdict                    | Rerun verdict                     | Evidence (probes-gui/ vs probes-prior/) |
|-----------------------|----------------------------------|-----------------------------------|------------------------------------------|
| p1 T1 create hello    | completed, EMPTY reply           | completed, EMPTY reply (4.1s)     | p1-naive-continuity.json T1; F-ORCH-15 reproduces |
| p1 T2 edit hello      | FAILED honest (8.1s)             | COMPLETED fabricated (42.5s); hello.txt still 'hello world' | p1 T2; daemon.log:1108,1508+; F-NEW-2 |
| p1 T3 recall change   | completed, "still in progress" contradiction | completed, last-task digest, no contradiction (state now completed) | p1 T3; F-ORCH-16(a) not exercised |
| p1 T4 recall files    | completed, wrong single task     | completed, wrong single task (hello.txt never named) | p1 T4; F-ORCH-16(b) reproduces |
| p1 dispatch_trace     | count=0 for session_id           | count=0 for session_id            | p1 dispatch_trace; F-ORCH-3 reproduces |
| p1 bus dropped        | 44/144                           | 490/590                           | F-NEW-3 |
| p2 A1 plant codeword  | completed, unverified claim, alpha_notes.txt NOT on disk | completed, alpha_notes.txt NOT on disk (committer again) | p2-isolation.json A1; project dir listing |
| p2 B cross-session    | parked ack, never terminalized 300s; 3x "## review: approved" pushes | FAILED terminal 43.6s "max ralph loop iterations"; 0 review pushes; BUT task-...-0010 entered an endless replan storm (F-NEW-10) | p2 B; daemon.log:2044+ |
| p2 A3 codeword recall | FAILED task_get plumbing text    | COMPLETED, answers ZEBRA-4417 correctly, decomposition prose leaks | p2 A3; F-NEW-7 |
| p2 bus dropped        | (prior p2 events small)          | 1390/1490                         | F-NEW-3 |
| p3 F1/F2 empty/white  | accepted:false clean             | accepted:false clean              | p3-failures.json raw_cases |
| p3 F3/F4 unknown ids  | accepted:true phantom convs      | accepted:true phantom convs       | F-ORCH-22 reproduces |
| p3 F5 missing file    | completed 61s, scaffolding title | completed 174s, CLARIFICATION QUESTION instead of answer | F-NEW-9 |
| p3 F6 shell exit 3    | completed 6s, scaffolding        | completed 77s, exit code 3 reported correctly | p3 F6 (improved) |
| p3 F7/F8 closed queue | -32603 queue not found           | -32603 queue not found            | honest error reproduces |
| p3 F9/F10 bad parts/oversize | accepted:true             | accepted:true                     | p3 raw_cases |
| p3 bus dropped        | (prior lower)                    | 3659                              | F-NEW-3 |
| p4 D1 direct          | terminal LOST (300s deadline)    | completed 79.6s (terminal captured) | p4-plan-delineation.json D1 |
| p4 D2 plan            | terminal LOST (420s)             | NO non-parked terminal within 420s; watchdog reaped 18:33:04 | p4 D2; daemon.log:12888 |
| p4 D3 ambiguous       | completed 6.6s (executed steer text as new turn, prior cross-probe contamination) | NO terminal within 420s; reaped 18:36:34; "could not determine intent" scaffolding | p4 D3; daemon.log:14767 |
| p4 D4 refine          | FAILED 233s ralph iterations     | NO terminal within 420s (parked ack only) | p4 D4 |
| p4 plan.list D2/D3/D4 | 0 plans at every checkpoint      | 0 plans at every checkpoint       | F-ORCH-1/13 class: plans live in task metadata |
| p4 bus dropped        | 9853/9953 (concurrent load)      | 25141/25241 (SERIAL; replan storm flood, F-NEW-10) | F-NEW-3 |
| p5 steer acceptance   | ACCEPTED queued, silently ignored (F-ORCH-23) | REJECTED -32603 queue not found on BOTH cid candidates at +8s while primary active | p5-steering.json steer_attempts; F-NEW-11 |
| p5 primary            | completed 11.7s                  | NO non-parked terminal in 360s (parked ack only); only step1.txt written this run (18:39); step2-5.txt are PRIOR-run leftovers (16:22 mtimes); queue_status is_active=false | p5-steering.json; project dir mtimes; F-NEW-12 |
| p6 T1 create          | completed, claims/evidence JSON leak | completed 6.1s, claims/evidence JSON envelope STILL leaks raw in reply | p6-positive-control.json T1; F-ORCH-21 reproduces |
| p6 T2 explicit edit   | completed fabricated, committer/git lane, zero file tools (F-ORCH-20) | completed 4.1s fabricated, BUT routed coder/code conf 1.0 (explicit disambiguation WORKED this run); file_edit attempted with empty anchor -> "missing 'anchor' field" ERROR -> apology narration shipped as completed result; hello.txt STILL 'hello world' | p6 T2; daemon.log:18309,18327-18328; F-NEW-13 |
| p6 T3 recall          | completed 3s                     | NO terminal in 300s (lost or never fired; bus dropped 326/426) | p6 T3; F-NEW-3 class |

NOTE on p4: the rerun ran p4 AFTER p2 on the same daemon; p2's failed
codeword task kept replanning in the background (F-NEW-10) and flooded
the bus during p4. This is a daemon-internal background load, not probe
concurrency - the probes themselves were strictly serial. It is itself
the finding: a failed interactive task must not keep consuming the
machine (and the event bus) for 30+ minutes after its terminal.

### 2.5 TUI smoke (agent-tui, optional scope item - DONE)

Session 5487b721, `agent-tui run --cols 120 --rows 35 --
/tmp/usability-rig/meept chat` with rig MEEPT_HOME/HOME.
- Resume: TUI loaded the most-recent session (the GUI-API-check
  session) showing the 01:49 UTC-stamped "What is 2+2?" turn next to
  18:54 local-stamped new turns - F-ORCH-7 mixed-clock resume
  reproduces. Sidebar: "agents: 10 active / workers: 10 (10 idle)",
  "tasks: 7 pending", "+60 more..." - the replan storm's task debris
  visible as current state (no stale/previous-run marker).
  Screenshot: probes-gui/tui-smoke-resume.txt.
- Sent "What is 2+2? Answer briefly." (18:54): rendered as a
  plan-task streaming card ("plan-task-20... 10%... streaming
  response... 6.2k tokens"); still streaming with NO answer after
  105s (screenshots tui-smoke-2plus2.txt, tui-smoke-2plus2-final.txt).
  F-ORCH-5 class (trivial question -> full task machinery) reproduces;
  the stall is consistent with F-NEW-10 background load. Session killed
  after capture. NOTE: agent-tui press rejects "enter" (use "Return").

### 2.4 GUI live checks (API level; browser drive blocked for loopback)

| Check                                  | Result | Evidence |
|----------------------------------------|--------|----------|
| GET /api/v1/daemon/status              | 200, running, 2 runtimes healthy, budget disabled | gui-api-checks.json |
| GET /api/v1/sessions                   | 200, count 34 | gui-api-checks.json |
| GET /api/v1/tasks                      | 200, count 47 | gui-api-checks.json |
| GET /api/v1/plans (no project_id)      | CONNECTION DROP - nil-pointer panic in handler (3x) | gui-api-checks.json; daemon.log 18:01:15/32 (F-NEW-1) |
| GET /api/v1/plans?project_id=x         | 200 {"count":0,"plans":null} (merge branch skipped) | curl probe 18:08 |
| GUI static server 57735                | 200 index.html (flutter_bootstrap) + main.dart.js served | gui-api-checks.json |
| WS wss://127.0.0.1:57734/ws upgrade    | 101, Sec-WebSocket-Accept verified, status/subscribed frames | gui-api-checks.json |
| POST /api/v1/sessions (create)         | 201, session+conversation ids returned | gui-api-turn.json session_create |
| POST /api/v1/chat/submit + terminal    | 200 accepted; WS turn.terminal (parked/async_dispatch ack) delivered in 1.15s with task_id/intent_type/agent_id/classified_by; task state observable via GET /api/v1/tasks (planning->executing); BUT the COMPLETION terminal never arrived on WS and the answer never landed in REST messages within 3.5 min (task still executing; only user msg + "## starting task" ack persisted) | gui-api-turn.json; F-NEW-14 |
| GET /api/v1/sessions/{id}/messages     | 200, history present for SESSION-keyed submit (no FK failure on this path) | gui-api-turn.json messages_final |
| RPC plan.list / plan.list_by_session   | 200-equivalent, 0 plans (task-gate plans live in task metadata, not plans.db) | rpc.py output 18:04 |

--------------------------------------------------------------------------
## 3. NEW FINDINGS (this rerun)
--------------------------------------------------------------------------

F-NEW-1 (CRITICAL, LIVE+REPRO): GET /api/v1/plans panics the HTTP handler
when the evolver sink store is a typed-nil interface - the GUI plans tab
cannot load at all on a default-config daemon.
- Live: 3 panics "http: panic serving ...: runtime error: invalid memory
  address or nil pointer dereference" (daemon.log 18:01:15.329,
  18:01:32.118, 18:01:32.131), one per GET /api/v1/plans attempt; client
  sees connection reset, no HTTP status. Stack:
    plan.(*SQLiteStore).ListPlans(0x0, ...) store_sqlite.go:352
    services.(*PlanService).List plan_service.go:127   <- FALLBACK branch
    http.(*Server).handlePlanList api_handlers.go:3096
- Mechanism (CODE): PlanService.List guards s.store == nil
  (plan_service.go:113) but the evolver-sink merge at :126-127 only checks
  s.fallbackStore != nil. SetEvolverSink (:27-33) "ignores nils" with a
  plain interface != nil test. daemon.go:1215 passes
  nilSafeEvolverPlanStore(components) whose return type is *plan.SQLiteStore
  (daemon.go:3481-3486); a nil CONCRETE pointer boxed into the
  plan.PlanStore interface field (services/service.go:92) is NOT == nil,
  so the guard passes and ListPlans runs on a nil receiver.
- REPRO: /tmp/usability-rig/repro/plan-nil-panic-repro.go (throwaway
  program, run via `go run` from a temp package dir inside the worktree;
  the zz_gui_probe/ dir was REMOVED from the worktree after capture):
  case 1 typed-nil primary store panics at store_sqlite.go:352;
  case 2 real primary + SetEvolverSink(mgr, (*plan.SQLiteStore)(nil))
  panics at plan_service.go:127 + store_sqlite.go:352 - BYTE-IDENTICAL
  stack to the live daemon.
- Discriminator (LIVE): GET /api/v1/plans?project_id=x returns 200
  {"count":0} because the merge branch requires projectID == ""
  (plan_service.go:126). RPC plan.list is unaffected (different handler,
  rpc/plan.go, returns count 0 cleanly).
- Attribution: ALL files on the panic path are byte-identical between
  1cee89d7 and ff19f10f (git diff --quiet per file: components_wiki.go,
  plan_service.go, service.go, store_sqlite.go, api_handlers.go,
  evolver_approval_bridge.go). The sink wiring landed 09-30 (c3e34c83),
  long before the audit base. This is a LATENT bug the prior audit never
  exercised (prior GUI was build-broken; notes-server.md listed the route
  statically; daemon-prior.log has 0 http panics). NOT a regression of the
  10 new commits. Open question for the fixer: why the live rig's
  EvolverPlanStore is nil while EvolverPlanManager is non-nil with
  skills.evolver.enabled=false (config get confirms false; no
  plans-evolver.db in the rig home; no evolver boot log lines) - the
  typed-nil guard must be fixed regardless of which wiring path injects
  the nil (reflect-based nil check or concrete-typed seam).
- Impact: GUI /plans tab (plan_provider.dart REST list) gets a network
  error on every load with default config; also any API client listing
  plans without a project filter. Fix sketch: nil-check the concrete
  pointer before boxing (nilSafeEvolverPlanStore returns plan.PlanStore
  with an explicit nil return), or reflect.ValueOf(store).IsNil() in
  SetEvolverSink, plus a regression test: NewRegistry with nil evolver
  sink -> Plan.List returns (empty, nil), never panics.

F-NEW-2 (MAJOR, LIVE): p1 T2 file-edit misroute now FABRICATES SUCCESS
instead of failing. "Now change hello.txt so it says 'hello meept'" ->
intent=git conf 0.856 agent=committer (daemon.log:1108), 6-step task,
turn.terminal status=completed handler=task_completed_relay, but
hello.txt on disk is STILL 'hello world' (cat verified 18:11). The loop's
"Unbacked file side-effect claims with zero tool executions" nudge fired
(daemon.log 18:10:48.740, 87 occurrences session-wide) yet the task still
completed and review approved. Prior run: same prompt FAILED honestly
(probes-prior/p1 T2 status=failed). The claim-without-evidence class
(F-ORCH-20) is now worse on this lane: silent false success.
Evidence: probes-gui/p1-naive-continuity.json turn 2; daemon.log
1508-1690 (committer task-20261120011047, zero file tools for the edit).

F-NEW-3 (MAJOR, LIVE): bus.poll drop-oldest buffer loss is dramatically
worse under the new HEAD's event volume: p1 dropped=490 of 590 events
(prior p1: 44 of 144); p2 dropped=1390 of 1490. turn.terminal delivery
to polling clients (TUI uses the same bus.subscribe/bus.poll path) is
now unreliable even for SHORT SERIAL single-session runs - no concurrent
load needed. F-ORCH-26 class, amplified. Evidence: probes-gui/*.json
"dropped" fields; probe_gui.py poll loop.

F-NEW-4 (MAJOR, LIVE): security-denied file write still finalizes
"completed" with a narration of the denial as the step result; no retry
with the correct workdir-relative path, no user-facing error terminal.
u-context-two-step primary: model hallucinated
/private/tmp/usability-u-context-two-step-a1/counter.txt (missing
usability-rig/worktrees-gui), deny at daemon.log:496-497, task finalized
completed steps_completed=1 (daemon.log 17:59:09.572). The user is told
"task completed" while the result text itself says "The file was not
created". F-ORCH-9/20 class + review gap.

F-NEW-5 (MAJOR, LIVE): steer-as-new-turn work completes on disk but its
terminal/reply is not observable in the bench transcript window
(u-steer-midrun: result2.txt written, no second-turn chat_message or
terminal captured; final_reply is the primary's). Prior clean run
captured it. Either terminal delivery loss (F-NEW-3 buffer) or a
relay-path change; needs a targeted re-probe with task.list_extended
fallback. Evidence: bench-results-gui/transcripts/
usability-u-steer-midrun-a1.json (tool_trace 11 events, all primary).

F-NEW-6 (MAJOR, LIVE): watchdog falsely reaps a legitimate
human-approval wait at exactly 10m: u-plan-mode-request turn
turn-e6b9c68ad2c007bb parked at "Plan awaiting user approval steps=12"
18:00:38, reaped 18:10:34 "no progress for 10m0s" -> status=failed
"this turn stopped responding and was marked failed" while the task
still awaits approval. The user gets a false failure AND the plan stays
invisible. queue-gates #7/#11 class reproduces cleanly on serial load.
Evidence: daemon.log:958,1417; probes-gui/p1 events (reaped terminal
captured by the still-open p1 subscription).

F-NEW-7 (MINOR, LIVE): recall replies improved but still wrong-shaped:
p1 T3/T4 now answer from the last-task digest with status=completed
(no more "still in progress" contradiction on a failed task - prior
F-ORCH-16(a) did not reproduce because T2 now falsely completes), but
T4 "What files did you make for me in this session?" still answers with
ONLY the last task ("Prior task: Now change hello...") and never names
hello.txt - the session's actual artifact. F-ORCH-16(b) persists.
p2 A3 codeword recall now SUCCEEDS ("I've retrieved the secret codeword
ZEBRA-4417 from memory") where prior run failed with task_get plumbing
text - but wrapped in a 2-step orchestrator task with decomposition
prose ("To decompose this request into 4 executable steps...") leaking
into the user reply. Evidence: probes-gui/p1 T3/T4, p2 A3.

F-NEW-8 (MINOR, LIVE): context firewall truncation is now DOUBLE-logged
and still fires on ordinary turns: 648 truncation WARNs this session,
324 of them the NEW "window-independent bound" line added by a6608719
(L10/L11) alongside the pre-existing "to fit model window" line - every
oversized system message now logs two WARNs. The a6608719 change bounds
the cut to a UTF-8 rune boundary and runs before the inertness
early-return (safety improvement) but does NOT stop silent truncation
on trivial turns (F-ORCH-17 persists; first turn of p1's fresh session
truncated at messages=3). Evidence: daemon.log 18:09:5x onward;
git show a6608719 -- internal/llm/context_firewall.go.

F-NEW-9 (MAJOR, LIVE): p3 F5 missing-file read answered with a
CLARIFICATION QUESTION instead of the requested plain statement, after
174s (prior: 61s, same "could not determine intent" scaffolding title).
Reply result: "What would you like me to validate? Please provide a file
path or specify what you'd like me to check." - the file path WAS in the
prompt; the orchestrator step lost it. Both runs finalize "completed"
for a turn that never answered the user. F6 shell-fail improved: exit
code 3 now reported correctly (prior run also completed but with the
clarification scaffolding; 77s vs 6s). Evidence: probes-gui/
p3-failures.json turns F5/F6 vs probes-prior/p3-failures.json.

F-NEW-10 (CRITICAL, LIVE): a FAILED task replans forever, re-broadcasts
its failure terminal on every cycle, and floods the event bus. p2 B's
codeword task task-20261120011106.011798000-0010 failed at 18:11:46
("max ralph loop iterations reached without sufficient evidence",
terminal delivered, probe moved on) but kept running: 113 "Triggered
replan" lines for that ONE task through 18:40:59 (iteration=3
max_iterations=3 repeated endlessly - the cap never sticks across
replan cycles), 43 "Task failed, pushing error to chat" relays, each
with a FRESH turn_id (turn-fd9e4399..., turn-74e9c51e..., 30+ distinct
ids captured in p4's event stream). Consequences: (a) the user's chat
stream receives the same failure over and over as new turns; (b) the
bus flood dropped 25141 of 25241 events in p4 (F-NEW-3 amplifier) and
starved p4 D2/D3/D4 of observable terminals; (c) 30+ minutes of local
LLM compute burned on a dead task. ralph_loop.go is byte-identical
between the bases (git diff --quiet), so this is a LATENT lifecycle
bug exposed by the serial rerun ordering, not a regression of the 10
commits. Prior run's p2 B never terminalized at all (parked), so the
storm class was not visible then. Evidence: daemon.log:1808-1811
(first replan), :2044 (first failure relay), :16796/:16945/:17216
(still failing at 18:40), grep -c "Triggered replan.*0010" = 113;
probes-gui/p4-plan-delineation.json events (30+ failed task_failed_relay
terminals for "what is the secret codeword?"). Fix sketch: terminalize
the task durably on max-iterations (state=failed, stop the replan
pump), dedupe failure relays by task_id, and never mint new turn_ids
for a re-relay.

F-NEW-11 (MAJOR, LIVE): chat.steer to an ACTIVE turn's conversation is
rejected "queue not found" (-32603) on both the raw conv id and the
deterministic thread id, +8s into a multi-step primary that is still
executing. Prior run: same shape ACCEPTED {"queue":"steer","status":
"queued"} then silently ignored (F-ORCH-23). The failure mode flipped
from silent-drop-with-receipt to honest-rejection - better than a false
receipt, but mid-run steering is now UNUSABLE on this lane (the queue
is not registered under any id the client can derive). Queue lifecycle
code unchanged between bases; timing/lane variance. Evidence:
probes-gui/p5-steering.json steer_attempts vs probes-prior/p5-steering.json.

F-NEW-12 (MAJOR, LIVE): p5's 5-step primary never terminalized within
360s (parked ack only) and only step1.txt was written this run;
queue_status afterwards: is_active=false, steering_depth=0 - the queue
died without finishing the work or telling the user. Prior clean run:
completed 11.7s. The daemon was under the F-NEW-10 replan-storm load at
the time, so this is load-sensitive, but the contract violation stands:
no terminal, no error, partial work, silent queue death. Evidence:
probes-gui/p5-steering.json (terminal=None, wait 360.06), project dir
mtimes (step1.txt 18:39 vs step2-5.txt 16:22 prior-run leftovers).

F-NEW-13 (MAJOR, LIVE): p6 T2 explicit file edit: routing IMPROVED
(coder/code conf 1.0 - the "this is a file edit, not a git operation"
disambiguation worked this run, vs committer/git 4/4 prior), but the
edit still failed and the failure was shipped as SUCCESS: file_edit
called with anchor:"" -> ERROR "edit 1: missing 'anchor' field"
(daemon.log:18327-18328), the loop narrated an apology ("I apologize
for the technical issue... I'll first read the current content..."),
and THAT narration became the completed step result; turn.terminal
status=completed, hello.txt unchanged ('hello world', cat verified).
A tool-schema error (empty anchor) plus review approving an
apology-as-result is the F-ORCH-20 claim-without-evidence class with a
new proximate cause. Evidence: probes-gui/p6-positive-control.json T2;
daemon.log:18309-18328.

F-NEW-14 (MAJOR, LIVE): GUI-path chat turn ("What is 2+2?" via REST
submit on a freshly created session): the parked ack terminal arrived
on WS in 1.15s carrying task_id/intent_type/agent_id/classified_by
(the fields both GUI clients parse-then-drop, prior audit Q2), task
state progressed planning->executing on GET /api/v1/tasks, but the
COMPLETION terminal never arrived on the WS chat/progress channels and
the answer never appeared in GET /api/v1/sessions/{id}/messages within
3.5 minutes (task still executing at 18:52, submitted 18:49). A simple
question taking >3.5 min with no user-visible completion is the
F-ORCH-5/13/14 class on the GUI's own transport. NOTE: the raw WS
client held chat+progress session subscriptions exactly as
websocket_service.dart does; the daemon-side replan storm (F-NEW-10)
was active concurrently. Evidence: gui-api-turn.json (ws_frames,
task_polls, messages_final), daemon.log:18637-18638.

--------------------------------------------------------------------------
## 4. WHAT CHANGED VS THE PRIOR REPORT (attribution)
--------------------------------------------------------------------------

git log 1cee89d7..ff19f10f (10 commits):
  ff19f10f docs(audit): bughunt report + disposition ledger
  3df1f1ab fix(daemon,tui): plain bots valid escalation targets (L13)
  4e01f463 fix(session,agent,tui,gui): durable user renames
  5494bf54 fix(voice): rumble stt_base_url derivation
  a6608719 fix(agent,llm,groups): collaboration budget opt-in, diagnosis
           vocabulary parity, firewall bound on every path (M7/M8/L10/L11/L19)
  120fe26c fix(daemon,voice,config): reload-live warm window, rumble GPU
           clamp, roster snapshot, ring text cap, filler re-arm
  202c0994 fix(gui): rename persistence, unified roster list,
           collapse-aware keyboard nav, palette scroll
  b1faf7f4 fix(tui): drill-in error state, rename validation,
           project-aware naming, cursor preservation
  98153249 fix(config): classify jev gate contract as optional template
  cf139405 test(e2e): pin classifier to chat in pool request-fault T13

NONE of the 10 commits touches: dispatcher routing/classifier veto,
task_approval.go (reject-approves bug), recall_answer.go,
dispatch_trace.go, bus proxy MaxEvents, tactical semaphore scheduling,
queue steer lifecycle, chat_submit validation, or the plan approval
threshold. Verified: git log 1cee89d7..ff19f10f -- <those paths> is empty.
Accordingly:

REPRODUCES (unchanged code, live-confirmed this rerun):
- F-ORCH-2/20 git-lane misroute of file edits (p1 T2, p2 A1, bench
  u-fail-nonexistent-read routing block) - and now with fabricated
  success (F-NEW-2). PARTIAL improvement: p6 T2's EXPLICIT
  disambiguation routed coder/code conf 1.0 this run (prior 4/4
  committer); the implicit lane is still broken (stochastic variance,
  no classifier commits in the range).
- F-ORCH-3 dispatch_trace session_id keying: count=0 for the primary
  session id in p1 (probes-gui/p1 dispatch_trace).
- F-ORCH-13/14/19 invisible approval gate + over-decomposition +
  hardcoded threshold: bench u-plan-mode-request 12 steps awaiting
  approval despite max_plan_steps=4 (gate counts pre-truncation).
- F-ORCH-15 empty completed reply: p1 T1 reply_len=0 again.
- F-ORCH-16(b) last-task-only recall digest: p1 T4.
- F-ORCH-22 phantom sessions: p3 F3/F4 accepted:true with fabricated
  conv ids (probes-gui/p3 raw_cases).
- F-ORCH-26 bus drop-oldest terminal loss: amplified (F-NEW-3).
- F-ORCH-23 steer failure class: shape flipped from accepted-then-
  ignored to queue-not-found rejection + silent queue death (F-NEW-11/12).
- F-ORCH-7 TUI mixed-clock stale resume: TUI smoke section 2.5.
- F-ORCH-5 trivial question -> full task machinery: TUI 2+2 streamed
  as a plan-task card, no answer in 105s; GUI REST 2+2 still executing
  after 3.5 min (F-NEW-14).
- F-ORCH-29 conversation-only FK history loss: 10 FOREIGN KEY WARNs
  this session (daemon.log), every bench conv hit it.
- Watchdog false-reap of approval waits (F-NEW-6; queue-gates #7/#11).
- F-ORCH-6 step-not-found spam: 14 occurrences.
- F-ORCH-19 unknown tool_hint warns: 14 occurrences.

DID NOT REPRODUCE THIS RERUN (stochastic or load-dependent; code unchanged):
- F-ORCH-16(a) "still in progress" on a failed task: T2 now falsely
  completes, so the failed-state branch was never exercised.
- F-ORCH-24 codeword recall failure: p2 A3 now answers ZEBRA-4417
  correctly (memory lane worked this run; prior failure was a task_get
  decomposition accident). Reply shape still cluttered (F-NEW-7).
- F-ORCH-25 "## review: approved" chat pushes: 0 such frames in p2
  events and 0 in daemon.log this session (prior: 3 frames). Code path
  unchanged; likely load/timing dependent. Keep open.
- F-ORCH-28 semaphore hang: no hang under serial load (prior clean run
  also had none; it was a concurrent-load phenomenon).
- u-route-research-write planner hint gap: PASS this run (stochastic).

FIXED BY THE NEW HEAD (attributable):
- F-ORCH-18 GUI web build: fixed by THIS WORKTREE'S one-line rename (not
  by the 10 commits; they do not touch platform_open_web.dart). Build
  green, 796 tests pass, bundle served.
- Context firewall UTF-8 cut + every-path bound (a6608719 L10/L11):
  safety improvement landed; silent truncation itself persists (F-NEW-8).
- Collaboration-lane hidden 50k token budget default removed (a6608719
  M7): not directly exercised by this battery (no collaboration tasks).
- GUI rename persistence / roster / palette / keyboard-nav fixes
  (202c0994, 4e01f463) and TUI drill-in/rename/cursor fixes (b1faf7f4):
  landed; static-only verification here (no live GUI/TUI drive of those
  flows this rerun; TUI smoke section 2.5).

NEW AT THIS RERUN (latent, exposed by first-ever live GUI-API pass and
serial probe ordering):
- F-NEW-1 plans REST panic (section 3). Latent since 09-30 sink wiring;
  prior audit could not hit it (GUI build broken, route never called).
- F-NEW-10 failed-task replan storm (section 3). ralph_loop.go
  unchanged; prior p2 B parked forever so the post-failure lifecycle
  was never observed. Exposed by p2 terminalizing this run.

--------------------------------------------------------------------------
## 5. LIMITATIONS
--------------------------------------------------------------------------

- No live browser GUI interaction: the browser_exec tool blocks loopback
  URLs and the daemon HTTPS cert is self-signed. GUI verification is
  API-level (REST + raw RFC6455 WS client, gui_api_check.py) plus static
  bundle checks. Click-through of the deep-link window.open path, the
  plans tab rendering, and WS reconnect behavior in a real browser remain
  unverified (prior report I4 still open).
- The release bundle minifies symbols: grep for openExternal/_blank in
  main.dart.js is not a valid oracle (0 hits even for known-present
  literals); compile success + deep-link string literals are the evidence.
- Probes ran SERIALLY on a fresh daemon (no load contamination), but the
  local 8B + prompt-router are stochastic: single-run verdict flips
  (u-steer-midrun, u-route-research-write, p2 A3) are reported as
  observations, not proof of code-level change. No routing/steering code
  changed between the bases (section 4), so flips are behavioral variance
  or event-delivery loss, not fixes.
- p4/p5 deadlines are long (up to 420s/turn); any probe exceeding its
  deadline is recorded as a finding, not silently retried.
- The exact wiring path that leaves EvolverPlanStore typed-nil with
  evolver disabled was not fully pinned (F-NEW-1 open question); the
  panic mechanism itself is proven by byte-identical isolated repro.
- TUI smoke: DONE (section 2.5); limited to one question + resume
  observation under replan-storm load; palette/navigation flows not
  re-exercised this rerun.
- flutter test covers the VM/native half of the platform opener; the web
  half is only pinned by the web build (no headless browser test infra).
- Worktree final state: ONLY the one-line fix (modified) + the new
  contract test (untracked). The throwaway repro program was moved to
  /tmp/usability-rig/repro/ and its package dir deleted, so the
  pre-commit root-artifact check will not trip. probe_gui.py/*_gui.py
  copies live in /tmp/usability-rig only.

--------------------------------------------------------------------------
## 6. RANKED RECOMMENDATIONS
--------------------------------------------------------------------------

1. (P0) Fix F-NEW-1 plans REST panic: concrete-nil-safe seam for the
   evolver sink (return plan.PlanStore nil explicitly from
   nilSafeEvolverPlanStore, or reflect-nil guard in SetEvolverSink) +
   regression test (default-config registry -> Plan.List no panic).
   Unblocks the GUI plans tab for every default-config install.
2. (P0) F-NEW-10 replan storm: durable terminalization on max ralph
   iterations (stop the replan pump, state=failed), dedupe failure
   relays by task_id, never mint fresh turn_ids for re-relays. One
   failed question burned 30+ min of local LLM, flooded the bus
   (25k dropped events), and starved every subsequent probe of
   terminals. This is the single biggest usability multiplier found
   this rerun.
3. (P0) Commit the one-line openExternal rename + the new contract test
   (this worktree). Web build is green; do not ship another HEAD with a
   broken web build. Add `flutter build web --release` to CI if absent.
4. (P1) F-NEW-2/13/F-ORCH-20: review must block "completed" when a
   file-shaped step's tool ledger is empty OR the only tool call
   ERRORED (empty-anchor file_edit); apology/narration text is not a
   result. Classifier git-veto fix (prior W8) remains the routing-side
   cure for the implicit-edit lane (p1 T2 still committer/git).
5. (P1) F-NEW-3/F-ORCH-26: critical-topic lane exempt from the 100-event
   drop-oldest trim (turn.terminal/task.completed/task.failed/plan.*),
   plus client dropped>0 re-query. Serial single-session runs now lose
   terminals; this is the top observability defect.
6. (P1) F-NEW-6: watchdog must exclude awaiting_approval/planning turns
   (prior W6); the 10m false-reap converts an invisible wait into a
   false failure (reproduced 3x this session: bench plan turn, p4 D2,
   p4 D3).
7. (P1) F-ORCH-13/19: make the task approval gate visible (subscribe
   task.pending_approval in TUI/GUI, approval card) and config-controllable
   (threshold key; respect --auto-approved semantics or document it).
8. (P1) F-NEW-11/12 + F-ORCH-23: steering queue must be registered under
   a client-derivable id for the WHOLE active turn, and a queue that dies
   mid-work must terminalize the turn (p5: partial work, silent death).
9. (P2) F-NEW-4: security-denied tool effects must terminalize the step
   as failed (or retry once with workdir-relative path), never finalize
   completed with denial prose.
10. (P2) F-ORCH-15/W3: never ship empty completed replies (p1 T1 again).
11. (P2) F-ORCH-29/W11: session-row upsert before persistExchange (10 FK
    WARNs this session; bench history still silently lost).
12. (P2) F-NEW-8: disclose firewall truncation to the user (or raise the
    budget ratios); dedupe the double WARN now that two bounds log.
13. (P2) F-NEW-14: GUI chat completion must be observable on the GUI's
    own transport (WS completion terminal + REST messages) within a
    bounded time for trivial questions; add a GUI-path liveness test.
14. (P3) Live browser GUI pass (manual or CDP attach) to close prior I4:
    plans tab, deep-link click-through, WS reconnect, rename flows.

--------------------------------------------------------------------------
## 7. ARTIFACT INDEX (this rerun)
--------------------------------------------------------------------------
- flutter-build-rerun.log            web build exit 0
- flutter-test-rerun.log             794 pass (pre-new-test)
- flutter-test-rerun2.log            796 pass (with contract test)
- daemon.log                         rebuilt daemon, whole rerun window
- daemon-prior.log                   prior daemon log (backup)
- bench-gui.log, bench-results-gui/  bench rerun (results.jsonl, transcripts/)
- run_bench_gui.sh                   bench script (fresh out/scratch dirs)
- probes-gui/                        p1..p6 JSON + logs + chain.log
                                     + tui-smoke-*.txt screenshots
- probes-prior/                      prior probe JSON/logs (backup)
- gui_api_check.py                   REST + raw WS client (readonly/turn)
- gui-api-checks.json                readonly GUI API results
- gui-api-turn.json                  chat-turn-over-REST+WS results
- probe_gui.py, p[1-6]_*_gui.py      probe copies writing probes-gui/
- Worktree changes (UNCOMMITTED):
    ui/flutter_ui/lib/core/platform/platform_open_web.dart (the fix)
    ui/flutter_ui/test/core/platform_open_contract_test.dart (new pin)
- repro/plan-nil-panic-repro.go      F-NEW-1 isolated reproduction