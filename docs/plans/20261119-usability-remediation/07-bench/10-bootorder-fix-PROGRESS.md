# 07-bench/10 BOOT-ORDER FIX -- PROGRESS

Task: fix F1 + F2 (CRITICAL) from 09-components-audit-REPORT.md, add
regression tests, no commit.

## F1 -- SetEffectsLedger called before the ledger exists

- [x] Read audit report + call sites (components.go 2823/2827; ledger opened
      3391-3399; bkpkg process-wide setter is backup/git_ops.go:212).
- [x] Confirmed push idempotency + backup-push claiming were silently
      unarmed on every production boot (nil at the call sites; setters
      ignore nil, so the legacy unclaimed path ran forever).
- [x] Fix applied: moved the two SetEffectsLedger calls to a new
      "post-ledger wiring" block immediately after the effects-ledger init
      block (components.go, right after `c.EffectsReconciler` assignment,
      before the execution-gate block). Old early block deleted. Same
      nil-degradation comments preserved (the setters are nil-guarded, so
      the degraded-open path keeps legacy behaviour unchanged).
      Note: bkpkg.SetEffectsLedger is PROCESS-WIDE (package var) -- calling
      it later with the same ledger value is idempotent.
- [x] Regression test: internal/daemon/effects_ledger_wiring_test.go
      - TestEffectsLedgerWiredAfterConstruction (default config):
        c.EffectsLedger non-nil AND bkpkg process-wide func returns non-nil
        ledger (exposes the F1 class -- before the fix the package func
        stayed nil on every boot).
      - TestEffectsLedgerWiringDegradedPath (unopenable data dir):
        nil ledger, no panic, both setters stay unwired, daemon still boots.

## F2 -- wireInstructions runs before c.Scheduler is assigned

- [x] Read instruction_wiring.go + Start block (components.go ~6298).
- [x] Confirmed instruction_wiring.go had NO separate scheduler-arming
      entry point: InstructionScheduler is only CONSTRUCTED inside
      wireInstructions behind `if daemonScheduler != nil`, and Start()
      never re-armed it. No existing skipped/failing instruction_wiring
      test found.
- [x] Fix applied: split wireInstructions -- store/handler/listener/parser/
      verifier/injector wiring stays at the early site (components.go
      ~3089); InstructionScheduler CONSTRUCTION moves to a new
      (c *Components) wireInstructionScheduler(logger) called immediately
      after the `c.Scheduler = sched` block (~5314). Start() lifecycle
      (c.InstructionScheduler.Start at ~6301) is unchanged and now has a
      live object to start on every boot where cfg.Scheduler.Enabled.
      Comment at the early site updated to name the split.
- [x] Regression test: internal/daemon/instruction_scheduler_wiring_test.go
      - TestInstructionSchedulerArmedWhenSchedulerEnabled (default config,
        scheduler on): c.InstructionScheduler non-nil AND its target job
        scheduler IS c.Scheduler (exposes the F2 class -- before the fix
        the field stayed nil on every boot).
      - TestInstructionSchedulerNilWhenSchedulerDisabled: nil scheduler,
        nil instruction scheduler, no panic.

## Verification

- [x] go build ./...
- [x] go test -p 2 ./internal/daemon/ ./internal/instructions/...
      ./internal/push ./internal/backup ./internal/scheduler ./internal/services
      (instructions/... and internal/push do not exist in this tree; the
      equivalent packages -- preferences, scheduler, services, backup,
      full daemon -- all pass. See 10-bootorder-fix-REPORT.md.)
- [x] gofmt -l clean on touched files.
- [x] Mutation proof: both primary regression tests FAIL against HEAD
      (pre-fix) components.go, PASS with the fixes.
- [x] Audit report updated (F1/F2 -> FIXED, commit refs pending).
- [x] 10-bootorder-fix-REPORT.md written.

## Constraints honoured

- ASCII only (no em-dashes), %w error wrapping in new code, gofmt clean.
- NO COMMIT (per task).
- Surgical, context-unique patches; components.go re-read before each patch
  (another session may hold dirty edits there).
