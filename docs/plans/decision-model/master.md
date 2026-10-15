# Decision Model Integration — Master Plan

Tree: `docs/plans/decision-model/` (master.md + leaf docs 01–04).

## User decisions (normative, 2026-10-15)

1. **Q1 — replace vs augment:** NO replacement. The decision model is an
   ADDITIVE slot (`decision_model` in models.json5). Nothing in the
   dispatcher is removed; the existing cascade stays primary.
2. **Q2 — tier routing signal:** Use the EXISTING dispatcher
   classification (confidence + complexity from the cascade we already
   run) to extend model-tier routing — NOT Jev. Rationale: the tier
   escalation threshold must be exceeded before we escalate, and the
   existing cascade already emits a calibrated confidence we can
   threshold. Feasibility/practicality assessed in leaf 02 as part of the
   work.
3. **Leaf 03 + 04:** execute with subagents.
4. **Compaction:** existing `ContextCompressor` stays the DEFAULT when no
   decision model is configured. Score-and-drop compaction is an
   OPT-IN lane active only when `decision_model` is defined.
5. **Decision model in config:** `decision_model` becomes a first-class
   model slot in `config/models.json5` + `internal/config/schema.go`.

## Problem statement

Meept's dispatcher already classifies every message (keyword table →
embedding gate → probe → LLM classifier) and the resolver already maps
model tiers (`fast`/`default`). A decision model (Jev-style typed
decision endpoint: fixed label set, calibrated probabilities, zero
generated tokens) adds a fourth source of typed decisions: model routing,
irreversibility gating, and opt-in compaction filtering. It must be
additive: every path works unchanged when the slot is empty/unset.

## Frozen contracts

### Contract A — config slot (leaf 01 owns)

```go
// internal/config/schema.go — ProvidersConfig gains ONE field:
DecisionModel string `json:"decision_model" toml:"decision_model"`
// Empty (default) = decision-model features DISABLED everywhere.
// Never defaults to small_model or classifier.
```

```json5
// config/models.json5 template gains (commented-out or empty default):
// Dedicated decision model: Jev-style typed decision endpoint (fixed
// labels, calibrated probabilities, no generated tokens). Empty =
// decision-model features disabled (tier routing keeps legacy behavior,
// tool gates keep confirmation-only flow, compaction keeps summarization).
"decision_model": "",
```

- `DefaultConfig()` entry: `DecisionModel: ""`.
- Resolver gains `DecisionModel *ModelConfig` + `DecisionModelName() string`
  accessor returning "" when unset. Mirrors the smallModel wiring.
- `make docs-generate` after schema change.

### Contract B — DecisionClient seam (leaf 02 owns)

```go
// internal/llm/decision_client.go (new file)
// DecisionClient asks ONE typed question against the decision model.
// Returns nil error only when a decision model is configured.
type DecisionQuestion struct {
    State   string   // serialized context (rendered state)
    Type    string   // "choice" | "score" | "noul"
    Prompt  string   // question text
    Options []string // choice labels (each must be ONE token after render)
    Threshold float64 // escalate-above bar, 0-1
}
type DecisionResult struct {
    Answer     string
    Confidence float64 // calibrated 0-1; NOT accuracy
    Raw        map[string]float64 // label→prob distribution
}
type DecisionClient struct{ ... } // resolver-backed
func (d *DecisionClient) Ask(ctx, q DecisionQuestion) (*DecisionResult, error)
func (d *DecisionClient) Available() bool // DecisionModel configured?
// ErrDecisionUnavailable when not configured — callers treat as
// "feature disabled", never a turn failure.
var ErrDecisionUnavailable = errors.New("decision model not configured")
```

Implementation shape: logit-read over label tokens via the provider's
completion API with `max_tokens=1` + logprobs when available; graceful
fallback = send the rendered question as a constrained prompt and parse
the single-token answer (document the degradation in the code). Keep the
client thin; do NOT build a full simple-jev server integration in this
tree — the client abstracts "ask one typed question", the serving shape
(logit endpoint vs prompt parse) is behind it.

### Contract C — tier routing signal (ANSWERED, NOT BUILT in this tree)

The user's Q2 (extend tier routing with existing dispatcher
classification instead of Jev) is feasible: the cascade already emits a
confidence on Intent, and `ResolveModelTier` (internal/llm/resolver.go:1112)
is a pure function a confidence-checked wrapper can sit beside. NOT
implemented here — the user asked feasibility, not implementation. The
DecisionClient + slot land anyway as the substrate for leaves 03/04.
Deferred design: `ResolveModelTierWithConfidence(tier, fallback string,
confidence float64)` alongside (never replacing) ResolveModelTier,
escalating fast→default when confidence >= a 0=off knob. No code in this
tree touches ResolveModelTier or dispatcher model selection.

### Contract D — irreversibility gate (leaf 03 owns)

```go
// internal/tools/builtin/decision_gate.go (new file)
// DecisionGateHook: BeforeToolCallHook in the agent loop's hook registry.
// Fires ONLY for tools whose spec marks them irreversible/destructive
// (spec-level flag; shell already has phase-1 confirmation flow —
// DecisionGate is a SECOND signal, never a replacement).
// Asks: noul question "May this action run?" rendered from the tool
// name + arguments summary. Below threshold → same phase-1
// ConfirmationResponse the tool itself would return (user prompt).
// Above → allow through unchanged.
// No decision model configured → hook never fires (zero overhead).
```

- Registration: agent loop hook registry (`internal/agent/hooks.go`
  `BeforeToolCallHook`), nil-safe setter, wired in components.go.
- The gate DECIDES whether to ask the user. It never marks the action
  done. Verification of the action stays in the existing post-action
  path.

### Contract E — score-and-drop compaction (leaf 04 owns)

```go
// internal/llm/decision_compactor.go (new file)
// DecisionCompactor: DECISION-MODEL-ONLY compaction lane. Replaces the
// summarizer Chatter at ContextCompressor stage-2 ONLY when the decision
// model is configured AND compaction.decision_enabled=true (new config
// knob, default FALSE — the existing summarizer is the default lane).
// Each history entry gets ONE score question ("does this entry still
// matter for the current task?" 0-4 rubric). Entries below the keep
// bar are dropped; survivors stay verbatim (no rewrite).
```

- Config: `compaction.decision_enabled` bool (default false) +
  `compaction.decision_keep_bar` int (default 2) in `internal/config/schema.go`
  CompactionConfig section (or ProvidersConfig — leaf 01 decides placement
  with the config owner pattern: behavior-flavored knob lives with its
  behavior).
- Zero decision model → knob ignored, summarizer lane byte-identical.

### Non-goals

- No TypeSafe/Jev hosted product dependency. Any serving stack must be
  open-weight/local or a documented local server (simple-jev pattern).
- No replacement of the dispatcher cascade.
- No quickplan/session-state routing through the decision model (the
  ~84% text-only ceiling stands).
- No threshold tuning on live traffic in this tree (calibration work is
  a separate campaign; this tree ships the plumbing with conservative
  defaults).

## Leaves

- **01** `01-config-slot.md` — decision_model slot + ALL decision knobs
  (config schema + template + resolver field + docs-generate). Branch:
  config/tooling. SINGLE WRITER of config surfaces.
- **02** `02-decision-client.md` — DecisionClient seam only. Branch: llm.
  Depends on 01 (slot + accessor).
- **03** `03-irreversibility-gate.md` — DecisionGateHook on destructive
  tools. Branch: tools+agent. Depends on 02.
- **04** `04-decision-compaction.md` — score-and-drop compaction lane
  (opt-in). Branch: llm. Depends on 02. PARALLEL WITH 03 (disjoint
  files: 03 = tools/agent, 04 = llm/daemon-construction).

Wave order: 01 → 02 → (03 ∥ 04) → integration gates.

## Tracking table

| Leaf | Status | Commit | Notes |
|------|--------|--------|-------|
| 01 | DONE | 44f8f073 | config slot + knobs + resolver field; tests in internal/config/decision_model_test.go + internal/llm/resolver_decision_model_test.go |
| 02 | PENDING | — | |
| 03 | PENDING | — | |
| 04 | PENDING | — | |

## Integration gates

- `go build -o bin/meept-daemon ./cmd/meept-daemon && go build -o bin/meept ./cmd/meept`
- `go test -p 2 ./internal/config/... ./internal/llm/... ./internal/agent/... ./internal/tools/...`
- `make docs-generate` after any schema.go change (leaf 01).
- Pre-commit hooks pass per commit (17 checks).
