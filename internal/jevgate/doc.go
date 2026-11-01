// Package jevgate implements the FAIL-CLOSED command-safety score gate on
// shell tool calls (jev-command-gate tree leaf 01,
// docs/plans/20261031-jev-command-gate/01-scorer-gate.md, Contract C3).
//
// POLARITY DECISION (documented, deliberate): this gate is FAIL-CLOSED.
// Every non-clean path -- scorer error, nil result, Degraded result,
// unparseable answer, score below threshold, confidence below floor, scorer
// unavailable -- escalates to the existing confirmation flow (ASK the human),
// never a silent allow. Rationale: the gate guards high-stakes LOCAL side
// effects, which already carry permission-table ASK semantics
// (internal/security/engine.go stage 1.5); the cost asymmetry is extreme --
// a false allow can be an irreversible local side effect while a false
// escalate costs one human click through the designed fallback (research:
// docs/research/jev-command-gate-calibration.md section 1.2). This is the
// DELIBERATE OPPOSITE of the builtin decision gate
// (internal/tools/builtin/decision_gate.go), which fails OPEN so that a down
// decision layer never blocks work; the ACP decision lane is fail-closed
// (internal/llm/AGENTS.md decision-lane notes). This package adds a third
// surface and flips neither existing one. The stage pipeline in
// internal/security/engine.go stays authoritative: this hook lives upstream
// at the tool-call layer and only ever escalates, never downgrades.
//
// The score is rubric-anchored, NOT a calibrated probability
// (docs/research/jev-command-gate-calibration.md section 4). Default
// threshold 0.90 (conservative end of the 0.85-0.90 band; leaf 03's teacher
// sweep freezes the exact value later). Confidence pre-condition defaults
// LOW: self-reported confidence is a VETO signal (low confidence forces
// ASK), never an allow signal.
package jevgate
