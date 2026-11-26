# Bughunt 2026-11-25 - since 2026-10-15 (coverage review + audit of the two gaps)

Repo: /Users/caimlas/git/meept, branch main. Request: "bughunt since oct 15".
Mode: READ-ONLY. No production code was edited. No commits. Fixing needs a
separate "fix all" instruction.

## What this wave is

The window 2026-10-15..2026-11-25 holds 1,730 commits. Fourteen prior reports
in `.hermes/audits/` already cover most of it. This wave did two things:

1. It mapped which commits each prior report read (coverage review).
2. It audited the two ranges that no report covers.

## Coverage map (verified from each report's stated anchor + git)

| Range | Commits | Covered by |
|---|---|---|
| 2026-10-15..10-16 | - | meept-audit-2026-10-15-wave.md, meept-audit-2026-10-16-wave.md |
| 2026-10-16..10-19 | - | meept-audit-2026-10-19-bughunt-2week.md, 2026-10-20-security-audit.md |
| 2026-10-19..10-30 | 152 | 2026-10-30-bughunt-wave.md |
| **GAP 1: 2026-10-30..11-06** `0a446a21..b3c03f80` | **282** | **No consolidated report.** Per-slice files exist in 2026-11-06-wave/ and 2026-11-07-wave/ (9 files, 5 scopes) with no disposition table. |
| **GAP 2: 2026-11-06..11-11** `7b510ed4..ea3d7082` | **446 non-merge** | **None.** The 2026-11-11 report read 7 commits (`ea3d7082..436f432f`). |
| 2026-11-11..11-14 | - | 2026-11-11-bughunt-readonly.md, 2026-11-11-fixwave-accuracy.md |
| 2026-11-12..11-15 | - | 2026-11-14-bughunt-week.md, 2026-11-15-bughunt-two-run.md |
| 2026-11-15..11-17 | 162 | 2026-11-17-bughunt-wave.md |
| 2026-11-17..11-20 | 79+ | 2026-11-19-bughunt-20day.md, wave-2026-11-19b/, 2026-11-20-bughunt-7day.md, voice-sidecar 11-18 |
| 2026-11-20..11-21 | 24 | 2026-11-21-bughunt-2day.md |
| 2026-11-21..11-24 | 161 | 2026-11-24-bughunt.md |
| 2026-11-24..now | 27 | Sibling fix commits for the 11-24 report (not audited; see ledger) |

Record correction: `2026-11-14-bughunt-week.md` line 3 says the 2026-11-11
report "covers everything before" `5840a51e`. That statement is wrong. The
2026-11-11 report's own scope is 7 commits.

Size of the gaps: Gap 1 has 47,796 production lines still live at HEAD (git
blame), largest in internal/daemon (6,344), internal/agent (5,969),
internal/voice (4,548). Gap 2 added 53,306 Go lines (476 files).

## Auditors (7, read-only, disjoint scopes, 26 min)

1. daemon / comm / rpc / activity / gatecapture / metrics (Gap 1).
2. agent / plan (Gap 1).
3. llm / memory / groups / security / config / queue-cluster / codemode (Gap 1).
4. voice (Gap 2).
5. non-voice: ledger / contact / slack / queue / mcp / agent fixes (Gap 2).
6. employee / tools / hermes / acp / tui (Gap 2).
7. voice + eval + gatecapture (Gap 1).

Reports: `.hermes/audits/2026-11-25-bughunt-gaps/01..07-*.md`.

Every HIGH below was re-read by the parent at HEAD. Two auditor HIGHs were
re-graded (one down, one up). The parent also widened one finding to a class.

## Baseline

- `go build ./...`: not re-run this wave. HEAD builds under the sibling's
  pre-commit gate (27 commits landed since 12:35).
- `go test -p 2 -count=1 ./...` at HEAD 997d6d02: 158 ok, 4 FAIL lines, 31
  no-test-files. Log: `/Users/caimlas/.git/worktrees/bughunt-logs/baseline-test-20261125.log`.
- Both red packages are WORKING-TREE state from sibling sessions, not the
  committed tree:
  - `internal/llm [build failed]`: the sibling's uncommitted revert of the
    `transient_retries` lane (4 files, -217 lines) leaves the committed
    `client_transient_retry_test.go` referencing removed symbols. HEAD holds
    all symbols. Same cause as the 11-24 report.
  - `internal/employee` (2 tests): the M17 fix was mid-landing during the run.
    The test file was written at 12:49:47, the baseline ended 12:45:47, and
    commit 7b7ab262 landed at 12:50:50. A test-ahead-of-code window.
- Effective committed-tree baseline: not re-measured after the sibling's 27
  commits. NOT VERIFIED.

## HIGH (9, parent-verified at HEAD c5d54c9e)

Map to auditor IDs: H1=A1-1, H2=A1-2, H3=A1-3, H4=A1-4, H5=A1-5,
H6=A2-1, H7=A3-1, H8=A6-1, H9=A6-7 (widened by the parent to a class).
Re-graded: A1-6 HIGH -> M3, A6-2 HIGH -> M2, A6-7 MEDIUM -> H9.

H1. (A1-1) `Ring.Tail(n)` returns the OLDEST n records. `activity.recent` freezes.
    `internal/activity/ring.go:63-66`: the loop starts at `head`, which
    `:15` defines as "index of the oldest element". The doc at `:50` says
    "newest n records". Sole production caller `internal/daemon/activity_rpc.go:115`.
    Trigger: after 1001 events (capacity 1000) the RPC returns a static window
    of the 50 oldest events. Blast: the "live tail" shows stale data forever.
    Tests cover only `n >= size`.

H2. (A1-2) Data race in `Aggregator.Query` on `pendingPlanIDs`.
    `internal/activity/aggregator.go:103-107` writes the field on every query
    with no lock; `internal/activity/plans.go:50` ranges it. The same file
    guards `planIndex` with a mutex and says "RPC handlers may query
    concurrently" (`plans.go:9-10`). One Aggregator is shared by
    `activity.query`, `activity.overview`, and the voice day-brief.
    Trigger: two concurrent polls. Blast: a Go memory-model race; plan
    stamping degrades.

H3. (A1-3) Slack, voice, and SMS webhooks skip the rate limiter. Comments say the
    opposite. `internal/comm/http/server.go:2142-2170`: three branches call
    their handler and `return` before `handler.ServeHTTP` at `:2172`, the only
    place wrapped by `rateLimitMiddleware` (`:2070`). The package has one
    limiter (`:2050`). The kanban pairing branch at `:2091` wraps itself.
    Blast: unauthenticated unthrottled endpoints that do an HMAC or Ed25519
    check over a 1 MiB body. The access log is skipped too.

H4. (A1-4) The gate-capture resolution lane is dead on the DEFAULT boot.
    `internal/daemon/components.go:8760` takes `gateCap *gateCaptureStore` BY
    VALUE. Inside `registerBuiltinTools`, `:8872` assigns a new store to that
    value parameter. The caller's variable at `:4063` is still nil, so
    `wireGateCaptureResolve` (`:4068`) never runs. The default preset is
    `workspace` (`internal/config/schema.go:4881`), which asks on `sudo`,
    `git push`, `bash -c`, and 4 more. Effects on default config:
    (a) `askArmed` (`gatecapture_wiring.go:107`) is a `sync.Map` with no bound
    and its only consumer is never wired, so it grows for the daemon's life;
    (b) zero `permission_table_ask` rows are recorded; (c) the orphan store is
    never closed (`c.GateCapture` has one write and no read).

H5. (A1-5) Tool-gate effects key has no session or agent. `internal/daemon/toolgate.go:386`
    builds `EffectKey("gate.tool", name, argsHash)`. The claimant is
    per-session (`:289-292`). `Claimant.Matches` is a full struct compare
    (`internal/effects/effects.go:60`), and every resolve checks it
    (`ledger_sqlite.go:274`). No builtin tool declares `read_only` (grep
    `RiskReadOnly` under internal/tools/builtin: 0 hits), so every builtin
    call reserves. Trigger: session B runs the same `(tool, args)` that
    session A ran. Blast: B's success logs `ErrClaimantMismatch` as
    "effects receipt failed"; while A is in flight, B runs the same effect a
    second time; a crashed claimant leaves a row the reconciler Error-logs at
    every boot (`effects_wiring.go:88`).

H6. (A2-1) The group `fs_roots` fence is never armed. `UnionFence` is dead code.
    `internal/agent/fence_stage.go:68-78`: `injectSessionFence` returns
    `base` (non-nil) when the factory is nil, when `NoFence` is set, or when
    the path is empty. So `fence != nil` is always true on a live loop. The
    callers use `if fence != nil {...} else if` for the group branch:
    `internal/agent/handler.go:4286-4292`, `internal/agent/dispatcher.go:3807-3813`,
    `:3872-3878`. A fourth site, `dispatcher.go:3972`, has no group branch at
    all. `injectGroupFence` has zero reachable callers. `UnionFence`
    (`fence_stage.go:181`) has zero callers of any kind.
    Blast: an agent-group member's `fs_roots` write boundary is unenforced on
    every lane. The code comment at `:171-174` says "the session fence is
    injected by the caller AFTER this" and "UnionFence composes both". Neither
    is true. Reach: `groups.Resolved{Managed: true}` is produced at
    `internal/groups/resolve.go:273`.

H7. (A3-1) Tier-policy "compress" enqueues jobs that always fail, then the same
    content is evicted. OPT-IN (`memory.tiers.enabled`, default false,
    `internal/daemon/tiers_engine.go:313`).
    `internal/memory/tierpolicy/execute.go:139-141` builds an `L1ExtractPayload`
    with no `L0Path`. `internal/memory/distill_async.go:239-241` rejects an
    empty `L0Path`. The engine counts it as compressed at `execute.go:97`.
    The hard band (`engine.go:396-420`) emits compress and then evict in the
    same pass, from the same oldest-first candidate list. The engine's own
    comment states the contract: "compress FIRST ... evict only what
    compression cannot fit". Blast: with tiers on, the hard band deletes
    episodic rows that were never distilled. Graded HIGH for data loss.

H8. (A6-1) The goal-tool "direct human" gate is one process-global flag.
    `internal/tools/builtin/goal_tools.go:169` is a package-level
    `atomic.Bool`. The round driver sets it per employee
    (`internal/employee/goal_round_driver.go:374-375`) and tracks `inflight`
    per employee (`:102`). `requireDirectHuman` (`goal_tools.go:184`) guards
    create, edit, pause, and resume. A cron trigger calls `Manager.Trigger`
    (`cron_trigger.go:173`), which runs a goal-loop round with no flag check.
    Blast: any round that overlaps a driver round for ANOTHER employee passes
    the gate. A cron or webhook round can move a blocked goal to active (the
    C-P3 human-gated edge). A driver round that ends first flips the flag off
    and a sibling's legal self-block is refused.

H9. (A6-7, widened) The central fence reads only the arg key `path`. 18 tools pass their
    file path under `file_path`, so the fence never sees it.
    `pkg/security/permissions.go:684` reads `details["path"]`; the path glob
    check at `:663` uses the same key. The executor passes raw args
    (`internal/agent/executor.go:2171-2181`). 17 tools in `internal/code/tools`
    use `SchemaPropFilePath = "file_path"` (`schema_constants.go:12`), and
    `internal/tools/builtin/lsp_navigate.go:39` uses the same key.
    Read proof: `internal/code/tools/lsp_hover.go:59,77,93` and
    `lsp_navigate.go:142,171,197` pass the raw path to `OpenFile`, which runs
    `os.ReadFile(absPath)` (`internal/code/lsp/document.go:55`). Those read
    tools have no fence of their own. The write tools `ast_edit`, `ast_resolve`,
    `lsp_rename`, `resolve_ast_edit` carry a tool-level `fenceChecker`, and the
    daemon wires no `SetFenceChecker` for them (`git grep 'SetFenceChecker('`
    in internal/daemon: 0 hits).
    Blast: with `[code_intel.lsp].enabled = true` (default false,
    `config/meept.json5:1216`), a model can read any file on the host that
    the daemon user can read, outside the session fence and outside the
    blocked-paths list. Graded HIGH as a fence bypass; reach is opt-in today.
    Source: widened by the parent from auditor finding A6-7.

## MEDIUM (35)

Count reconciliation: the 7 auditors filed 11 HIGH + 34 MEDIUM + 36 LOW = 81.
The parent re-graded A1-6 and A6-2 from HIGH to MEDIUM and A6-7 from MEDIUM
to HIGH. Final: 9 HIGH + 35 MEDIUM + 36 LOW = 81. All 81 are named below.
Items M1-M30 carry one line each. The last 5 carry auditor evidence only.

Default posture:

M1. (A6-3, parent-verified) TUI project prompt sends the placeholder as a
    path. `internal/tui/project_prompt_wiring.go:38` returns
    `(no project detected)`. The prompt pre-selects "Yes"
    (`internal/tui/modals/project_prompt.go:131`); Enter sends
    `Accepted: true` (`:97`). `internal/tui/app.go:1607-1609` calls
    `SetProjectPath` with that string. `validateShortName` accepts it
    (`internal/project/manager.go:57-65`). The daemon runs `MkdirAll` and
    `git init` at `<base_dir>/(no project detected)` (`:602-618`) and sets it
    active (`internal/rpc/projects.go:248`). The Flutter GUI has the guard
    the TUI lacks: `if (cwd.isNotEmpty)`
    (`ui/flutter_ui/lib/providers/session_project_provider.dart:52-55`).
    Parity break per `docs/agents/ui-and-feature-rules.md`.

M2. (A6-2, RE-GRADED by the parent from HIGH; auditor missed the central
    fence) Office tools use the symlink-unsafe `resolvePath` for their own
    fence. `office_docx.go:137`, `office_pptx.go:142`, `office_workbook.go:130`.
    `file_read`/`file_write` use `resolvePathSecure`
    (`filesystem.go:1142-1146` documents why). The office tools DO reach the
    central fence (they use the key `path` and map to `file_write`,
    `executor.go:465-467`), which resolves symlinks
    (`internal/security/fence.go:254`). So the escape is closed on the
    default posture. It is open when `fence_enabled` is false or the session
    has `--nofence`. Defense-in-depth gap, not an open bypass.

M3. (A1-6, RE-GRADED by the parent from HIGH) OTLP `chat.turn` span never
    adopts the minted `TurnSpanID`. `internal/daemon/otlp_traces_wiring.go:203-216`
    omits the field; `RecordTurnSpan` reads it
    (`internal/metrics/otlp_trace_producer.go:179`). Hop spans parent on an
    id that is never exported. OPT-IN (default off,
    `internal/config/otlp_traces.go:33`).

M4. (A2-2) `Executor.security` written under `e.mu`, read bare.
    `internal/agent/executor.go:1546, 2226`.

M5. (A2-3) `AgentLoop.turnCounter++` with no lock. `internal/agent/loop.go:10304`.

M6. (A2-4) `go l.triggerTitleRefresh` holds an LLM call and is not in
    `l.wg`. `internal/agent/loop.go:10310`.

M7. (A2-5) Result-cache hit returns before the exposure gate, the group gate,
    and `checkPermission`. `internal/agent/executor.go:1449`. Opt-in cache.

M8. (A1-9) `groups.assign`/`groups.remove` race on the live config; no lock
    in `internal/rpc/groups.go`.

M9. (A1-10) `groups.assign` returns success, but enforcement is a boot-time
    snapshot. `internal/daemon/components.go:1155`; the Resolver has no
    reload method.

M10. (A1-11) `register()` in `internal/daemon/activity_rpc.go:35-45` probes
    with a real dispatch (`CallMethod`), so each boot logs 5 false Warns.

M11. (A1-12) Three panic recovers log no `debug.Stack()`.
    `intake_wiring.go:196, 311`, `spill_wiring.go:71`. Violates
    `internal/daemon/AGENTS.md` ("capture `debug.Stack()` in recovers").

M12. (A3-3 / A2-6) `GroupFenceStage.pathInRoot` is a lexical prefix match
    with no symlink resolution. `internal/agent/fence_stage.go:135-142`.
    Subsumed by H6 while the stage is unreachable.

M13. (A3-4) Groups C5 backstop compares lexically.
    `pkg/security/groups_backstop.go:47-81`.

M14. (A3-5) Working-tier eviction queue is never flushed; `Store.Flush` and
    `EpisodicSinkAdapter` have zero production callers.
    `internal/memory/working/store.go:322-332`.

M15. (A3-6) `scope_filter.visible` treats an unknown row scope as global.
    `internal/memory/scope_filter.go:134-138`. Contradicts the file's own
    fail-closed rule under multiuser.

M16. (A3-2) Demotion's "lossless" episodic copy is at once in the eviction pool.

M17. (A6-4) Goal round driver sets `inflight` before `scheduleDeferredTurn`,
    which returns early with no scheduler. Employee is stuck "in flight".
    `internal/employee/goal_round_driver.go:365-366, 599-600`.

M18. (A6-5) Any `UpdateGoal` error during reserve disarms the goal; nothing
    re-arms it. `goal_round_driver.go:578-583`.

M19. (A6-6) `exit_plan_mode` is one shared tool instance whose `sessionID`
    and `askUser` are mutated per turn by every concurrent loop.

M20. (A6-9) `office_pptx` edit-in-place uses a plain `os.WriteFile`; a failed
    write leaves a truncated zip over the original. `office_pptx.go:368, 453`.

M21. (A5-1) `contact` tool blocks the agent turn for the whole call, and a
    call past the 10-minute cap is misreported `no_answer`.

M22. (A5-2) Goal approval-gate park rows are never released when the goal is
    paused, blocked, or retired. `gate_park_wiring.go:210-256`.

M23. (A5-3) `RunAgentWithModelRef` defer-clears a SHARED per-agent loop
    override. `internal/agent/registry.go:1253-1263`.

M24. (A5-4) `HasCommitment` dedupe fails open past 1000 commitment rows
    (`ORDER BY id ASC LIMIT 1000`). `brief_commitments.go:220-245`.

M25. (A4-1) `pipeline.close()` runs on the child's single C1 dispatch loop;
    each end frame blocks all other calls up to 5 s. `voicechild/child.go:495-503`,
    `pipeline.go:2919-2941`. Violates the "never block the C1 dispatch loop" rule.

M26. (A4-2) A synchronous LLM call-brief (up to 5 s) and DB reads run inside
    the Telnyx hangup webhook handler. `voice_employee.go:1132-1180`.

M27. (A4-3 / A7-4) `TextServer.Send`/`SendRaw` take no write lock; concurrent
    writers can interleave a partial frame. `internal/voice/socket.go:322-361`.

M28. (A4-4) Signal-leg resampler drops the un-consumed tail of every chunk
    (2 of 320 samples on the 48k path). `internal/signalleg/audio.go:200-233`.

M29. (A7-2) Daily-dial cap unit is consumed before the POST and never
    refunded on failure. `internal/voice/dial.go:614-622`.

M30. (A7-5) Supervisor-on reply path truncates at 280-rune sentences and 16
    sentences. `internal/voice/supervisor.go:413-441`.

Remaining MEDIUMs with auditor evidence, not re-read by the parent: A1-7 (OTLP
sampling keeps rate/4), A1-8 (trace circuit never re-opens), A7-1 (`Dialer.fin`
map unbounded), A7-3 (barge gate clock mix), A7-6 (escape-rate metric
overwritten).

## LOW (36)

Auditor-graded, not re-read. One line each in the per-auditor reports:
A1-13..18 (6), A2-7..11 (5), A3-7..12 (6), A4-5..7 (3), A5-5..7 (3), A6-8,
A6-10..14 (5), A7-7..12 (6). Two that a fixer should see first:
- A3-11: JSON5 duration preprocessing rewrites a quoted duration-looking
  string to an integer unless the key is registered. Same class the
  `bughunt-audit-and-fix` skill records for `refresh_interval`.
- A7-12: `supervisor.parseAllowed` is fail-OPEN on any reply with an "ALLOW"
  prefix.

## Already fixed by a later wave (37 candidates dropped by auditors 4-7)

Voice 15 + 12, non-voice 4, employee/tools/tui 6. Each is listed with its
file:line in the per-auditor report. Not re-checked by the parent.

## Not audited (ledger - what this wave did NOT read)

This list is the follow-up scope. It is large; the two gaps are only
partially covered.

Gap 1:
- `internal/daemon/components.go` (12.4k lines) and `daemon.go`: range hunks
  + targeted traces only. Not read end to end.
- `internal/voice/talkloop.go` lines 1-983; `voicechild/pipeline.go` lines
  1-445, 1334-2058, 2425-2867; `voicechild/providers` bodies (whisper, rumble,
  openai, piper, pcmstream, vad).
- `internal/groups` diagnose/membership_writer; `internal/config` 12 files;
  `internal/llm` 15 files; `internal/security` 9 files; `internal/memory` 11
  files; `internal/skills/lifecycle`; `internal/cluster` 6 files;
  `cmd/meept/doctor*.go`.
- `internal/agent/loop.go` non-concurrency diff lines (~1000); 40 small agent
  files grepped only.

Gap 2 (auditor 5 read 14 of ~380 in-scope files at line level):
- `internal/scheduler` (timing.go +839, receipts.go +370, jobs.go, claims.go).
- `internal/session` (projection, child_descriptor, store_sqlite,
  format_migration, schema_version_sqlite).
- `internal/plan` (manager.go +686, store_check, transitions).
- `internal/signal` (rpc.go +757, supervisor.go +430).
- `internal/telnyx` (sms_attempt, messaging); `internal/comm/http/server.go`
  beyond the auth-exemption block; `internal/hermes` SSE pump (A6-14 only).
- `fix(agent)` commits 0b5cc1cd, 663b633a, 993f69de, cc06827f, e7dc20be,
  50b4309e, 80659bb5: read by message and file list only.
- `office_pptx.go:460-1061` and `office_docx.go:175-452` (OOXML body).
- Flutter / Dart: not audited at all (Go-only wave).

Both gaps: no `-race` run; e2e suites not run; the 27 sibling fix commits
since 2026-11-24 17:36 not audited.

## Shared-tree ledger

- Sibling sessions are live. HEAD moved from e43a7cf3 to c5d54c9e during this
  wave (27 commits, all fix commits for the 2026-11-24 report). No finding
  site changed between verification point 1a4a314b and c5d54c9e
  (`git diff --stat` over every cited path: empty).
- Working tree at start: 7 staged, 47 modified, 15 untracked. None are mine.
  My only writes are the 8 files under `.hermes/audits/2026-11-25-bughunt-gaps/`
  and this report.
- The sibling's uncommitted `internal/llm` revert (-217 lines) is still in the
  tree. If committed alone it turns `go test ./internal/llm` red for everyone;
  `client_transient_retry_test.go` must go in the same commit.
- Auditor 3 created and removed probe test files (`zz_a3_*`); none remain.
- Auditor 1 spawned two leaf auditors of its own; that is one more layer than
  the brief described.
- A `go test -overlay` run by auditor 2 left nothing in the repo.
- Baseline gate log: `/Users/caimlas/.git/worktrees/bughunt-logs/`.
  Delete at your discretion.

## Disposition

All findings OPEN. Nothing fixed this wave (read-only request).

## Suggested fix partition (by file ownership, for a "fix all" wave)

1. internal/activity: H1, H2 (ring.go, aggregator.go, plans.go). Small.
2. internal/comm/http: H3 (server.go). Small: wrap the three branches in
   `rateLimitMiddleware`, as the kanban branch does.
3. internal/daemon gate-capture: H4 (components.go: change the parameter to
   `**gateCaptureStore`, or return the store). Add a non-vacuity pin.
4. internal/daemon + internal/effects: H5 (toolgate.go key; add session and
   agent to the key, or compare claimant by session only).
5. internal/agent fence: H6 (fence_stage.go, handler.go, dispatcher.go: build
   the union of the session fence and the group fence). Pairs with M12.
6. pkg/security + internal/code/tools: H9 (central: map `file_path` into the
   fence check; tool-level: fence the 13 unfenced read tools).
7. internal/tools/builtin + internal/employee: H8 (goal_tools.go: make the
   flag per-turn via context, not a package global).
8. internal/memory/tierpolicy: H7 (execute.go: put the tier selection in the
   payload the worker accepts; or hold evict until the compress job lands).
9. internal/tui: M1 (project_prompt_wiring.go: skip the prompt when no CWD,
   as the GUI does).

## Owner decisions (self-contained)

D1. Group `fs_roots` (H6). WHAT IS BROKEN: the group fence is never armed
    (`fence_stage.go:68-78`). THE DECISION: is `fs_roots` an ADDITIVE root
    (the subject may write in any listed root, in addition to the session
    dir) or the ONLY allowed roots? OPTIONS: (a) additive union - a group
    member keeps session-dir writes plus the roots; matches the doc comment
    at `fence_stage.go:84-88`. (b) exclusive - safer, but breaks any group
    member whose work lives in the session dir. RECOMMENDATION: (a), because
    it is the documented contract.

D2. Effects key scope (H5). WHAT IS BROKEN: the key omits the session
    (`toolgate.go:386`). THE DECISION: is a repeated `(tool, args)` across two
    sessions one effect or two? OPTIONS: (a) add session to the key - each
    session claims its own record; cross-session duplicate suppression ends.
    (b) keep one key and drop the claimant check for gate records. (c) keep
    one key, add agent but not session. RECOMMENDATION: (a); a read-shaped
    tool call in a new session is a new effect.

D3. Tier-policy compress payload (H7). WHAT IS BROKEN: compress jobs always
    fail (`execute.go:139`, `distill_async.go:239`). THE DECISION: should
    tier compression distill the selected IDs, or the session's L0 day file?
    OPTIONS: (a) carry the target IDs in the payload and teach the worker to
    distill by ID. (b) point `L0Path` at the session's day file and accept
    that the selection is a hint. RECOMMENDATION: (a); (b) does not compress
    what the engine counted.

D4. Daily-dial cap (M29). WHAT IS BROKEN: a failed dial spends a unit
    (`dial.go:614-622`). THE DECISION: does a refused or failed dial count
    against the cap? RECOMMENDATION: refund on failure; the cap is for
    completed calls.
