# F15 — Execution-time fence for scheduled shell jobs (bughunt P1, conservative branch)

Status: **scheduler-side seam LANDED at `f29e9233c`; daemon wiring HANDED
OFF** (exceeds the one-additive-line rule; exact wiring below).
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

Gates at commit: `go test -count=1 ./internal/scheduler/` ok (23s),
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
with `Guard` only — and the session root is NOT a boot-time constant: it is
known per-job at creation time (the creating session's bound working
directory) and jobs persist/reload across restarts, so the guard's
`c.FenceChecker` path (nil since the shared checker retired,
components.go:1268-1274) cannot be reused by copying one line.

Exact wiring needed (daemon lane + one scheduler-side follow-up field;
all scheduler-side hooks already exist):

1. **Scheduler-side follow-up (prerequisite, one small additive commit):**
   add `JobDependencies.FenceFactory func(sessionRoot string) JobFence`
   (run_result.go / jobs.go), consulted by `NewShellJobWithDeps` when
   `deps.Fence == nil && deps.FenceFactory != nil && cfg.ShellConfig != nil`,
   with the root read from a new persisted `ShellJobConfig.SessionRoot`
   (additive JSON: old rows load unchanged and keep the legacy posture).
   The factory's non-nil result becomes BOTH the job's fence and its
   `fenceRoot`. Until this field lands, the daemon lane cannot complete
   the wiring alone.

2. **Daemon wiring (components.go, alongside the jobDeps build ~:5361):**

   ```go
   jobDeps.FenceFactory = func(sessionRoot string) scheduler.JobFence {
       if sessionRoot == "" { return nil } // unresolvable root -> legacy posture, warn-logged
       fenceAllowRead := []string{"/usr", "/bin", "/sbin", "/etc"} // same as the components.go:5227-5234 sites
       return agent.NewSessionFenceStage(sessionRoot, fenceAllowRead, false, logger)
   }
   ```

   ~10 lines (closure + allow-read constant + logger capture) — far beyond
   one line, hence the handoff.

3. **Where the root is stamped.** `schedule_create` / `cron_create` /
   `job_start` must persist the creating session's resolved working
   directory into `ShellJobConfig.SessionRoot` at creation time. The tools
   already resolve it (executor injects it at creation, `06fb3b657`'s
   details["working_dir"] path — that file belongs to the F2 lane; the
   stamp site is `internal/tools/builtin/tool_schedule_create.go:221`,
   `tool_cron_create.go:184`, `tool_job_start.go:154`).

4. **Recovery path (automatic with 1+2).** Jobs restored from the store on
   boot go through `NewShellJobWithDeps` (`CreateJobWithDeps`), so the
   factory consult covers recovered rows once `SessionRoot` is persisted —
   no separate wiring needed.

5. **Do NOT** wire `c.FenceChecker` into `jobDeps.Fence` directly: it is
   nil by design (components.go:1268-1274) and a shared daemon-rooted
   checker would jail every job against the daemon CWD (the exact S-1/H2
   failure mode), not the creating session's root.

## Pins for the daemon lane (when wiring lands)

- Boot with `fence_enabled:true`, create a shell job from a session rooted
  at DIR with `command: "cat /etc/hosts"` → job fires → settled result is
  failed with "session fence" in the error, nothing spawned.
- Same shape with an in-DIR command → executes normally.
- Legacy rows (no `SessionRoot` persisted, pre-wiring store) → legacy
  unfenced posture (P1-4 shape through the real daemon).

## Landing notes (shared-tree incidents, both resolved)

- The first landing `8dbe83a32` was DROPPED from the branch by a sibling
  lane's `git reset` (reflog HEAD@{1..2}); content recovered from the
  working tree and re-landed re-gated.
- The second attempt (`16a62cd9f`, amended `f79dbf3b4`) swept two
  sibling-owned `internal/employee/*` hunks through a concurrent
  index-lock race; both were restored to HEAD^ state before the final
  commit, so the sibling's L16/T-8 lane keeps ownership (their change was
  left as working-tree modifications for them to commit).
- Final commit `f29e9233c` contains ONLY the 5 owned scheduler paths;
  verified via `git show --name-only`.

## Out of lane / surfaced

- The creation-time fence from `06fb3b657` and this seam are INDEPENDENT
  layers: creation-time rejects at schedule time; this seam denies at fire
  time (TOCTOU window closed only when BOTH are armed — i.e. after the
  daemon wiring above lands).
- F2 (parallel lane) owns `internal/agent/executor.go` /
  `pkg/security/permissions.go`; nothing in this commit touches either.
  No coordination dependency: the seam consumes a `FenceStage`-shaped
  interface, which is what `agent.NewSessionFenceStage` already returns.
