# Leaf 03: Alias Failover for Classifier and Intent Analyzer

## Goal

Make classification failures actually rotate the `classifier` alias chain
(`local/lfm-1.2b-q8 → local/lfm-8b-f16 → zai/glm-4.5-air → ollama/llama3.2`).
Today the chain is dead config: alias health tracking exists in
`internal/llm/resolver.go` (`RecordAliasFailure` / `RecordAliasSuccess` /
`ResolveForAlias`) but only the agent loop calls it (`loop.go:3795`). The
classifier and intent analyzer hold a plain `*llm.Client`, resolved once at
daemon startup, and never report failures — so a sick local endpoint serves
100% of classification traffic forever.

## Files You Will Touch

1. `internal/agent/llm_classifier.go` — add resolver-aware retry.
2. `internal/agent/intent_analyzer.go` — same.
3. `internal/agent/dispatcher.go` — pass resolver + alias name into both
   (DispatcherConfig gains fields; construction site ~line 341).
4. `internal/daemon/components.go` — populate the new fields at the
   ClassifierClient construction site (~line 618, where
   `createAuxiliaryLLMClientWithResolver` is called with
   `c.LLMResolver` already in scope).
5. Tests: `internal/agent/classifier_failover_test.go` (new).

Do NOT touch internal/llm/resolver.go (existing API is sufficient).
Do NOT touch token caps (leaf 02) or reasoning fields (leaf 01).
COORDINATION: leaves 01/02 also edit llm_classifier.go and
intent_analyzer.go. Dispatch this leaf LAST or have the orchestrator
merge sequentially. Keep changes additive to minimize conflicts.

## Interface Contract

```go
// LLMClassifierConfig additions:
type LLMClassifierConfig struct {
	// ... existing fields ...
	Resolver  *llm.Resolver // nil = no failover (current behavior)
	AliasName string        // e.g. "classifier"; required when Resolver != nil
}
```

Retry semantics (both classifier and analyzer):

1. Attempt Chat against current client.
2. On error (including ErrEmptyResponse — errors.Is / ClassifyClassificationFailure):
   - If Resolver == nil → return error (unchanged behavior).
   - Else RecordAliasFailure(alias, err); ResolveForAlias(alias) to get the
     next candidate ModelConfig; build/swap the underlying client config;
     attempt once more.
3. On success after rotation → RecordAliasSuccess(alias).
4. Max 2 total attempts (current + one rotation). No loops.

Client-swap mechanism: `*llm.Client` is concrete. Two options — pick ONE:

- **Preferred**: add `func (c *Client) Reconfigure(cfg *ModelConfig)` to
  internal/llm/client.go that swaps c.config under c.configMu (the field
  already has RLock readers; verify no other unsynchronized reads). Small,
  additive.
- Fallback: hold `ModelConfig` + rebuild a Client per attempt via
  `llm.NewClient` inside the classifier. Acceptable but allocates.

Document which you chose in the report.

## Tasks

### Task 1: Reconfigure (if chosen)

Test: `TestClient_Reconfigure_SwapsConfig` — build client with model A
config, call Reconfigure with B, issue request against httptest server,
assert it hits B's URL. Use existing test helpers/patterns from
internal/llm tests.

### Task 2: Classifier failover

In Classify(), wrap the Chat call:

```go
resp, err := c.client.Chat(ctx, messages, opts...)
if err != nil && c.resolver != nil && c.aliasName != "" {
	c.resolver.RecordAliasFailure(c.aliasName, err)
	if nextCfg, rerr := c.resolver.ResolveForAlias(c.aliasName); rerr == nil {
		c.reconfigure(nextCfg)          // Task 1 mechanism
		resp, err = c.client.Chat(ctx, messages, opts...)
		if err == nil {
			c.resolver.RecordAliasSuccess(c.aliasName)
		}
	}
}
```

Preserve the existing unavailable-cooldown logic — note interaction: the
cooldown currently short-circuits before Chat. Rotation must happen BEFORE
the cooldown gate returns "unavailable", otherwise a dead primary still
blocks fallbacks. Reorder carefully: check cooldown only for the CURRENT
candidate, not the whole alias.

### Task 3: Analyzer failover

Same pattern in AnalyzeTrueIntent. IntentAnalyzer has no cooldown logic;
simpler.

### Task 4: Wiring

DispatcherConfig gains:

```go
Resolver      *llm.Resolver
ClassifierAlias string // default "classifier" when Resolver set
```

components.go: at the ClassifierClient site (~line 618), also stash
`c.LLMResolver`. At dispatcher construction (~line 1973), pass through.

If cfg.ClassifierClient is nil (falls back to main LLMClient), do NOT wire
failover — main-client failure handling is a different path.

### Task 5: Tests

`classifier_failover_test.go`:
- `TestLLMClassifier_RotatesOnEmptyResponse`: httptest primary returning
  empty content, secondary returning valid JSON intent; assert result OK
  and resolver health shows success on candidate 2.
- `TestLLMClassifier_NoRotationWithoutResolver`: same setup, nil resolver;
  assert error returned (no panic).
- `TestIntentAnalyzer_RotatesOnEmptyResponse`: mirror of the first.

Use existing mock patterns from dispatcher_test.go where possible.

### Task 6: Verification

```bash
go build ./...
go vet ./internal/agent/ ./internal/llm/
go test ./internal/agent/ ./internal/llm/
```

## Self-Verification Checklist

- [ ] Empty response triggers exactly ONE rotation, then succeeds or errors
- [ ] Success after rotation records AliasSuccess (health resets)
- [ ] Cooldown no longer blocks candidates other than the failed one
- [ ] Nil resolver = byte-for-byte old behavior
- [ ] components.go wires resolver + alias for classifier AND analyzer
- [ ] go build/vet/test green

## Review Checklist

- Is there any path where rotation loops? (Max attempts enforced?)
- Does Reconfigure respect configMu? Any data race with concurrent Chat?
- Did the cooldown reorder change behavior for single-provider configs?
- Are new DispatcherConfig fields documented?

## Report Format

Files changed, chosen swap mechanism + why, test names + PASS tails,
cooldown-reorder diff summary, deviations + rationale.
