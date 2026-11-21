# Execution tracking: job-tools

Worktree: /Users/caimlas/git/meept/.hermes/worktrees/job-tools
Branch: plan/job-tools (from 48655d53)
Design: docs/plans/20261120-job-tools/00-design.md (rulings at 326-345 authorize implementation)

## Wave 1 - foundations (parallel)

| id | scope | files | state |
|----|-------|-------|-------|
| A | scheduler result lane + spill + cancel registry | internal/scheduler/run_result.go (new), persistence.go, jobs.go, scheduler.go, timing.go | PENDING |
| B | shared two-phase shell guard in internal/security + builtin delegate | internal/security/shell_guard.go (new), internal/tools/builtin/shell.go | PENDING |
| C | e2e suite + manifest entries + docs rows | e2e/suites/tools-jobs/ (new), e2e/manifest.json, docs/workflows/job-scheduling.md, docs/workflows/tool-routing.md | PENDING |

## Wave 2 - tools + wiring

| id | scope | files | state |
|----|-------|-------|-------|
| D | job_start/status/stop tools + provider Discover | internal/tools/builtin/tool_job_*.go (new), schedule_register.go | PENDING |
| E | permission + routing + grants + pins + daemon wiring | pkg/security/permissions.go, internal/agent/executor.go, spec.go, internal/config/schema.go, schema_tools_test.go, tool_action_completeness_test.go, internal/daemon/always_full_tools_registry_test.go, internal/daemon/components.go | PENDING |

## Gates

- go build ./... (worktree)
- go test -p 2 ./... (macOS: bounded parallelism)
- make e2e-fast
- make analyzers
- make fmt-check

## Notes

- Design item 19 says append "tools-jobs" to existing path_map keys for internal/scheduler/, internal/tools/, internal/security/; do not add duplicate keys.
- Each leaf reports file:line evidence. Orchestrator verifies before commit.


## Gate results (2026-11-20)

- make analyzers: PASS (mutexio, predid, selflock)
- go build ./...: PASS
- targeted packages (scheduler, security, builtin, agent, config, daemon, pkg/security): PASS
- make test: 4 failures, all confirmed flakes:
  - internal/scheduler TestProcessMissedTicksCoalesceDisabled / TestOneShotFutureDueNotFiredAtBoot: async TempDir cleanup race, pass in isolation
  - internal/shadow TestTeacherClient_CostUsesRealTokenCounts: port exhaustion (can't assign requested address), passes in isolation
  - internal/signal TestSupervisorRestartOnCrash: passes 3/3 alone here; passes at base commit
- make e2e-fast-area AREA=tools-jobs: 2 real defects found (exit-code mapping; job_status timing). Fix children dispatched.
