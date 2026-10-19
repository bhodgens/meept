# Leaf 01 — [acp] decision config (Contract A)

Parent: `master.md` (Contracts A + the independence rule, decisions 2-3).
Branch: config. Single writer of config surfaces in this tree.

## Scope

Make `permission_mode: "decision"` a valid, documented ACP mode and add its
independent threshold knob.

## Tasks

### Task 1: failing tests (existing file)

Extend `internal/config/decision_model_test.go` OR the ACP config test that
asserts `Validate()` mode acceptance (grep `permission_mode` in
`internal/config/*_test.go` for the owner file; if none exists for ACP, add
cases to the closest existing config validate test — NO new test files).
Cases:
- `PermissionMode: "decision"` valid when `DecisionThreshold` in (0, 1].
- `PermissionMode: "decision"` + `DecisionThreshold: 0` or `> 1` → error
  mentioning `decision_threshold`.
- `PermissionMode: "permissive"` with `DecisionThreshold: 0` → valid
  (unused knob may stay zero outside decision mode).
- `DefaultConfig().ACP.PermissionMode == "permissive"` unchanged;
  `DefaultConfig().ACP.DecisionThreshold == 0.5`.

### Task 2: implementation

- `ACPConfig` gains `DecisionThreshold float64` with json/toml tags per
  Contract A (schema.go:2499 region).
- `Validate()` switch: add `"decision"`; bounds check as above.
- `DefaultConfig()`: `DecisionThreshold: 0.5`.
- `config/meept.json5` `[acp]` section: `permission_mode` comment updated to
  list all three modes; add `decision_threshold: 0.5` with a one-line comment
  naming fail-closed + independence from `decision_gate_enabled`.
- Docs: the configuration page for ACP under `docs/configuration/` (grep
  `permission_mode` to find it) gains the mode + knob.
- Run `make docs-generate` (schema changed).

## Verification

```
go test -p 2 ./internal/config/ -run 'ACP|Validate' -count=1
go build ./internal/config/
```
RED before, GREEN after (report both).

## Stop clause

If `decision_threshold` naming collides with an existing ModelsConfig field
(it must not — decision-model knobs are `decision_escalation_threshold` /
`decision_gate_*`), report before proceeding. Report — do not silently
rename.

## Out of scope

No session/manager/daemon code. No e2e suite changes.
