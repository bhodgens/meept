# Jev ScoreGateHook Daemon Wiring - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Construct + register ScoreGateHook in the daemon; schema
  knob; wiring-failure e2e case.
- **Dependencies:** none (gate + gatecapture + store all committed).
- **Estimated Context:** 60K

## Goal

internal/jevgate gate.go:77 `NewScoreGateHook(scorer, config, confirm,
logger)` exists; the daemon never constructs it. The DecisionClient it
needs is already built at components.go ~:1264; gateCapture store wiring
already exists (gatecapture_wiring.go). This leaf makes the gate live
behind an opt-in key, default false.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Schema knob: `JevCommandGate JevGateConfig` on SecurityConfig (security.jev_command_gate.*: enabled false, score_threshold 0.90, confidence_threshold 0.10) + normalize + DefaultConfig + template block with drift annotation. Config struct shape = jevgate.ScoreGateConfig (serialize.go:59) - mirror field names. | Modify: internal/config/schema.go, config/meept.json5, new internal/config/schema_jevgate_test.go | `go test -p 2 ./internal/config/ -run Jev` | keys absent | defaults pinned, json5 override parses, disabled default | Given config; When enabled=true; Then adapter maps to ScoreGateConfig |
| T2 | Construction: beside the decision-gate block (~components.go:1263-1290) build `NewScoreGateHook(decisionClient, adapter(cfg), confirmFlow, logger)` when enabled. ConfirmFlow: reuse the decision-gate confirmation lane (deny->confirm conversion) - read decision_gate.go for the existing pattern; fail-closed stub only if the lane cannot be reused (document which). | Modify: internal/daemon/components.go | `go test -p 2 ./internal/daemon/ -run TestJevGateWiring` (new) | hook nil when enabled | hook non-nil, registered | Given enabled cfg; When components built; Then ScoreGateHook registered |
| T3 | Registration: BeforeToolCallHook peer at the decision-gate registration site (~:2277-2284) + `SetRecorder(gateCap)` wiring so captures flow. Name: "jev-score-gate". | Modify: internal/daemon/components.go | same as T2 | registration absent | present in registry names | Given enabled cfg; When hook registry inspected; Then jev-score-gate present |
| T4 | Wiring-failure e2e case in e2e/suites/jev-command-gate/: armed boot -> shell tool call over threshold -> gate escalates to confirmation; delete registration -> call executes. Reuse the suite's existing harness patterns. | Modify: e2e/suites/jev-command-gate/ (new test file) | `go test -p 2 -tags e2e ./e2e/suites/jev-command-gate/` | wiring unproven | wiring proven + manifest scenarios updated if needed | Given armed daemon; When shell call; Then confirmation flow fires |

## Self-Verification Checklist

- [ ] go build ./... green
- [ ] config/daemon/jev e2e tests green
- [ ] gofmt clean, ASCII only
- [ ] disabled default = zero behavior (gate inert)
- [ ] SetRecorder wired (captures land when armed)

## Review Checklist (orchestrator)

- [ ] Schema keys grep-verified against serialize.go field names
- [ ] Registration beside decision-gate (ordering documented)
- [ ] ConfirmFlow lane choice documented (reused vs stub)
- [ ] e2e wiring-failure property demonstrated

## Do NOT commit. The orchestrator stages and commits after review.
