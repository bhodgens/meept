# Leaf 01: Disable Thinking for Classification Calls

## Goal

Make `LLMClassifier.Classify` and `IntentAnalyzer.AnalyzeTrueIntent` send
`enable_thinking: false` to llama.cpp-compatible endpoints so thinking
models answer directly instead of burning the token budget on reasoning.

Root cause being fixed: LFM2.5-8B-A1B (served at 127.0.0.1:8080) is a
thinking model. With no enable_thinking field sent, it reasons in-band;
the classifier's 200-token cap truncates generation mid-think; the response
`content` field arrives empty; classification fails with "empty content".

Verified from `~/.meept/logs/runtimes/127.0.0.1-8080.process.log`
(2026-08-25): both completions stopped exactly at their caps
(300/200 tokens), `truncated = 0`, followed by "empty content" in the
daemon log.

## Files You Will Touch

1. `internal/llm/reasoning_translate.go` — add a case for OpenAI-compat
   local providers.
2. `internal/agent/llm_classifier.go` — pass WithReasoning(Enabled: false).
3. `internal/agent/intent_analyzer.go` — same.
4. `internal/llm/reasoning_translate_test.go` (or new test file) — tests.

Do NOT touch `internal/llm/client.go`. Do NOT change token caps (leaf 02).
Do NOT modify resolver/alias code (leaf 03).

## Interface Contract (exposed to siblings)

No new exported symbols. The contract is behavioral:

- When a caller passes `WithReasoning(&llm.ReasoningConfig{Enabled: false})`
  and the target model has capability "reasoning" or "thinking", the
  outgoing request payload MUST contain:
  ```json
  "chat_template_kwargs": {"enable_thinking": false}
  ```
- Callers that pass no reasoning config are unaffected (payload unchanged).

Check first: read `ReasoningConfig.IsZero()` in internal/llm/reasoning.go.
If `Enabled:false` makes IsZero() true, shouldSendReasoning returns false
and nothing is sent — that would silently break this leaf. In that case,
fix IsZero (or the gate) so an explicit Enabled=false is NOT zero-valued,
with a test proving it.

## Tasks (TDD)

### Task 1: IsZero semantics

Read `internal/llm/reasoning.go`. Write test:

```go
func TestReasoningConfig_EnabledFalseIsNotZero(t *testing.T) {
	rc := &ReasoningConfig{}
	b := false
	rc.Enabled = &b
	if rc.IsZero() {
		t.Fatal("explicit Enabled=false must not be zero-valued")
	}
}
```

Run it. If it fails, adjust `IsZero()` so an explicitly-set Enabled pointer
makes the config non-zero. Re-run until green.

### Task 2: local-provider passthrough

In `reasoning_translate.go`, inside `applyOpenAICompatReasoning`, add a
case arm BEFORE the default:

```go
case ProviderIDOllama, "qwen", "local", "gala-mlx", "gala-llama":
	// llama.cpp / vLLM style: template-level thinking toggle.
	// Qwen3 uses the same field name.
	enable := rc.ResolveEnabled()
	body["chat_template_kwargs"] = map[string]any{
		"enable_thinking": enable,
	}
```

Note: merge with the existing ProviderIDOllama/"qwen" case if you prefer
one arm — but ensure BOTH `enable_thinking` AND `chat_template_kwargs`
semantics don't conflict. Simplest correct shape: keep one case emitting
both fields:

```go
case ProviderIDOllama, "qwen", "local", "gala-mlx", "gala-llama":
	enable := rc.ResolveEnabled()
	body["enable_thinking"] = enable // qwen native
	body["chat_template_kwargs"] = map[string]any{"enable_thinking": enable}
	if budget := ResolveBudget(rc, nil, nil, globalBudgets); budget != nil {
		body["thinking_budget"] = *budget
	}
```

Write table test `TestApplyOpenAICompatReasoning_LocalProvider` covering:
- rc Enabled=false → payload has chat_template_kwargs.enable_thinking == false
- rc nil → payload unchanged
- provider "zai" → no chat_template_kwargs (existing behavior intact)

### Task 3: classifier opts in

In `internal/agent/llm_classifier.go` Classify(), alongside the existing
`llm.WithMaxTokens(200)` option:

```go
noThinking := false
llm.WithReasoning(&llm.ReasoningConfig{Enabled: &noThinking}),
```

### Task 4: analyzer opts in

Same change in `internal/agent/intent_analyzer.go` AnalyzeTrueIntent()
(next to its `llm.WithMaxTokens(300)`).

### Task 5: full package verification

```bash
go build ./internal/... 
go vet ./internal/llm/ ./internal/agent/
go test ./internal/llm/ ./internal/agent/
```

All green required.

## Self-Verification Checklist

- [ ] Explicit Enabled=false produces chat_template_kwargs in payload
- [ ] nil reasoning config produces NO new payload keys
- [ ] zai provider path untouched (existing tests still pass)
- [ ] classifier and analyzer both send the option
- [ ] No other callers of WithReasoning changed
- [ ] go build/vet/test green on internal/llm and internal/agent

## Review Checklist

- Does the diff avoid changing default behavior for callers that do NOT
  opt in?
- Are the new payload fields only added when reasoning config is present?
- Any risk to GLM (zai) thinking-block handling? (Must be none — different
  switch arm.)

## Report Format

Return: files changed, test names added, test output tail (PASS lines),
and any deviation from this spec with rationale.
