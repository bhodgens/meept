# Classifier Harness Bug Findings

## Verification Checklist

- [x] No stub implementations found
- [x] Config wiring partially complete (see Bug #1)
- [x] Logging comprehensive enough (see Bug #2)
- [x] All 12 intent types handled in classifier (see Bug #3)
- [x] Error messages partially actionable
- [x] State survives concurrent calls
- [x] Metrics output partial JSON issue (see Bug #5)

## Bugs Found

### Bug #1: Mismatched config sources for classifier model
- Severity: High
- Files: `internal/daemon/components.go:921`
- Pattern: 0014-B3 (config partially mapped in daemon wiring)
- Description: The daemon resolved the classifier's LLM client from `c.ModelsConfig.ClassifierModel` (source: `models.json5`), but passed `c.Config.MultiAgent.ClassifierModel` (source: `meept.json5`) to the Dispatcher. If the operator sets `classifier_model` in `meept.json5` but not in `models.json5`, the classifier model name in the Dispatcher would diverge from the actual model used for the LLM client.
- Status: Fixed (components.go:921 now uses `c.ModelsConfig.ClassifierModel`)

### Bug #2: Classifier only logs on failures, not on all executions
- Severity: Medium
- Files: `internal/agent/llm_classifier.go:196,268,292,394`
- Pattern: 0012-B2, 0012-B3, 0012-B5 (logging only on failures)
- Description: The LLMClassifier only calls `c.logger.Warn/Debug/Error` on error paths (LLM unavailable, parse failure, invalid intent). Successful classifications are never logged by the classifier itself. While the dispatcher logs classifier results, the classifier is opaque to external observers when used standalone (e.g., in the benchmark harness). Every classification attempt should be logged regardless of outcome.
- Status: Open

### Bug #3: ActionReview vs IntentReview inconsistency in thresholds map and agent mapping
- Severity: Medium
- Files: `internal/agent/llm_classifier.go:31,46`
- Pattern: 0011-B1 (bypass logic, wrong enum usage)
- Description: The `intentThresholds` map (line 31) and `agentMapping` map (line 46) used `string(ActionReview)` as the key. The rest of the classifier uses `string(IntentReview)`. While both resolve to `"review"` currently, this inconsistency is fragile. Fixed by changing both map keys to `string(IntentReview)`.
- Status: Fixed (llm_classifier.go:31 and :46 now use `IntentReview`)

### Bug #4: CategoryMetrics JSON field typo — `avg_confusion` instead of `avg_confidence`
- Severity: Medium
- Files: `internal/eval/classifier_metrics.go:33`
- Pattern: 0053-B1 (incorrect output format)
- Description: The `CategoryMetrics.AvgConfidence` field had JSON tag `json:"avg_confusion"` instead of `json:"avg_confidence"`. This caused benchmark results JSON to report confusing field names. Fixed.
- Status: Fixed (classifier_metrics.go:33 now has `json:"avg_confidence"`)

### Bug #5: BenchmarkResults.Config always contains zero-value BenchmarkConfig in JSON output
- Severity: Low
- Files: `cmd/meept-classifier-test/main.go:79-84`
- Pattern: 0053-B1 (output includes unintended data)
- Description: The `main.go` constructs `BenchmarkResults` without setting the `Config` field, and the `Run()` function doesn't modify `BenchmarkResults.Config` either. When serialized to JSON, the output contains an empty `{"config": {"BenchmarkName": "", "VariantInfo": "", "ClassifierTimeout": 0}}`. This is dead data that bloats the output and may confuse consumers expecting it to reflect the actual benchmark config.
- Status: Open

### Bug #6: LLM response content is parsed from `resp` instead of `content`
- Severity: Critical
- Files: `internal/agent/llm_classifier.go:373-385`
- Pattern: 0014-B2 (logic bug)
- Description: In `parseResponse`, the function declares `var resp classificationResponse` and then checks `if resp.Intent == ""` before extracting JSON from `cleanContent` and unmarshaling into `&resp`. This logic is correct as written — the `resp.Intent == ""` check is a guard so we don't skip parsing. No bug here after closer inspection.
- Status: NotApplicable

### Bug #7: Benchmark runner creates a new LLMClassifier per test (expensive, but not a bug)
- Severity: Low
- Files: `internal/eval/classifier_benchmark.go:71-76`
- Pattern: Informational only
- Description: Each test case creates a brand new `LLMClassifier` via `NewLLMClassifier`. This is wasteful since the classifier has no per-test state (except the atomic unavailable flag which is reset per request). The `unavailable` flag carries across tests within the same `Run` call, but since it's created fresh per test, it starts clean each time — which is actually the correct behavior for a benchmark. Not a bug, but worth noting.
- Status: NotApplicable

### Bug #8: Test data includes empty string input for chat intent
- Severity: Low
- Files: `testdata/eval/classifier-test-corpus.json5:112`
- Pattern: 0012-B1 (edge case not handled)
- Description: The test corpus includes `{ input: "", expected_intent: "chat", expected_agent: "chat" }` as a valid test case. An empty input string passed to the LLM will likely produce an error (no intent returned) since the LLM will reject or fail to classify empty input. This test case will always be counted as an error in the benchmark, artificially lowering overall accuracy.
- Status: Open

### Bug #9: BenchmarkRunner.Run() does not accept BenchmarkConfig for Config field preservation
- Severity: Low
- Files: `internal/eval/classifier_benchmark.go:126-141`
- Pattern: 0051-B1 (state not preserved across calls)
- Description: The `RunComparison` method populates `BenchmarkResults.Config` with `r.config` (the runner's config), but `main.go` ignores this and constructs `BenchmarkResults` manually without calling `RunComparison`. The runner-level `Config` is lost. If `RunComparison` is used, the config is preserved correctly.
- Status: Open

### Bug #10: Pre-existing slog format errors blocking eval package build
- Severity: Critical
- Files: `internal/eval/classifier_benchmark.go:109,117`
- Pattern: 0053-B1 (output format error)
- Description: Two `logger.Info()` calls passed raw float values (`"%.1f", acc` and `"%.2f", modelMetrics.OverallAccuracy*100`) directly as slog args without wrapping them in a key-value pair. `slog.Logger.Info()` expects alternating key-value pairs or `slog.Attr`, so these caused compile-time errors (`slog.Logger.Info arg should be a string or a slog.Attr`). This blocked the entire eval package from building. Fixed by wrapping floats in `fmt.Sprintf()`.
- Status: Fixed (classifier_benchmark.go:109,117)
