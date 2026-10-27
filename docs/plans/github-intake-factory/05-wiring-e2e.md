# Daemon Wiring + Hermetic E2E Suite — Implementation Leaf (05)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed — make them pass; do NOT
> weaken them. Do NOT commit. Explore with search_files or terminal cat,
> not read_file. After writing a file, do NOT read it back.

## Meta

- **Parent:** master.md
- **Scope:** daemon wiring (poller + linker + write-back on the proven
  scheduler-adapter path), CLI `meept intake status`, docs, and the
  hermetic e2e suite.
- **Dependencies:** 01, 02, 03, 04 (all components + config exist)
- **Estimated Context:** 65K
- **Concurrency Group:** C

## Goal

Make intake actually run: when `[intake].enabled`, the daemon polls on
the configured interval, tickets become goals, outcomes comment back.
When disabled (default), nothing is scheduled and nothing runs. Then
prove the whole chain hermetically against a fake GitHub.

## Context

The wiring precedent for recurring jobs:
`internal/daemon/components.go:5121` (`employeeSchedulerAdapter{sched:
c.Scheduler}` feeding `ScheduleAssessJobs`). The adapter's
`RunAtInterval(name, interval, fn)` is idempotent per name. The #64
github wiring (`internal/daemon/github_wiring.go`) is the pattern for:
enabled-gating, fail-closed registration, report-only logging.

The e2e harness: `e2e/harness/daemon.go` builds the scratch daemon;
`e2e/suites/github-pr-ci/` is the reference suite shape (fake GitHub
httptest, `[github]` overlay via config overlay, token file, scripted
turns NOT needed here — intake is daemon-driven, so scenarios assert on
the fake's request counters + the employee goal API surface).

Key files:
- internal/daemon/components.go (~5121) — scheduler adapter pattern
- internal/daemon/github_wiring.go — enabled-gate + pool wiring pattern
- internal/daemon/employee_scheduler_adapter.go — RunAtInterval
- e2e/suites/github-pr-ci/ — suite + fake + overlay reference
- e2e/manifest.json — suite registration + path_map

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/daemon/intake_wiring.go
// wireIntakePoller registers the intake poll job when [intake].enabled.
// Mirror of wireGitHubTools' gating posture:
//   - disabled -> Debug log, return (no job, no error)
//   - enabled + ValidateEnabled() error -> Warn log naming the missing
//     fields, return (fail-closed, no partial wiring)
//   - enabled + valid -> build GitHubPoller + Linker + WriteBack on the
//     SAME githubsvc pool (reuse c.GitHubPool when wired; else build a
//     pool from [github].token sources exactly as wireGitHubTools does),
//     schedule via employeeSchedulerAdapter.RunAtInterval named
//     "intake.poll", interval from PollInterval (default 5m).
// The poll fn: tickets := poller.Poll(ctx); for each:
//   Linker.EnsureGoal(ctx, t, cfg.DefaultEmployee, gateSpecFromCfg);
//   WriteBack.SetDoneLabel(cfg.DoneLabel) at wiring time.
// Poll errors log Warn and wait for the next tick (never panic the
// scheduler goroutine).
func (c *Components) wireIntakePoller(ctx context.Context)
```

Call site: `components.go` next to the github wiring call (~3005), same
surgical-patch discipline.

CLI (cmd/meept): `meept intake status` (report-only, doctor convention)
— enabled?, watch repo, poll interval, required labels, default employee
resolved or missing, gate command. Mirrors `cmd/meept/github.go`.

Docs: `docs/workflows/intake.md` (overview/problem/behavior per sibling
workflow docs) + AGENTS.md package-table row for internal/intake.

### What This Leaf Consumes

```
internal/intake (GitHubPoller, Linker, WriteBack)  (from 01-03)
internal/config.IntakeConfig                        (from 04)
employeeSchedulerAdapter                            (existing)
e2e harness helpers                                 (existing)
```

## Tasks

### Task 1: Daemon wiring (F7)

**Objective:** poller scheduled only when enabled; disabled by default.

**Files:**
- Create: `internal/daemon/intake_wiring.go`
- Modify: `internal/daemon/components.go` (one call site + a Components
  field `intakeCancel context.CancelFunc` for shutdown if the poll loop
  needs it — prefer NO new goroutine: RunAtInterval already owns the
  lifecycle)
- Test: gate `TestIntakeWiring_Enabled`, `TestIntakeWiring_DisabledDefault`

Pool reuse: when `[github]` is also enabled, share `c.GitHubPool` (one
pool). When `[intake]` is enabled alone, build a dedicated pool from
`[github].token_secret` sources (token is shared config; document this
coupling in docs/workflows/intake.md — intake has no separate token).

Run: `go test -p 2 ./internal/daemon/ -run TestIntakeWiring -v` -> PASS.

### Task 2: CLI + docs

**Objective:** user-facing surface per the wiring requirement.

**Files:**
- Create: `cmd/meept/intake.go` (meept intake status; report-only)
- Create: `docs/workflows/intake.md`
- Modify: `AGENTS.md` (package table row: internal/intake)

Run: `go build -o bin/meept ./cmd/meept && ./bin/meept intake status`
-> prints the disabled state cleanly, exit 0.

NOTE (github-pr-ci residual, proven): stage NEW files BY NAME —
`git add cmd/meept/intake.go docs/workflows/intake.md` — tracked-path
staging misses untracked files.

### Task 3: Hermetic e2e suite (F2, F3, F4, F5, F7)

**Objective:** the full loop proven without github.com.

**Files:**
- Create: `e2e/suites/github-intake/fake_github.go` (issues list +
  comments + labels routes; counters; adapt the github-pr-ci fake)
- Create: `e2e/suites/github-intake/main_test.go` (the five gate-test
  scenarios: intake-create-wait, intake-dedup, intake-redispatch,
  intake-writeback, intake-disabled)
- Modify: `e2e/manifest.json` (suite + path_map entries for
  `internal/intake/` + `internal/config/`)

Poll interval in the overlay: short enough for the suite budget (e.g.
"2s" — the config field is a string duration; the loader exemption from
leaf 04 keeps it a string). Goal-completion drive: to reach
GoalHealthy without a live LLM loop, drive the reflect path the way
existing employee e2e tests do (grep `goal_loop_integration_test.go`
for the harness pattern); if that harness surface cannot reach
GoalHealthy hermetically, assert the write-back path at the unit-drive
level inside the suite by invoking WriteBack.Report directly against
the fake — record which level each scenario actually pins.

Run: `make e2e-fast-area AREA=github-intake` -> 5 scenarios PASS.

## Self-Verification Checklist

- [ ] Gate test F7 PASS; disabled default holds (no job registered)
- [ ] `go build ./...` clean; `go vet ./internal/daemon/ ./cmd/meept/` clean
- [ ] CLI report-only, exit 0 on disabled
- [ ] docs/workflows/intake.md complete; AGENTS.md row added
- [ ] `make e2e-fast-area AREA=github-intake` 5/5 PASS
- [ ] manifest path_map covers internal/intake/ + internal/config/
- [ ] scripts/audit-secrets-value-callers.py green (no direct Broker use)
- [ ] gofmt clean everywhere

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Registration/scheduling gated on enabled everywhere (wiring + CLI)
- [ ] No token printed by CLI or logs
- [ ] Docs present; AGENTS.md updated in same commit scope
- [ ] Poll errors never kill the scheduler job (recover/log, no panic)
- [ ] e2e suite hermetic: no live network, no sleeps beyond the overlay
      interval, counters assert dedup + no-storm
- [ ] `make graphs` run if any HTTP/RPC surface changed (none expected)

## Notes

- components.go is huge — patch surgically; do not reorder existing
  registrations. Sibling sessions may hold it; re-read the region
  immediately before patching.
- If the scheduler adapter lacks a shutdown hook for the intake job,
  reuse its idempotent-by-name property (re-registration replaces);
  daemon shutdown leaving a final tick in flight is acceptable for v1 —
  document it.
