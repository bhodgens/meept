# F15 — Execution-time fence for scheduled shell jobs (bughunt P1, conservative branch)

Status: **scheduler-side seam LANDED; daemon wiring HANDED OFF** (exceeds the
one-additive-line rule; exact wiring below).

Commit: `8dbe83a32` (scheduler-side fence seam + pins, owned paths only).
Creation-time half (out of lane): `06fb3b657`.

## What landed (scheduler-side seam, nil = legacy)

Files (all owned by this lane):

- `internal/scheduler/run_result.go` — new `JobFence` interface: structural
  mirror of `pkg/security.FenceStage` (`CheckPath` + `CheckCommand`), so the
  production stage minted by `agent.NewSessionFenceStage` and test doubles
  satisfy it WITHOUT the scheduler importing `pkg/security` (import-cycle
  direction preserved; same pattern as the existing `ShellCommandGuard`).
- `internal/scheduler/jobs.go`:
  - `JobDependencies.Fence JobFence` + `JobDependencies.FenceSessionRoot string`
    — optional arming inputs. `nil Fence` = no fence input, no judgment;
    standalone tests and pre-fix embedders are byte-identical.
  - `ShellJob` fields `jobFence`/`fenceRoot` threaded through `newShellJob`
    (signature grew fence/fenceRoot params; `NewShellJob` passes nils).
  - `ShellJob.Execute` **phase 3**: after guard phase 2, before spawn —
    when armed, `jobFence.CheckCommand(guardSource, effectiveWorkDir())`.
    `guardSource` is the same unwrapped `/bin/sh -c` source the guard
    classifies (never the literal `/bin/sh`); `effectiveWorkDir()` returns
    the job's `ShellConfig.WorkDir` when set, else the armed session root
    (implicit-cwd posture: unset work_dir means the session's bound
    directory, never an unjudged daemon CWD). Denial wraps as
    `"session fence: <err>"` and settles a failed result (result-lane
    identity preserved).
- `internal/scheduler/schedule_fence_pins_test.go` — 5 pins:
  - P1-1: armed fence + `cat /etc/hosts` job → DENIED at fire time; judged
    source is the unwrapped shell source; fence consulted exactly once.
  - P1-2: unset `work_dir` judged against the ARMED SESSION ROOT (and the
    same out-of-root command still denied).
  - P1-3: explicit out-of-root `work_dir` judged as-is and denied.
  - P1-4 (the nil=legacy pin): deps with NO fence → legacy job executes
    and writes its output file unchanged.
  - P1-5: nil `JobDependencies` nil-safe.
- `env_guard_pins_test.go` / `spill_guard_pins_test.go` — mechanical
  newShellJob-signature updates only (`""` = fence unset).

Gates at commit: `go test -count=1 ./internal/scheduler/` ok (22s),
`go test -count=1 ./internal/memory/` ok (shared-tree gate), `go build ./...`
ok, gofmt clean, go vet clean (scheduler package).

Non-vacuity: every pin runs through the REAL seam (`NewShellJobWithDeps` +
`ShellJob.Execute`) with a posture-faithful fence double (absolute-path-
token-outside-root denial, mirroring `fence.go`'s `checkPath`+pathWithin
verdict on out-of-root targets). On the pre-fix tree P1-1 cannot even be
expressed — there is no fence input to arm — so the pins fail by
construction on revert. An in-place jobs.go probe was user-blocked (shared
tree); verification instead ran the double+seam logic standalone (all 3
probe cases green).

## WIRING HANDOFF (daemon lane — NOT a single additive line)

The seam arms only when `JobDependencies.Fence` is non-nil. Today
`internal/daemon/components.go` ~:5349-5380 builds `jobDeps` ONCE at boot
with `Guard` only — and crucially **the session root is not a boot-time
constant**: it is known per-job at creation time (the creating session's
bound working directory) and jobs persist/reload across restarts, so the
guard's `c.FenceChecker` path (nil since the shared checker retired,
components.go:1268-1274) cannot be reused by copying one line.

Exact wiring needed (daemon lane + `internal/scheduler` approval for any
new accessor; all scheduler-side hooks already exist):

1. **Creation path (primary).** Where the scheduler tools register jobs —
   `scheduler.RegisterJob` / `CreateJobWithDeps` callers reached from
   `internal/tools/builtin/tool_schedule_create.go:221` /
   `tool_cron_create.go:184` / `tool_job_start.go:154` — the shell job must
   be built with a per-job fence instead of the shared boot-time
   `jobDeps.Guard` fence slot. Minimal shape:

   ```go
   // internal/daemon (e.g. components.go, alongside the jobDeps build ~:5361)
   jobDeps.FenceFactory = func(sessionRoot string) scheduler.JobFence {
       if sessionRoot == "" { return nil } // unresolvable root -> legacy posture, logged
       fenceAllowRead := []string{"/usr", "/bin", "/sbin", "/etc"} // same as components.go:5227-5234 sites
       return agent.NewSessionFenceStage(sessionRoot, fenceAllowRead, false, logger)
   }
   ```

   This needs a scheduler-side addition (NOT in this commit — it adds an
   exported field): `JobDependencies.FenceFactory func(sessionRoot string) JobFence`,
   consulted by `NewShellJobWithDeps` when `deps.Fence == nil &&
   deps.FenceFactory != nil && cfg.ShellConfig != nil`, using
   `cfg.Metadata["session_root"]` (or a new `ShellJobConfig.SessionRoot`
   persisted field — preferred, additive JSON, old rows load unchanged) as
   the root, and the factory result BOTH as the fence and the effective
   `fenceRoot`. Until that scheduler-side field lands, the daemon lane
   cannot complete the wiring alone.

2. **Where the root comes from.** The creating tools already resolve the
   session's working directory (executor injects it at creation time,
   `06fb3b657`'s details["working_dir"] path); it must be stamped into the
   job config at `schedule_create`/`cron_create`/`job_start` time — the
   daemon lane owns `internal/tools/builtin` registration args and the
   `agent.Executor` injection point is `06fb3b657`'s own code
   (`internal/agent/executor.go`, ~:536 area).

3. **Recovery path (must not be forgotten).** Jobs restored from the store
   on boot go through `scheduler.go`'s load path (recovery_pins_test.go
   coverage), NOT the tool path. With the persisted `ShellJobConfig.SessionRoot`
   from (1)/(2), `NewShellJobWithDeps`'s factory consult covers recovered
   rows automatically — but ONLY if the boot-time `jobDeps` in
   `components.go` (~:5361-5377) carries the `FenceFactory` closure. That
   daemon-side block is ~10 lines (closure + allow-read constant + logger
   capture), far beyond one line.

4. **Do NOT** wire `c.FenceChecker` into `jobDeps.Fence` directly: it is
   nil by design (components.go:1268-1274) and a shared daemon-rooted
   checker would jail every job against the daemon CWD (the exact S-1/H2
   failure mode), not the creating session's root.

## Pins for the daemon lane (when wiring lands)

- Boot with `fence_enabled:true`, create a shell job from a session rooted
  at DIR with `command: "cat /etc/hosts"` → job fires → settled result is
  failed with "session fence" in the error, nothing spawned.
- Same job created from a session rooted at DIR with in-DIR command →
  executes normally.
- Legacy rows (no session root persisted, pre-wiring store) → legacy
  unfenced posture (P1-4 shape through the real daemon).

## Out of lane / surfaced

- The creation-time fence from `06fb3b657` and this seam are INDEPENDENT
  layers: creation-time rejects at schedule time; this seam denies at fire
  time (TOCTOU window closed only when BOTH are armed — i.e. after the
  daemon wiring above lands).
- F2 (parallel lane) owns `internal/agent/executor.go` /
  `pkg/security/permissions.go`; nothing in this commit touches either.
  No coordination dependency: the seam consumes a `FenceStage`-shaped
  interface, which is what `agent.NewSessionFenceStage` already returns.
- Two fence-pin test runs during development were flake-free; full
  scheduler suite (including 20s timing suites) green before commit.
