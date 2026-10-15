# Leaf 01 — decision_model config slot

## Goal

Add a first-class `decision_model` model slot plus ALL decision-feature
config knobs for this tree (leaves 03/04 consume them read-only; this
leaf is the single writer of config surfaces).

## Tasks

1. `internal/config/schema.go` — ProvidersConfig gains:
   - `DecisionModel string` json/toml `decision_model` — comment: "Dedicated
     decision model (Jev-style typed decision endpoint). Empty = decision-model
     features disabled (gates confirmation-only, compaction summarizer).
     Never defaults to small_model."
   - `DecisionEscalationThreshold float64` json/toml
     `decision_escalation_threshold` — "confidence above which a
     decision-model answer is trusted without escalation. 0 = treat every
     answer as below-threshold (escalate)."
   - `DecisionGateEnabled bool` json/toml `decision_gate_enabled` —
     "opt-in irreversibility gate hook on destructive tool calls.
     Requires decision_model. Default false."
   - `DecisionGateTools []string` json/toml `decision_gate_tools` —
     "tool names the gate consults. Empty = sensible defaults chosen by
     the gate implementation."
2. Compaction knobs live in the compaction config section: grep
   `CompressionConfig` in internal/config/schema.go to find the section;
   add `DecisionEnabled bool` (`decision_enabled`, default false) and
   `DecisionKeepBar int` (`decision_keep_bar`, default 2) with comments.
3. `DefaultConfig()` — entries: DecisionModel "", DecisionEscalationThreshold
   0.85, DecisionGateEnabled false, DecisionGateTools nil,
   DecisionEnabled false, DecisionKeepBar 2.
4. `config/models.json5` — add the decision_model slot (empty default)
   + commented gate/compaction knob entries with one-line comments each.
5. Resolver: `internal/llm/resolver.go` — `decisionModel *ModelConfig`
   field wired where smallModel resolves (grep `smallModel =`); accessor
   `func (r *Resolver) DecisionModelName() string` returning the id or
   "". Nil-guard first.
6. Tests (new internal/config/decision_model_test.go): DefaultConfig
   values; JSON5 round-trip of all new knobs.
7. `make docs-generate` if mage/gomarkdoc available; else note in report.

## Constraints

- Later leaves own their code files; this leaf owns ONLY config files +
  resolver field + tests. If resolver.go is contested by a sibling,
  stage explicit paths.
- `go test -p 2 ./internal/config/... ./internal/llm/...` green.

## Report back

RED/GREEN evidence, files, test names, resolver wiring location (file:line).
