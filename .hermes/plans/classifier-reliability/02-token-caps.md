# Leaf 02: Token Caps Derived From Model Config

## Goal

Replace hardcoded classifier/analyzer token caps with values derived from
the model's configured `max_output`, floored at a sane minimum. Defense in
depth for leaf 01: if a future thinking model still reasons despite
enable_thinking=false (or a caller forgets to set it), classification gets
enough budget to finish instead of truncating to empty.

Root cause context (verified 2026-08-25 from llama-server log): classifier
used `WithMaxTokens(200)`, analyzer `WithMaxTokens(300)`; the served
LFM2.5-8B generated exactly those counts and stopped, producing empty
content. The models.json5 entry declares `"max_output": 2048` for this
model — the hardcoded caps ignored it entirely.

## Files You Will Touch

1. NEW FILE `internal/agent/classification_caps.go` — helper + constants.
2. NEW FILE `internal/agent/classification_caps_test.go`.
3. `internal/agent/llm_classifier.go` — use the helper.
4. `internal/agent/intent_analyzer.go` — use the helper.

Do NOT touch internal/llm/ (leaf 01 owns reasoning fields; leaf 03 owns
resolver wiring). Do NOT change any config files.

## Interface Contract

```go
// classificationCaps.go
package agent

// classificationHardCeiling caps any derived cap so a misconfigured
// max_output cannot make a single classification call unbounded.
const classificationHardCeiling = 2048

// classificationFloor is the minimum workable budget: enough for a
// thinking model to close its think block plus emit a short JSON verdict.
const classificationFloor = 1024

// effectiveClassificationCap returns the token budget for one
// classification call:
//
//	1. Start from cfg.MaxOutput when cfg != nil and MaxOutput > 0.
//	2. Otherwise start from requested.
//	3. Clamp result to [classificationFloor, classificationHardCeiling].
func effectiveClassificationCap(cfg *llm.ModelConfig) int
```

Behavior table (must be covered by tests):

| cfg.MaxOutput | requested (unused now, kept for signature stability) | result |
|---------------|-----------|--------|
| nil cfg       | —         | 1024 (floor) |
| 0             | —         | 1024 |
| 512           | —         | 1024 (floor wins) |
| 2048          | —         | 2048 |
| 99999         | —         | 2048 (ceiling wins) |

Note: floor beats a smaller max_output. Rationale: an empty classification
is strictly worse than exceeding the model's declared output by a bit —
llama.cpp clamps internally anyway. Document this in the doc comment.

## Tasks

### Task 1: Helper + table test

Write `classification_caps_test.go` first with the full behavior table,
then implement `classification_caps.go` until green. The helper takes the
cfg so callers stay one-liners.

### Task 2: Classifier

In `llm_classifier.go` Classify(), the LLMClassifier needs access to the
ModelConfig. Check how it's wired:

- `LLMClassifierConfig` has `Client *llm.Client`. Add `GetModelConfig()`
  usage if Client exposes it; otherwise add field `ModelConfig *llm.ModelConfig`
  to LLMClassifierConfig and populate it where the dispatcher constructs
  the classifier (`dispatcher.go` ~line 341, `cfg.ClassifierClient` /
  `cfg.ClassifierModel` are already there — the daemon's
  `createAuxiliaryLLMClientWithResolver` resolves the ModelConfig; expose
  it or re-resolve via the same resolver call).

Prefer: add `ClassifierModelConfig *llm.ModelConfig` to DispatcherConfig,
set it in components.go next to ClassifierClient (the resolved aliasCfg is
already in scope there — see "Resolved model alias" log line), pass into
LLMClassifierConfig. Keep leaf 03's resolver fields OUT of this leaf.

Then replace `llm.WithMaxTokens(200)` with
`llm.WithMaxTokens(effectiveClassificationCap(cfg.ModelConfig))`.

### Task 3: Analyzer

Same pattern in `intent_analyzer.go`: NewIntentAnalyzer gains the cfg (or
a cap int computed by the dispatcher). Replace WithMaxTokens(300).
Update the dispatcher construction site accordingly.

### Task 4: Verification

```bash
go build ./...
go vet ./internal/agent/
go test ./internal/agent/ ./internal/llm/
```

Also run existing dispatcher tests — constructor signatures may have
changed; update call sites minimally (additive fields, not breaking).

## Self-Verification Checklist

- [ ] Behavior table fully covered by tests
- [ ] No hardcoded 200/300 remains in classifier/analyzer
- [ ] DispatcherConfig changes are additive (existing tests updated, not deleted)
- [ ] components.go passes the resolved ModelConfig through
- [ ] go build/vet/test green

## Review Checklist

- Is the floor-vs-max_output precedence documented and intentional?
- Did any other caller of NewLLMClassifier/NewIntentAnalyzer break?
- Is classificationHardCeiling justified (why 2048)?

## Report Format

Files changed, tests added, PASS tails, deviations + rationale.
