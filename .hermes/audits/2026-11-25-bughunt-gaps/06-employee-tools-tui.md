# Auditor 6: employee / tools / hermes / acp / tui (range 2026-11-06..11-11)

Scope: non-test Go under internal/employee, internal/tools/builtin (mcp excluded), internal/bot, internal/hermes, internal/acp, internal/skills, internal/tui changed in `7b510ed4..ea3d7082` (71 files), read at HEAD `7b7ab262` (two employee commits landed during the audit: `e9461a8d` M15 retire drain, `7b7ab262` M17 shutdown ctx — both re-read; the round driver, tools/builtin, hermes/acp, skills, bot and TUI files are byte-identical to e43a7cf3). Files read: 62 of 71 (the 9 not reached are listed under "Not audited"; 4 of the 71 are deletions).

Rules applied (quoted in findings where violated): root AGENTS.md ("no I/O under mutex; no ignored errors"; "Guard typed-nil interfaces; nil-guard every Set*"), docs/agents/coding-practices.md ("Type assertions ... two-value form", "Every Set* method MUST include a nil guard", "Never hold a mutex across I/O"), docs/agents/ui-and-feature-rules.md ("TUI and Flutter GUI stay at parity"), docs/workflows/tui.md (tables sized on both axes — no violation found in range), internal/agent/AGENTS.md (unbound session is a legitimate state), filesystem.go:1142 comment ("SECURITY FIX (2026-07-14): ... fence bypass via symlink attacks ... Callers that need full path resolution including symlinks should use resolvePathSecure instead").

Severity summary: HIGH 3, MEDIUM 5, LOW 6.

## Findings

### A6-1 — HIGH — Process-global `goalAutonomousRound` flag is shared by every concurrent turn; it grants human-only goal authority to any round that overlaps a driver round

- `internal/tools/builtin/goal_tools.go:169-187, 486`; `internal/employee/goal_round_driver.go:374-376`
- Added in range (fb8f6d73); unchanged at HEAD (`git log -3 -- goal_tools.go`: last touch 7ea05edd lint-only).

```go
// goal_tools.go
var goalAutonomousRound atomic.Bool            // :169  "process-global; the drive runs one continuation at a time"
func requireDirectHuman() error {              // :184
	if goalAutonomousRoundActive() { return ErrGoalToolDirectHumanRequired }
	return nil
}
// goal_round_driver.go (deferred closure, ONE per employee, runs concurrently with other employees' rounds)
d.MarkAutonomousRound(true)                    // :374
defer d.MarkAutonomousRound(false)             // :376
... d.mgr.Trigger(context.Background(), employeeID, payload)  // :385  minutes of LLM work
```

- Trigger: the driver is per-employee and the per-employee invoke mutex (manager.go `acquireInvokeMutex`) serializes only within one employee; the scheduler runs each deferred job on its own goroutine. Two employees' rounds therefore overlap in time and both flip the same `atomic.Bool`.
- Effect (two failure directions):
  1. **Authority leak:** while ANY employee's driver round is in flight, the flag is true for the WHOLE process. Any other non-human round that holds goal tools — a cron round (`cron_trigger.go` `source:"cron"`, never calls `MarkAutonomousRound`), a webhook/bus-triggered round, or a second employee's round — passes `requireDirectHuman()` and may run `goal_create`, `goal_update edit|pause|resume`. `resume` is the one C-P3 edge (`blocked -> active`) that `Manager.UpdateGoal` (manager.go:2528 at e43a7cf3; the `UpdateWithTransition` doc at goal.go:1047) says is human-gated "at the edge" by `requireDirectHuman`. The flag is also the self-block threshold gate (:486): a non-autonomous round can `blocked` below the threshold.
  2. **Spurious refusal:** employee B's round finishes first and runs `defer MarkAutonomousRound(false)`; employee A's still-running round now sees `false` and its legitimate self-`blocked` (past threshold) is refused with `ErrGoalToolBlockThreshold`, so the goal keeps burning rounds to the cap.
- Also note the polarity: the comment says "an unset flag (false) means direct human", so every autonomous path that does NOT set the flag (cron/webhook) is treated as a human turn by default.
- Caller evidence: `goal_tools.go:185` (`requireDirectHuman` in edit/pause/resume/create), `:486`; driver `goal_round_driver.go:374-376`; `dsh_wiring.go:98` binds `builtin.SetGoalAutonomousRound`; `git grep MarkAutonomousRound` — only the driver sets it. Cron path `cron_trigger.go:163` sets `source:"cron"` and nothing else.
- Later commits: none touched the flag or its setter.
- Confidence: confirmed by reading the full path (flag, both callers, scheduler concurrency, cron path).

### A6-2 — HIGH — `office_docx` / `office_pptx` / `office_workbook` fence resolves paths with the symlink-unsafe `resolvePath`, so a symlink inside the session dir escapes the fence (read AND write)

- `internal/tools/builtin/office_docx.go:128-146`, `office_pptx.go:142`, `office_workbook.go:130`
- Added in range; unchanged at HEAD.

```go
// office_docx.go:128 (pptx :131-150 and workbook :119-146 are byte-equivalent)
func (t *OfficeDocxTool) resolveFenced(ctx context.Context, path string) (string, error) {
	sessionDir := tools.WorkingDirFromContext(ctx)
	...
	absSession, err := filepath.Abs(sessionDir)
	resolved, err := resolvePath(ctx, path)          // :137 — NOT resolvePathSecure
	resolved = filepath.Clean(resolved)
	if resolved != absSession && !strings.HasPrefix(resolved, absSession+string(filepath.Separator)) {
		return "", fmt.Errorf("office_docx: path %q resolves to %s, which is outside ...")
	}
	return resolved, nil
}
```

- Trigger: the model (which has `file_write` for these tools per executor.go:465-467, and typically `shell`) plants `ln -s /other/worktree link` inside the session dir, then calls `office_docx action:read path: "link/plan.docx"`, or `action:create path: "link/x.docx"`. `filepath.Clean` keeps the symlink component; the textual prefix check passes; `doRead`/`SaveTo` follow the link.
- Effect: reads of `.docx/.pptx/.xlsx` files outside the session jail (the exact issue-#138 class the `read_file`/`file_write` session jail closed), and writes/overwrites of documents outside the jail. These three tools are also not covered by `guardSecretReadPath` / `checker.CheckPath` (no `FenceChecker` field at all).
- Caller evidence: registered unconditionally in production at `internal/daemon/components.go:8982-8984`; grant map `executor.go:465-467` (`file_write`). The repo documents the hazard at `filesystem.go:1142-1146`: "SECURITY FIX (2026-07-14): Previously, this function [resolvePath] did not resolve symlinks, allowing fence bypass via symlink attacks (e.g., a symlink inside the workspace pointing to /etc or ~/.ssh). Callers that need full path resolution including symlinks should use resolvePathSecure instead." The pre-existing `spreadsheet_write.go:109-114` has the same shape (the office tools copied it verbatim) — co-located instance outside this range.
- Later commits: none to the three office files.
- Confidence: confirmed by reading resolvePath/resolvePathSecure and all three tools' Execute paths.

### A6-3 — HIGH — TUI project prompt: "Yes" on a session with no detection context binds `project.set {path:"(no project detected)"}` and the daemon creates + activates a garbage git repo

- `internal/tui/project_prompt_wiring.go:38, 53-56`; `internal/tui/app.go:1607-1611`; `internal/tui/modals/project_prompt.go:50, 151`
- Added in range (TUI/GUI parity leaf); unchanged at HEAD.

```go
// project_prompt_wiring.go
func projectPromptDefaultPath(sess *types.Session) string {
	if sess != nil && sess.DetectionContext != nil && sess.DetectionContext.CWD != "" {
		return sess.DetectionContext.CWD
	}
	return "(no project detected)"                      // :38
}
...
return modals.OpenProjectPromptMsg{SessionID: sessionID, DefaultPath: projectPromptDefaultPath(sess)}  // :53-56

// app.go
case modals.ProjectConfirmedMsg:
	if msg.Accepted {
		path := a.projectPrompt.SessionDefaultPath()    // :1607  == "(no project detected)"
		return a, func() tea.Msg { err := a.rpc.SetProjectPath(sessionID, path); ... }  // :1609
```

- Trigger: switch to any session whose `DetectionContext.CWD` is empty (internal/agent/AGENTS.md documents this as a legitimate state: "else leaves the session UNBOUND"). The modal opens with `selected = 0` (Yes pre-selected, `project_prompt.go:50`) and renders `Use current directory for execution? (no project detected)`; pressing `enter` or `y` sends the placeholder as the path.
- Daemon side: `internal/rpc/projects.go` `handleSet` → `pm.CreateOrResolve(ctx, "(no project detected)")` → `validateShortName` accepts it (no `/`, `\`, `..`) → `os.MkdirAll(baseDir/"(no project detected)")`, `git init`, sidecar, `store.CreateProject`; then `p.Status = "active"` ("so new sessions have a default project"), `SetProject(session, ...)`, `TouchRecent`.
- Effect: a bogus git repository is created on the daemon host, becomes the default active project for NEW sessions, and this session's working directory (all file tools) is that empty repo. The Flutter GUI this was ported from guards it: `ui/flutter_ui/lib/providers/session_project_provider.dart:52-55` — `case ProjectPromptResult.accept: if (cwd.isNotEmpty) { onProjectBound(cwd); }` — accept with empty CWD binds nothing. Parity rule violated (ui-and-feature-rules.md: "TUI and Flutter GUI features must be kept at parity").
- Secondary: there is no "dismissed" memory, so `n`/`esc` re-opens the prompt on every switch back to the same unbound session (`app.go:1431` emits on every `SwitchSession`).
- Caller evidence: `app.go:1431` (`maybePromptProject` on session switch), `:1586` (open), `:1598-1611` (accept path). Daemon: `internal/rpc/projects.go:74` (`project.set` registration), `handleSet` → `CreateOrResolve` (`internal/project/manager.go`).
- Later commits: none to the three TUI files.
- Confidence: confirmed by reading the full path TUI → RPC → CreateOrResolve.

### A6-4 — MEDIUM — Goal round driver: `inflight` is set before `scheduleDeferredTurn`, but when no scheduler is bound the turn is never scheduled and the employee is stuck "in flight" forever (and each manual advance reserves a round that never runs)

- `internal/employee/goal_round_driver.go:364-366, 599-600`
- Added in range; unchanged at HEAD (`git diff ea3d7082..HEAD` on this file: one comment/field cleanup).

```go
func (d *GoalRoundDriver) scheduleDeferredTurn(employeeID string) {
	if d.deferred == nil {
		return                       // :365-366 — reserve-only mode
	}
...
// OnRoundSettled
	d.markInflight(employeeID, true)    // :599
	d.scheduleDeferredTurn(employeeID)  // :600
```

- Trigger: `d.deferred == nil`. The daemon only binds it when `c.Scheduler != nil` (`dsh_wiring.go:101-105`), and `c.Scheduler` is nil when `[scheduler].enabled=false` or `scheduler.NewScheduler` fails (`components.go:5355-5360`). In that configuration the driver still starts (`autoAdvance` defaults on).
- Effect: first poll tick → `OnRoundSettled` persists `RoundsStarted+1` and marks inflight; no turn is scheduled; every later tick returns `GoalRoundIdle` at `isInflight` (`:303`). The reserved round is charged against the 256 cap and never runs; nothing logs. The documented "reserve-only mode ... for the manual RPC/CLI advance to pick up" is not what happens — the manual `agents.goals.round.advance` (`daemon.go:1578-1600`) bypasses `isInflight` entirely, so each call reserves another never-run round until `round-limit` auto-blocks.
- Caller evidence: `dsh_wiring.go:101` (conditional binding), `:152` (`Start` regardless), `daemon.go:1592` (manual RPC calls `driver.OnRoundSettled` directly).
- Later commits: none.
- Confidence: confirmed by reading; reachability requires scheduler disabled/failed (a documented opt-out).

### A6-5 — MEDIUM — Goal round driver: any `UpdateGoal` error — including a transient CAS conflict or SQLITE_BUSY — permanently disarms the goal; nothing can re-arm it

- `internal/employee/goal_round_driver.go:578-583, 572` (block path), `goal.go:1107-1153`
- Added in range; unchanged at HEAD.

```go
goal.StartRound()
d.Arm(goal)                                   // :580
if err := d.mgr.UpdateGoal(ctx, goal); err != nil {
	d.disarm(goal.ID)                          // :582
	return GoalRoundIdle, fmt.Errorf("reserve goal round: %w", err)
}
// goal.go UpdateWithTransition
callerRevision := g.Revision
...
return fmt.Errorf("%w: goal %s caller snapshot at revision %d, persisted revision changed under this write", ErrGoalRevisionConflict, ...)  // goal.go:1153
```

- Trigger: any writer lands between `drivableGoal`'s `ListGoals` and `UpdateGoal` inside `OnRoundSettled` — the periodic assess (`scheduler_jobs.go:326` `goalStore.Update`), `AssessGoal` (manager.go:2850 at HEAD), the `goal_update` tool (`goal_tools.go:499`), intake re-dispatch (`intake/linkage.go:209`), or the manual RPC racing the poll loop. Also any store error (busy, closed).
- Effect: `disarm` sets `d.armed[goalID] = &false`; `isArmed` then returns false for that goal and `drivableGoal` skips it forever. The only non-test caller of `Arm` is `OnRoundSettled` itself (`git grep '\.Arm(' HEAD` → `goal_round_driver.go:580` and an unrelated `parked_turn.go:289`), which is unreachable once disarmed. Autonomous continuation silently stops for that goal until process restart; the warn log names the error but not the permanence. The design comment says "until something re-arms it" — nothing does.
- Caller evidence: as above; `goal_round_driver.go:572` is the identical pattern for a failed `BlockGoal` (there the next tick retries because `disarm` is only reached on success — so only :582 has the stuck behaviour).
- Later commits: none to the driver.
- Confidence: confirmed by reading; window is narrow for the CAS case, but every transient store error hits it.

### A6-6 — MEDIUM — `exit_plan_mode` is a single shared tool instance whose `sessionID`/`askUser` are mutated per turn by every concurrent loop (data race + cross-session plan-mode state)

- `internal/tools/builtin/plan_mode.go:244-246, 286`; `internal/daemon/dsh_wiring.go:500-504`; `internal/agent/speak.go:305-310`; `internal/agent/loop.go:4015`
- Tool added in range; unchanged at HEAD.

```go
func (t *ExitPlanModeTool) SetSessionID(sessionID string) {  // :244
	t.sessionID = sessionID                                   // :245  unsynchronized field write
}
func (t *ExitPlanModeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sessionID := t.sessionID                                  // :286  unsynchronized read
	if sessionID == "" || t.ctrl == nil || !t.ctrl.PlanMode(sessionID) {
```

- Wiring: `dsh_wiring.go:500-504` constructs ONE `exitTool` and `c.ToolRegistry.Register(exitTool)` into the global registry; `speak.go:305-310` `wireResponseSetters` runs at EVERY turn start over `l.registry.List()` and calls `setter.SetSessionID(l.sessionID)` / `SetResponseFunc(responder)` on that same pointer. `FilterToolsForSkill` returns a view over the same tool pointers (no cloning).
- Trigger: two sessions with overlapping turns (the normal multi-session case), at least one in plan mode.
- Effect: (a) a Go data race on `t.sessionID`/`t.askUser` (unsynchronized write from loop A's turn start vs read in loop B's tool execution); (b) session A's `exit_plan_mode` is evaluated against whichever session most recently started a turn — if that session is not in plan mode the tool errors "only available while plan mode is active", if it IS, A's plan-mode check passes for B and (once a real responder is wired) `SetPlanMode(sessionID,false)` would clear B's plan mode.
- Note: at HEAD the injected responder always returns an error (`speak.go:305`), so the Approve branch is unreachable today; the PlanMode read and the race are reachable now.
- Caller evidence: `speak.go:310` (per-turn mutation), `loop.go:4015`, `dsh_wiring.go:504` (single registration).
- Later commits: none.
- Confidence: confirmed by reading; race reachable with concurrent sessions.

### A6-7 — MEDIUM — `lsp_navigate` reads any file path from tool input with no fence, no secret-read guard, no symlink resolution

- `internal/tools/builtin/lsp_navigate.go:159-197`
- Added in range; unchanged at HEAD.

```go
workspace, _ := args[navPropWorkspace].(string)       // :159  model-controlled
if workspace == "" { workspace = tools.WorkingDirFromContext(ctx) }
...
absPath = filepath.Join(workspace, absPath)            // :169  "../../other" or absolute path accepted
absPath, err := filepath.Abs(absPath)                  // :171  no EvalSymlinks
...
if _, err := srv.DocMgr.OpenFile(ctx, absPath); err != nil {   // :197  os.ReadFile(absPath) + didOpen to the server
```

- Trigger: `lsp_navigate operation:hover file_path:"/abs/or/../../other-worktree/x.go" line:1 char:1`, or `workspace: "/"`.
- Effect: the tool reads the named file from disk (`document.go` `OpenFile` → `os.ReadFile`), retains its full content in the shared `DocumentManager` (never closed — `lsp_navigate` has no CloseFile call, so each navigated file is a permanent entry), pushes it to the LSP server, and returns hover content and cross-worktree symbol locations. None of the four guards `read_file` applies (`resolvePathSecure`, `guardSecretReadPath`, `checker.CheckPath`, session-jail `checkPathJailed`) are present; `DetectLanguageID` by extension means any `.go/.py/.ts/...` file on the daemon host outside the session jail is reachable with a `file_read` grant.
- Caller evidence: registered at `components.go:10239-10244`; grant map `executor.go:431` (`lsp_navigate -> ToolFileRead`). The sibling `internal/code/tools/lsp_document_symbols.go:57,73` has the same unfenced shape (pre-existing, outside this range).
- Later commits: none.
- Confidence: confirmed by reading; exposure limited to hover/locations + the doc-cache growth.

### A6-9 — MEDIUM — `office_pptx` edit-in-place writes the deck with a plain `os.WriteFile` (non-atomic); a failed write leaves a truncated zip over the original

- `internal/tools/builtin/office_pptx.go:366-370, 452-455, pptxOutputPath`
- Added in range; unchanged at HEAD.

```go
if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {      // :368  out == path when in_place:true
	return nil, fmt.Errorf("office_pptx: write %s: %w", out, err)
}
...
if err := os.WriteFile(out, data, 0o644); err != nil {             // :453  writeDeck
```

- Trigger: `action:edit, in_place:true` on an existing deck; ENOSPC/EINTR/kill mid-write.
- Effect: `os.WriteFile` truncates the destination first, so the user's only copy of the binary `.pptx` is destroyed with no temp-file + rename and no backup. `office_workbook.go:249` (`f.Save()`) and `office_docx.go:317/345` (`rd.SaveTo(out)` where `out` may equal `path`) have the same in-place shape. The repo's `file_write` is also non-atomic, so this follows house style — graded MEDIUM because the edit path overwrites an existing binary archive the tool cannot regenerate.
- Caller evidence: `components.go:8983` (registration); `pptxOutputPath` returns `path` for `in_place` and for `create`.
- Later commits: none.
- Confidence: confirmed by reading.

### A6-8 — LOW — Deferred goal round discards every cancellation source it is handed (`context.Background()` into `Trigger`); shutdown is now covered by M17's manager-level fan-in, but the driver's own `loopCtx` and the scheduler's `runNowCtx` are still dropped

- `internal/employee/goal_round_driver.go:377, 385`; `internal/daemon/dsh_wiring.go:199-211`; `employee_scheduler_adapter.go:60`
- Added in range; unchanged at HEAD.

```go
payload := d.goalRoundPayload(context.Background(), employeeID)        // :377
...
if _, err := d.mgr.Trigger(context.Background(), employeeID, payload); err != nil {   // :385
// adapter
func (j *simpleIntervalJob) Execute(_ context.Context) error { j.fn(); return nil }  // ctx dropped; fn takes none
```

- Trigger: every autonomous round. `wrapJob` (scheduler.go:706-722) hands the job a ctx derived from `runNowCtx`, and the poll loop owns `loopCtx`, but `DeferredScheduler.ScheduleOneShot(name, fn func())` has no ctx parameter and the driver substitutes `context.Background()` in both the payload build and the `Trigger` call.
- Effect at HEAD: `triggerViaGoalLoop` (manager.go:2026-2036, M17 `7b7ab262`, landed during this audit) now derives `roundCtx` from the caller ctx AND the manager's shutdown context, so an in-flight driver round IS cancelled once `StopAll` passes its 5 s drain bound. What remains: `goalRoundPayload` (`ListGoals` on a dead store after `components.Stop`) and the pre-`Trigger` half of the closure run uncancellable, and `Trigger(context.Background())` itself bypasses the driver's own `loopCtx` so `StopGoalRoundDriverLoop` cannot stop a round that has not yet entered `triggerViaGoalLoop`. "ctx not passed to a child call" class; impact now bounded by M17. (Range-era impact before M17 was MEDIUM: a minutes-long round ran against stores/runtimes the daemon had already closed.)
- Caller evidence: as quoted.
- Later commits: `7b7ab262` (M17) added the manager-level fan-in; the driver file itself is unchanged.
- Confidence: confirmed by reading both the range code and the M17 diff.

### A6-10 — LOW — Approval-gate resume decodes the OUTER `TurnReentry` map into `goalGateResumePayload`, so the recorded `Result` is always nil on resume

- `internal/employee/goal_loop.go:1548-1552, 1601-1611, 1625, 1674-1681`
- Added in range; unchanged at HEAD.

```go
type goalGateResumePayload struct {
	PlanID string                  `json:"plan_id,omitempty"`
	Prompt string                  `json:"prompt,omitempty"`
	Result *bot.BotExecutionResult `json:"result,omitempty"`   // :1551
}
// park time — TurnReentry is the OUTER map; the payload is nested under "reentry"
TurnReentry: map[string]any{"plan_id": plan.ID, "prompt": plan.Prompt, "reentry": json.RawMessage(reentry), "employee_id": l.employeeID},  // :1674-1681
// resume time — decodes the OUTER map
raw, err := json.Marshal(reentry)          // :1602  reentry == data.TurnReentry
json.Unmarshal(raw, &p)                    // → p.Result == nil (lives under "reentry", never read)
if _, reflectErr := l.Reflect(ctx, plan, p.Result); reflectErr != nil {   // :1625
```

- Effect: `Reflect(ctx, plan, nil)` takes the failure path (`result == nil`): `consecutiveSuccesses` is not incremented, `lastResult` is not recorded, `notifySpeakRound` is skipped, and the LLM verdict computed before the park was discarded (`return GoalUnknown, nil` precedes the store write). Health resolves to `GoalHealthy` via `HealthDecayFunc(0, n)` so the approval still lands, but the resume does not "replay the completion verdict" the comment promises; the nested `reentry` key is dead data (and would arrive as a `string`, not `json.RawMessage`, after a persisted round-trip).
- Caller evidence: `goal_gate_wiring.go:114/120` → `loop.ResumeGoalGate(resumeCtx, data.TurnReentry)`.
- Later commits: none to these lines.
- Confidence: confirmed by reading.

### A6-11 — LOW — `exit_plan_mode` `classifyDecision` approves on substring `"approve"` ("do not approve", "disapprove" → Approve); latent while the responder fails closed

- `internal/tools/builtin/plan_mode.go:358-364`
- Added in range; unchanged at HEAD.

```go
func classifyDecision(answer string) decision {
	lower := strings.ToLower(strings.TrimSpace(answer))
	if strings.Contains(lower, "approve") {   // :360
		return approvePlan
	}
	return keepPlanning
}
```

- Effect: once a real interactive responder is wired, a human typing "I don't approve — keep planning" lands the plan on the durable gate (`CreatePlan → SubmitPlan → ApprovePlan` with approver `"user"`). Unreachable at HEAD because `speak.go:305`'s responder always returns an error before `classifyDecision` runs. Recorded as latent.
- Related rule nit in the same function: `_ = t.ctrl.SetPlanMode(sessionID, false)` at `:337` is an ignored error (AGENTS.md: "no ignored errors"; `SetPlanMode`'s own doc says errors are "returned (and logged by the caller)").
- Confidence: confirmed by reading.

### A6-12 — LOW — `contact` tool: the chain runner takes no context; voice/SMS/Signal I/O cannot be cancelled by the turn's ctx

- `internal/tools/builtin/contact.go:17, 109`
- Added in range; unchanged at HEAD.

```go
type ContactChainRunner interface { Run(person, message string, channels []string) string }  // :17
...
report := t.chain.Run(person, message, channels)   // :109  ctx unused
```

- Effect: a multi-channel fallback chain (phone call ring-out, then signal, then SMS) ignores `ctx` cancellation and any per-tool `TimeoutMs` (the tool declares none). Also returns `NewSuccessResult(report)` even when every channel failed — acceptable as a report envelope, noted only for context. `ctx not passed to a child call` class.
- Confidence: confirmed by reading; I/O duration is bounded by the chain's own ring timeouts (not verified).

### A6-13 — LOW — Driver `armed` / `deferredGoal` maps grow without bound (one entry per goal ever driven, never deleted)

- `internal/employee/goal_round_driver.go:421-422, 432-435, 455-460, 566-575`
- Added in range; unchanged at HEAD. `Arm`/`disarm` write `d.armed[goal.ID]`; `goalRoundPayload` writes `d.deferredGoal[employeeID]`; nothing deletes from either. Bounded by distinct goal ids per process lifetime (goals created via `goal_create`/intake over weeks). Not a leak of goroutines or file descriptors — LOW.

### A6-14 — LOW — `hermes` SSE pump flushes a PARTIAL frame after a scanner error (e.g. `bufio.ErrTooLong`), delivering truncated JSON as a complete event

- `internal/hermes/sse.go:238-258` (in-range file; the flush itself predates the range — the range only added a `//nolint:bodyclose`).
- `scanner.Buffer(..., sseMaxLine)`; on `scanner.Err() != nil` the code logs at debug and then calls `dispatch()` which sends whatever `dataLines` were accumulated before the error as `Event{Data: json.RawMessage(...)}`. A `data:` line longer than `sseMaxLine` yields a truncated RawMessage that consumers `json.Unmarshal` as if complete. Reachability depends on upstream emitting a >`sseMaxLine` line (large tool payloads). LOW; outside the range's substantive changes.

## Already fixed by a later wave

- **`agents.goals.approve_gate` was un-approvable over the Unix RPC socket and in legacy flat-key HTTP mode.** Range-end `handler.go` `handleGoalsApproveGate` used bare `CallerFromContext(ctx)` while its siblings (`handleGoalsApprove`/`Reject`) used `effectiveApprover(ctx)` (owner-trusted unix peer → `"user"`). With no injected caller the only resume surface for a human-approval goal gate returned "unauthenticated: API key required for gate approval"; combined with the C25 quiescence check (`goalGateParkedForEmployee`) the employee would never advance again. Fixed at HEAD by `34ecd7f3` (2026-11-13, "owner-trusted unix socket can approve employee plans"): handler.go:784 now calls `effectiveApprover(ctx)`.
- **Deadlock on the manager-routed gate drain.** `ResumeByKey` (internal/agent/gate_park.go:240-282) runs the resume callback SYNCHRONOUSLY (`func(){...}()` not `go`), while `Manager.ResumeGoalGateParked` holds the per-employee invoke mutex across `resumeFn`; the range-end daemon callback re-locked the same mutex. Fixed post-range by `0d62dadb` (2026-11-21) with `goalGateDrainLockedKey` in `internal/daemon/goal_gate_wiring.go:53-58,112-120`. (The stale comment at manager.go:2794-2801 — "the parker invokes the resume callback in its own goroutine" — and the pointless `wg.Add(1)` around a synchronous call in gate_park.go:421 remain; cosmetic.)
- **A round running against a retired/deleted bot reported success.** The range's `retireEmployee` refactor deleted the bot without marking the employee retired first; a trigger round already holding the invoke mutex executed against the deleted row. Fixed by M15 (`e9461a8d`, landed during this audit): `retireEmployee` calls `m.markRetired(id)` before `DeleteBot` and `triggerViaGoalLoop` fails closed on `isRetired` with `SkipReason: "employee retired mid-flight"` (manager.go:2019).
- **In-flight goal rounds could not be cancelled at daemon shutdown.** Range-era `triggerViaGoalLoop` ran `Decide` on the caller's ctx, and the driver hands it `context.Background()`. Fixed for the shutdown case by M17 (`7b7ab262`, landed during this audit): `Manager.ShutdownContext()` (manager.go:1130) is cancelled by `StopAll` after the 5 s drain bound, `triggerViaGoalLoop` fans it into `roundCtx` (manager.go:2026-2036), and `Trigger` refuses new rounds post-shutdown. Residual "ctx not passed" shape recorded as A6-8 (LOW).
- **Driver struct carried a dead duplicate `cancel` field** alongside `pollCancel` (goal_round_driver.go range-end :101). Removed by `3c1c9ae4`.
- **`StopAll` could wedge daemon shutdown on an in-flight cron round** (range-era `Cron.Stop()` unbounded wait) — fixed by `8a1970e2` (`cronStopDrainTimeout`); the invoke-mutex map entry was dropped without draining — fixed by `f2772f21` / M15 (`drainInvokeMutexBounded`, now bounded). Both in paths touched by the range's manager.go changes.

## Verified clean

- `internal/hermes/client.go:357-372` — non-2xx is rejected by default ("never 'any status'"), body closed via deferred `respClosing`, no reconnect loop exists in this package (no backoff needed).
- `internal/hermes/sse.go:165-171, 186-200` — non-2xx classified before any channel exists and body closed; pump owns and closes body and channel; ctx checked between frames and inside the channel send.
- `internal/acp/session.go:98-110` `childEnv` — allowlist mode still passes `PATH/HOME/TMPDIR/...` because `runtime.isAllowedName` (envpolicy.go:56-58) ORs `BaseEnvKeys` unconditionally; `cmd.Env` is never nil.
- `internal/tools/builtin/shell.go:196-226, 525-547, 813-822` — `blockNetworkLaunch` returning nil on a non-network error is correct (caller falls through to the generic wrap); direct exec confines via `confineDirect` before start.
- `internal/tools/builtin/delegate_workflow.go:396-423` — one engine per background job (so the buffered(1) completion channel cannot block); the detached goroutine is bounded by `EngineTimeout` (30 min, :57) applied inside the codemode sandbox; registry eviction (`registerWorkflowJobForSession`) is under `jobMu` and never touches goroutines.
- `internal/tools/builtin/ledger.go:205-215, 450-560` — identity stamped from ctx (`ACPTurnSourceFromContext`), refuses unattributed turns, `limit` validated, store-side state machine.
- `internal/tools/builtin/timeout_probe.go:70-90` — opt-in-only (`tool_timeout_probe_enabled`), honours ctx, bounded by its own `TimeoutMs`.
- `internal/tools/builtin/goal_tools.go:420-433` — CAS on `revision` is checked before mutation; `complete`/`blocked` correctly gated (apart from A6-1's flag scope).
- `internal/tools/builtin/plan_mode.go:58-75, 109-131` — controller map under `mu`; `PlanMode` persistence fallback is read-only.
- `internal/employee/goal.go:1049-1153` — `UpdateWithTransition` transition fence + CAS predicate correct; the `callerRevision == 0` fallback to `cur.Revision` is last-write-wins only for constructed goals, and every production `Update` caller passes a store-loaded copy (goal_loop.go:1303/1472/2218/2336/2437/2472, manager.go:2754/2863/2922/3166, scheduler_jobs.go:326 all `Get`/`ListActive` first).
- `internal/employee/goal_loop.go:2000-2019` `lookupActiveGoal` / `daemon/employee_service_adapter.go:578` — fresh store load every call (no cached `*Goal` with a stale revision).
- `internal/employee/manager.go:2803-2822` `ResumeGoalGateParked` — holds `invokeMu` across the drain, but the drain callback is marked `goalGateDrainLocked` on the manager path (no self-deadlock at HEAD).
- `internal/employee/enforcement.go:646-695, 700-731` — `tools_forbidden`/`tools_allowed` canonicalized on both sides (:663/:685); step 1c (:715) only fires when `caller_approved` is present and not `"true"` (non-voice turns byte-identical); `details` is executor-stamped, not tool input.
- `internal/employee/enforcement.go:1670-1685` — `PeriodicAuditor.Audit` guards `len(turns)==0` before `turns[0]`.
- `internal/employee/messaging.go:63-200` — CHECK migration probes sqlite_master, pre-flights bad rows, rebuilds in one transaction with rollback; index capture excludes auto-indexes (`sql IS NOT NULL`).
- `internal/employee/handler_auth.go:82-97` `resolveCaller` — nil store returns `(nil, nil)` only for the documented legacy flat-key path; a store-missed key fails closed (`errCallerUnresolved`, :94) without logging key material.
- `internal/employee/budget_checker.go:60-75, 100-135, 150-170` — read-modify-write races between `RecordGoalRound` and `RecordRoundSpend` are serialized per employee by the invoke mutex (the deferred closure calls `RecordGoalRound` before `Trigger`, outside the mutex, but `RecordRoundSpend` only fires from the loop's completion path inside it); `context.Background()` use is on in-memory/store-local calls.
- `internal/employee/wiring.go:268-281` + `daemon/components.go:5578, 7697-7699` — `StopAnchor` is wired and called from `stopComponents`.
- `internal/scheduler/scheduler.go:928-937` — interval jobs (the driver's one-shot) are skipped and purged on restart, so a stale `employee.goal-round.*` registration cannot re-fire after a restart; `executeJob` holds no scheduler mutex across `job.Execute`, so the in-callback `Unschedule` is safe; `Stop` bounds the drain on `ctx`.
- `internal/tui/command_handler.go:116-121` — `/voice` (voice_command.go:77/93/104 synchronous `rpc.Call`) runs inside the `tea.Cmd` goroutine, not `Update`.
- `internal/tui/models/chat.go:2299, 4098, 4110` — all direct RPC calls sit inside `tea.Cmd` closures.
- `internal/tui/models/messagequote.go:75-85` — `QuoteSelected` bounds-checks `selectedMsgIdx` against `len(m.messages)` (stale selection after `ctrl+l` clear is a no-op).
- `internal/tui/app.go:1598-1630` — `SetProjectPath` runs in a `tea.Cmd`; dismiss/confirm clear `activeModal` (no stuck overlay).
- `internal/tui/modals/project_prompt.go:65-120` — `esc` now dismisses instead of `tea.Quit` (range fix); all paths hide the modal before emitting.
- `internal/skills/lifecycle/origin.go:43-70`, `writer.go:274-290, 394-410`, `evolver.go:594-615` — CRLF/quoted-origin tolerance closes two fail-opens; per-funnel sidecars; user-locked skip precedes the verifier.
- `internal/employee/manager.go:2026-2036` (M17) — `triggerViaGoalLoop` round ctx fans the shutdown ctx; `go stopOnShutdown()` exits on `roundCtx.Done()` (no goroutine leak past the round); `Trigger` refuses post-shutdown rounds only when the caller's own ctx is live.
- `internal/bot/notification_bot.go:40-66, 100-110` — nil emitter → no goroutine; nil logger defaulted.
- `internal/tools/builtin/github_pr.go:37-49` and the five `effects.Run/Complete` sites — claimant is session-scoped; no "default" fallback.

## Not audited

- `internal/tools/builtin/patch_validator.go`, `hashline.go` and the deletions `hashline_parser.go`, `knowledge_graph.go`, `memory_curation.go` (range removed code; the surviving `hashline.go` change is 20 lines of dead-code removal — not read line-by-line).
- `internal/tui/models/status.go`, `internal/tui/render/syntax.go` (deleted in range; no HEAD code to audit).
- `internal/employee/drift.go`, `internal/bot/webhook.go` (deleted in range; `webhook.go`'s replacement `internal/employee/webhook_trigger.go` is post-range and only skimmed).
- `internal/tools/builtin/office_pptx.go:460-1061` and `office_docx.go:175-452` body (template/replace/merge logic) — only the path-resolution and write sites were read; the OOXML manipulation was not reviewed for zip-slip or decompression bombs beyond noting the existing `maxPPTXEntryBytes` guard at :614-620.
- `internal/tools/builtin/delegate_control.go`, `delegate_register.go` (398 + 103 lines) — only the workflow-job registry section of `delegate_workflow.go` was read; the child spawn/cancel/list control tools were not reviewed.
- `internal/tools/builtin/platform.go` async delegate section (`executeAsync`) — read the diff hunks only, not the full function at HEAD.
- Post-range additions in scope packages (`cron_trigger.go`, `goal_loop_retry.go`, `result_topic.go`, `kanban_tools.go`, `intake_save.go`, `tool_job_*.go`, TUI `kanban_view*.go`, `chat_nav.go`, `sessions_tree.go`, `approval_card.go`) are outside the assigned range and were not audited.
