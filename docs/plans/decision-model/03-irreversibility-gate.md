# Leaf 03 — irreversibility gate hook

## Goal

A `BeforeToolCallHook` that consults the DecisionClient on
irreversible/destructive tool calls and escalates to the existing
user-confirmation flow when confidence is below the threshold.

## Depends on

Leaf 02 (DecisionClient) landed.

## Tasks

1. `internal/tools/builtin/decision_gate.go` — `DecisionGateHook`
   implementing `PrepareNextTurn`-era hook interface
   `BeforeToolCallHook` (read `internal/agent/hooks.go` for the exact
   signature — verify arg types before writing; do not guess).
2. Semantics (Contract D):
   - Fires only when: DecisionClient.Available() AND the tool's spec
     marks it destructive. Spec marking: read how shell/confirmation.go
     flows mark `requires_confirmation` — prefer a hook-side allowlist
     config knob `decision_gate_tools` ([]string, default
     ["shell","file_write"] — VERIFY real tool names via the registry
     before freezing defaults; report actual names) over modifying every
     tool spec.
   - Renders noul question: tool name + one-line args summary →
     `DecisionQuestion{Type:"noul", Prompt:"May this action run?"}`.
   - Confidence >= DecisionEscalationThreshold → allow (return zero
     modification).
   - Below → deny the call by returning the hook's deny/skip
     modification (read hooks.go for the mechanism; if the hook can only
     inject messages, inject a tool-result-shaped message mirroring
     ConfirmationResponse with requires_confirmation=true so the UI's
     existing confirm flow picks it up).
   - Client error (incl. ErrDecisionUnavailable) → fail OPEN (allow) and
     log at Warn. The gate must never make things worse when the
     decision layer is down.
3. Config: `decision_gate_enabled` bool (default false — opt-in) next to
   DecisionEscalationThreshold in schema.go + DefaultConfig false +
   models.json5 commented entry. Opt-in keeps zero behavior change for
   existing installs.
4. Wiring: nil-safe setter on the hook's consumer in
   `internal/agent/loop.go` (follow the SecretScanner SetSecretScanner
   injection pattern) + construction/wiring in
   `internal/daemon/components.go` near the other hook wirings.
5. Tests: hook disabled → never fires; enabled + client unavailable →
   fail-open; enabled + high confidence → allow; enabled + low
   confidence → confirmation-shaped deny. Use the stub client pattern
   from leaf 02's tests.
6. Sweep: `go test -p 2 ./internal/tools/... ./internal/agent/...` green;
   both binaries build.

## Constraints

- The gate NEVER executes or marks an action done. It only chooses
  allow vs ask.
- Explicit-path commit.

## Report back

RED/GREEN evidence, the real destructive tool names found, the exact
deny mechanism used, wiring locations (file:line).
