# Design: job_start / job_status / job_stop

Status: design only. No code changed.

## Decision

Build a single job-handle system on top of `internal/scheduler`. Do not build a
second job registry. Do not expose a generic in-process command registry beside
the scheduler.

The scheduler already has cron, one-shot timing, persistence, restart recovery,
claim-before-deliver, DST handling, and manual trigger. A second registry would
duplicate all of that.

Three tools share one opaque handle:

| Tool | Input | Output |
|------|-------|--------|
| `job_start` | `command`, `working_dir?`, `timeout_seconds?`, `at?`, `every?`, `cron?` | `{handle, status:"running"|"scheduled"}` |
| `job_status` | `handle` | `{handle, status, output?, exit_code?, error?, started_at?, finished_at?}` |
| `job_stop` | `handle`, `reason?` | `{handle, status:"cancelled"|"already_done"|"not_found", stopped, exit_code?}` |

`job_start` accepts shell commands only (it maps to a `JobTypeShell` row).
Prompts belong to `schedule_create`'s agent jobs; do not add a prompt mode.

A handle is a scheduler job id: `id.Generate("job-")` (`pkg/id/id.go:18`). The scheduler
already uses `sched-` ids at `internal/tools/builtin/tool_schedule_create.go:143`;
the new prefix prevents confusion with recurring cron rows. One handle maps to
one execution attempt. A re-run mints a new handle.

## Handle kinds

`job_start` creates a **one-shot `TimingAt` shell job** through the scheduler.
This is the integration point with existing scheduling.

- `at?` sets `TimingSpec{Kind: TimingAt, At}` (pattern:
  `tool_schedule_create.go:162-171`). The scheduler's one-shot horizon binds
  here: `validateOneShotHorizon` (`internal/scheduler/timing.go:829-835`)
  rejects `at` more than `[scheduler].one_shot_window` (default 2h,
  `internal/config/schema.go:3432`) in the future with
  `ErrOneShotBeyondHorizon`. `job_start` surfaces that error verbatim; do not
  retry or extend the window in the tool.
- `every?` / `cron?` create a recurring job. Same handle shape; `job_stop` then
  cancels the in-flight run, it does not delete the schedule. `every` is
  floored at `MinTimingEverySeconds = 60` (`timing.go:76`); smaller values are
  a validation error. `at`, `every`, `cron` are mutually exclusive —
  `job_start` rejects more than one; `ParseTiming`/`ValidateJobConfig` back
  the check.
- No `at` / `every` / `cron`: schedule at `time.Now()` and fire immediately.
  The job is still a real scheduler row, so it survives a daemon restart.

Timeout ceiling: every `executeJob` dispatch path wraps its context in a hard
`30 * time.Minute` timeout (`scheduler.go:532` RunNow, `timing.go:976`
one-shot, `wrapJob` `scheduler.go:709` cron-fired). `ShellJob.Execute` then
applies `ShellJobConfig.Timeout` inside that already-capped context, so an
`timeout_seconds` above 30m silently dies at 30m. For this leaf:
`effective_timeout = min(timeout_seconds, 30m)` and `job_start` documents the
cap in its schema description. Lifting the ceiling touches three dispatch
sites and is explicitly out of scope here.


This reuses `fireOneShotOnce` (`internal/scheduler/timing.go:914-1011`) and its
boot recovery. The result store survives the row prune.

## How job_status gets output

Today output is lost after the bus event.

- `JobResult` (`internal/scheduler/jobs.go:155-164`) carries `Output`.
- `ShellJob.Execute` publishes it only on `scheduler.job.completed`
  (`jobs.go:443-466`). That topic is fire-and-forget with no retention.
- `executeJob` publishes a *second* `scheduler.job.completed` event with a
  different shape and no output (`scheduler.go:806-824`). Do not change that
  topic; external subscribers depend on it.

Add a **separate result lane** to the scheduler store.

- `StoreData` (`internal/scheduler/persistence.go:21-26`) gains
  `Results []JobExecutionResult \`json:"results,omitempty"\``.
- A field on `JobConfig` will not work: `fireOneShotOnce` deletes the row on
  success (`timing.go:1000-1009`). The result lane survives the prune.
- Keep `currentStoreVersion = 1` (`persistence.go:29`). `omitempty` makes old
  `jobs.json` files decode with an empty result set. No migration.
- Bound the lane (suggest 256 entries, FIFO) to match the workflow job cap
  (`internal/tools/builtin/delegate_workflow.go:860`).
- `ShellJob.Execute` writes the result through a back-reference on the job,
  mirroring `deliveryScheduler` (`jobs.go:213-216`, set at
  `scheduler.go:969-988`). Add `Guard` and `ResultSink` to `JobDependencies`
  (`jobs.go:613-619`); the daemon constructs `JobDependencies` via
  `scheduler.WithJobDependencies` (`scheduler.go:94-98`) and
  `CreateJobWithDeps` (`jobs.go:786-805`) passes it to `NewShellJob` — wire
  both fields through `NewShellJob`, the same both-paths pattern
  `createJob` uses for `deliveryScheduler`.
- `ShellJob` has no `ExitCode` field today (`jobs.go:355-364`): the result
  derives exit code from the `exec.ExitError` (and `-1`/unset on cancel or
  signal death), not from a struct field.
- Keys are `job_id` (the handle) — a recurring job's successive runs overwrite
  by design, `job_status` returns the latest attempt. A per-attempt key would
  break `job_status`'s only input.
## Output: temp file, not memory (user ruling 2026-11-20)

`ShellJob.Execute` captures stdout/stderr into unbounded in-memory buffers
(`jobs.go:432-436`). Ruled out: jobs must spill to disk, not memory.

- Spawn with `cmd.Stdout`/`cmd.Stderr` as file handles on
  `$MEEPT_HOME/spill/jobs/<handle>.out` (create + truncate at start; the dir
  exists already — `$MEEPT_HOME/spill/tools/` is the shell tool's spill root,
  `shell.go:464`, so `jobs/` sits beside it).
- The result lane stores `{exit_code, status, started_at, finished_at,
  output_path, output_bytes, output_sha256, truncated_to}` — NOT the output
  text. `job_status` returns the first N bytes (default 4 KiB) read back
  from the file plus the path, so consumers pull limited volume on demand.
- `jobs.json`-lane entries stay tiny regardless of command output size; the
  file is the bound. Cleanup: delete the file when the lane entry is evicted
  (FIFO 256), best-effort.
- On `job_stop` kill, the partial file remains readable — `job_status` after
  a cancel returns whatever the command produced before death, with exit
  code 137/143.
- `executeJob` currently calls `job.Execute` for ALL kinds
  (`scheduler.go:795`); the spill + lane write lives inside `ShellJob.Execute`
  so agent/reminder jobs stay out of it.

`job_status` reads the live state from `Scheduler.GetJob` (`scheduler.go:582`)
and the settled output from the result lane. It is a direct core call, not an
RPC hop. Existing precedent: `schedule_get` (`tool_schedule_list.go:168`).

Exit codes follow unix conventions so other agents can consume them:

- `0` = success.
- `1` = generic failure (non-zero command exit maps through; a command exit
  of N>1 is preserved verbatim).
- `137` = SIGKILL-equivalent: `job_stop` group kill or timeout kill.
- `143` = SIGTERM-equivalent (grace-period kill).
- A run lost to a daemon crash mid-execution is recorded FAILED at boot-sweep
  time with exit code `1` and error text `run lost to daemon restart` — every
  settled attempt has a status and an exit code, never a silent gap.

## How job_stop cancels

`runNowCancel` is **scheduler-global** (`scheduler.go:62-65`), canceled in `Stop`
(`:298-302`). Never wire `job_stop` to it. That would cancel every in-flight
job.

Add a per-execution cancel map keyed by handle:

```text
map[handle]{cancel context.CancelFunc, cmd *exec.Cmd, started time.Time, state}
```

- `executeJob` (`scheduler.go:723`) creates a child context from `runNowCtx`
  and stores the cancel. Remove the entry on terminal.
- `job_stop` looks up the handle, calls `cancel()`, then escalates to a process
  group kill after a short grace period.
- `internal/scheduler` does NOT import `internal/pty` today (verified via
  `go list -deps`), and `pty` has no meept-internal imports, so the plan calls
  `pty.KillProcessTree` — this ADDS a scheduler->pty edge. Two options, pick
  one at implementation time:
  1. Inline a 10-line group-kill helper in `internal/scheduler` (syscall.Kill
     to -pgid + Process.Kill, mirroring `pty/session.go:534-548`); keeps the
     scheduler's import set unchanged.
  2. Import `internal/pty` for `KillProcessTree` (verified cycle-safe; pty
     imports no meept packages).
  Option 1 is preferred: the scheduler is a low-level package and a kill
  helper does not justify a new edge.

- Spawn with `SysProcAttr.Setpgid = true` and reuse
  `pty.KillProcessTree` (`internal/pty/session.go:534-548`). Set `WaitDelay`
  like `runtime.LocalBackend` (`internal/runtime/local.go:81`) so a grandchild
  holding the output pipes cannot block the wait.
- Without `Setpgid` + `WaitDelay`, context cancel kills only `sh`, not its
  children. The current direct path has neither (`shell.go:646-657`).
- On cancel, publish a terminal result with status `cancelled` so `job_status`
  converges.
- `job_stop` is idempotent: `already_done` is a successful no-op, matching
  `Registry.CancelTask` (`internal/task/registry.go:486-487`).

`job_stop` cancels an in-flight run. It does not delete a recurring schedule.
Use `schedule_delete` for that.

## Shared shell guard (close the gaps)

`ShellJob.Execute` calls `exec.CommandContext` directly
(`internal/scheduler/jobs.go:410-469`). It applies none of the guards that
`ShellExecuteTool.Execute` applies (`shell.go:554-760`). This is the largest
gap.

Central fence note (agent-groups leaf 01 T3, `components.go:8595-8599`):
per-tool `SetFenceChecker` wiring was REMOVED from the daemon; fence
enforcement moved into the central FenceStage inside
`security.PermissionChecker.CheckPermissionForAgent` (pkg/security), consulted
by every loop's executor. That central stage does NOT cover scheduler-fired
shell jobs — they never pass through an agent loop executor. The
execution-time guard below is therefore still required, and the fence
component of it should call the same FenceStage logic the central stage uses,
not a parallel implementation.

Verified gaps:

- `tool_cron_create.go:172-183` has **no fence check at all**.
- `tool_schedule_create.go:198-210` checks `workDir = "."` but stores `""`
  when the caller passes no `working_dir` (`:215-217`); the job then inherits
  the daemon CWD (`jobs.go:419-421`).
- `ScheduleCreateTool.SetFenceChecker` exists (`tool_schedule_create.go:28-36`)
  but production never wires it. `scheduleProvider` has no fence field
  (`schedule_register.go:64-67`).
- Boot-recovered jobs never pass even the creation-time check.
- The documented TOCTOU is at `jobs.go:398-409`.

Fix at the source: put the shared guard in `internal/security`, the only
cycle-safe home. `builtin` imports `scheduler`, so the guard cannot live in
`builtin`. `pkg/security` cannot import `internal/`.

Extract two phases to preserve the frozen 403-before-400 order
(`shell.go:566-574`):

1. `CheckCommand(command)` — risk classification, permission table. Runs before
   work-dir resolution.
2. `CheckCommandInDir(ctx, command, workDir)` — fence, Tirith scan, network
   class. Runs after resolution.

`internal/security` is the cycle-safe home, and it already imports
`pkg/security` (`go list -deps` verified), so the shared guard can reuse
`pkg/security.ErrCommandBlocked` and keep the existing error identity that
tests pin (`shell.go:566-574` order contract, `safety_test.go:56`). The guard
must also cover what `ShellExecuteTool` covers beyond classify/permission:
`confineDirect` network-class application (`shell.go:308-313`) and the Tirith
orchestrator scan (`shell.go:607`). `ShellJob.Execute` today applies NONE of
these — no network class, no Tirith, no fence at execution time.

Then:

- `ShellExecuteTool.classifyRisk` / `checkPermissionTable` become wrappers so
  existing tests keep compiling.
- `ShellJob.Execute` calls both phases before `exec.CommandContext`.
- `schedule_create` and `cron_create` call phase 1 (and phase 2 when a workdir
  is known) at creation for early UX, but execution-time is the binding check.
- Resolve `filepath.EvalSymlinks(workDir)` at creation and re-verify at
  execution. Do not use `openat2`: the daemon also runs on macOS.

## Wiring checklist

1. New file `internal/scheduler/run_result.go` — result type, store methods.
2. `internal/scheduler/persistence.go:21-26` — add `Results` lane.
3. `internal/scheduler/jobs.go:613-619` — add `Guard`, `ResultSink` to
   `JobDependencies`; `:410-469` — guard + result write + cancel registry.
4. `internal/scheduler/scheduler.go:723-872` — per-run cancel child context.
5. New file `internal/security/shell_guard.go` — shared two-phase guard.
6. `internal/tools/builtin/shell.go` — delegate to shared guard.
7. `internal/tools/builtin/tool_schedule_create.go`,
   `tool_cron_create.go` — delegate creation checks to shared guard.
8. New files `internal/tools/builtin/tool_job_start.go`,
   `tool_job_status.go`, `tool_job_stop.go`.
9. `internal/tools/builtin/schedule_register.go:75-86` — add 3 tools to
   `Discover()`. They ride the existing `"scheduler"` provider.
10. `pkg/security/permissions.go:35` — add 3 `BuiltinRules` rows. Unknown
    action is denied at `:574-581`.
11. `internal/agent/executor.go:258` — add `ToolActionMap` rows.
12. `internal/agent/spec.go:210-233` — grant per-agent, not baseline.
13. `internal/config/schema.go:2591-2612` — add tool names to
    `DefaultAlwaysFullTools()`; all three take args.
14. `internal/config/schema_tools_test.go:67-76` — update exact list.
15. `internal/agent/tool_action_completeness_test.go:52` — add pinned names.
16. `internal/daemon/always_full_tools_registry_test.go:22` — verify.
17. `docs/workflows/job-scheduling.md:20-30` — add 3 rows.
18. `docs/workflows/tool-routing.md` — review.
19. `e2e/suites/tools-jobs/` + `e2e/manifest.json`. The manifest's coverage
    policy requires every `internal/` package to map to at least one suite.
    `internal/scheduler/` and `internal/tools/` ALREADY have `path_map`
    entries — APPEND `"tools-jobs"` to those two lists; do not add duplicate
    keys. Check `internal/security/` for an existing entry and extend it the
    same way (it currently maps to the security suites). Add the new suite
    entry with `status: "todo"` per manifest convention.

Note: an earlier draft said `e2e/manifest.json` carried an unstaged
`generated` bump from other work. Verified 2026-11-20: the working tree has
NO manifest modification (`git status` shows only the staged internal/bot
deletion wave and untracked plan/report dirs). The e2e leaf works from the
committed manifest.

## Tests to preserve

- `internal/tools/builtin/shell_test.go:42,124,212,262,301,325,386,396`
- `internal/tools/builtin/safety_test.go:56`
- `internal/tools/builtin/setters_test.go:33-35`
- `internal/services/terminal_fence_test.go:25`
- `pkg/security/fence_stage_test.go:234`
- `internal/scheduler/scheduler_test.go:450-490` (`TestShellJob`)
- `internal/security/fence_test.go:182,204` (`TestCheckCommand`,
  `TestCheckCommand_PathValidation`) — the shared two-phase guard must keep
  `internal/security.FenceChecker.CheckCommand` behavior identical; phase 2
  delegates to it rather than re-implementing fence logic
- `go test -p 2 ./...`, `make e2e-fast`, `make analyzers`, `make fmt-check`

## Resolved decisions (defaults; user form timed out)

1. Handle prefix: `job-`.
2. `job_stop` cancels the in-flight run only; it does not delete a recurring
   schedule. Use `schedule_delete` for that.
3. `job_start` with no schedule argument fires an immediate one-shot through
   `TimingAt`.
4. Result lane: 256 entries FIFO. Cancel grace: 2s before group kill.
5. Stop at the design doc. The user gates multi-phase work through another
   agent's review before implementation.

## Open questions for the review gate

1. Restart semantics for a `job-` row whose execution was in flight when the
   daemon died: `fireOneShotOnce` stamps `OneShotFiredAt` BEFORE dispatch
   (`timing.go:918-928`, deliberate exactly-once posture), so the run is
   LOST for fire-and-forget kinds and `job_status` will show no settled
   result. Accept silent-loss with the row's `OneShotFiredAt` as the only
   trace, or write a `cancelled`-shaped result at boot-sweep time? Default
   assumed: accept (matches the scheduler's documented posture for shell
   kinds); surface to the review gate.
2. `job_start` on a schedule-disabled daemon (`[scheduler]` off or
   `scheduler not running`): reject the tool call, or boot the scheduler
   dependency on demand? Default assumed: reject with a clear error; the
   scheduler is a core daemon component that boots unconditionally today.
3. Output spill: `ShellJob.Execute` captures unlimited stdout/stderr into
   memory (`jobs.go:432-436`). A long-running job can OOM the daemon. The
   shared-guard leaf does not fix this. Cap at the result-lane write (truncate
   to a fixed size, suggest 64 KiB, with a `truncated` flag) and note the
   unbounded in-memory capture as known debt, or also bound the live buffers?
   Default assumed: truncate at the lane write only.

## Rulings (2026-11-20, user review — supersede the defaults above where they conflict)

1. **Crash-in-flight reporting (open question 1): RULED — report it.**
   Every settled attempt carries a status and an exit code. A run lost to a
   daemon restart is written FAILED (exit `1`, error
   `run lost to daemon restart`) at boot-sweep time into the result lane, so
   `job_status` never shows a silent gap. Unix-consistent exit codes are a
   contract for other agents (exit-code table above).
2. **Scheduler-off behavior (open question 2): RULED — reject, never boot on
   demand.** The scheduler CAN be off in practice, two verified paths:
   `[scheduler].enabled=false` gates construction (`components.go:5147`;
   packaged default `true` at `schema.go:4738`, but an operator config can
   flip it), and a failed `scheduler.NewScheduler` (bad timezone / data dir)
   leaves `c.Scheduler` nil with only a Warn (`components.go:5198-5202`).
   All three job tools return a clear "scheduler not available" error in
   both cases.
3. **Output storage (open question 3): RULED — temp file, never memory.**
   See "Output: temp file, not memory" above. The in-memory `bytes.Buffer`
   capture is REPLACED by file-handle output, not capped; `job_status` reads
   back a limited window (default 4 KiB) from the spill file on demand.

