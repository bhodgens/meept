# Empty LLM Response Handling Research

## Problem

When the LLM classifier returns empty content or fails, the agent loop:
1. Logs "LLM returned empty content, nudging for more information"
2. Retries up to 3 times with the same failing model
3. Eventually gives up with no real response

Current logs show:
```
level=WARN msg="LLM classifier failed, trying keyword"
  error="Post \"http://127.0.0.1:8080/v1/chat/completions\": dial tcp 127.0.0.1:8080: connection refused"
level=WARN msg="LLM returned empty content, nudging for more information"
  agent=chat iteration=1 nudge_attempts=1 max_nudges=3
```

## Current Fallback Chain

1. **LLM Classifier** (`local/lfm-code`) → FAILS (connection refused)
2. **Keyword Classifier** → WORKS but low confidence (0.3)
3. **Result**: IntentChat with agent_chat, but agent_loop gets empty response

## Root Causes

### Cause 1: Classifier Has No Model Fallback
The classifier uses a direct model reference, not an alias:
```json5
"classifier_model": "local/lfm-code"  // Single point of failure
```

Unlike the `coder` alias which has `["zai/glm-4.7", "ollama/llama3.2"]`, the classifier has no fallback.

### Cause 2: Agent Loop Doesn't Handle Empty Responses
When `nudge_attempts` exhausts, there's no further fallback:
- No model switch
- No user notification that classification failed
- No suggestion to rephrase

## Solutions (from竞品 Analysis)

### Pattern 1: Hermes Agent - Multi-Model Cascade
Hermes uses a cascading fallback:
1. Try primary model (Claude)
2. If timeout/error → try secondary (local Ollama)
3. If still failing → use cached response or suggest alternatives

### Pattern 2: Claude Code - Graceful Degradation
Claude Code handles failures:
1. Detect specific error types (network, auth, rate limit)
2. Network errors → retry with exponential backoff
3. Model errors → switch to backup model
4. Repeated failures → "Unable to process request, please try: 1) Check connection 2) Rephrase request 3) Try simpler task"

### Pattern 3: Cline/Continue - User Guidance
When AI fails:
1. Show exact error to user
2. Suggest specific actions:
   - "Local model not running. Start with: `ollama serve`"
   - "API key expired. Check settings."
   - "Request too complex. Try breaking into smaller steps."

## Recommended Implementation

### Phase 1: Add Classifier Alias (Model Fallback)
```json5
// config/models.json5
"model_aliases": {
  "classifier": {
    "models": [
      "local/lfm-code",      // Primary: fast local model
      "zai/glm-4.5-air",     // Fallback 1: remote API
      "ollama/llama3.2"      // Fallback 2: local Ollama
    ],
    "timeout": 10,
    "max_fails": 2
  }
}
```

Then modify `internal/agent/dispatcher.go`:
```go
// Instead of using cfg.ClassifierModel directly:
model := resolver.ResolveForAlias("classifier")
classifierClient := llm.NewClient(model, ...)
```

### Phase 2: Agent Loop Empty Response Handling
In `internal/agent/loop.go` or `executor.go`:

```go
// When LLM returns empty content after nudging:
if nudgeAttempts >= maxNudges {
    // 1. Try alternate model if available
    if d.resolver != nil {
        altModel, err := d.resolver.RotateToNextModel("classifier")
        if err == nil {
            d.logger.Info("Retrying with alternate model", "model", altModel.ModelID)
            return d.retryClassification(ctx, input, altModel)
        }
    }

    // 2. Fall back to enhanced keyword classification
    intent := d.enhancedKeywordFallback(input)
    d.logger.Info("Using keyword fallback", "intent", intent.Type)
    return intent, nil

    // 3. If all else fails, provide user guidance
    return d.createGuidanceResponse(input, lastErr)
}
```

### Phase 3: User Guidance for Common Failures
Create helper that maps errors to actionable guidance:

```go
func (d *Dispatcher) handleClassificationError(err error, input string) (*Intent, error) {
    // Connection refused → local model guidance
    if strings.Contains(err.Error(), "connection refused") {
        d.logger.Warn("Local model unreachable",
            "suggestion", "Start local model: llama.cpp --port 8080")
        // Fall back to remote model automatically
    }

    // Timeout → suggest shorter request
    if errors.Is(err, context.DeadlineExceeded) {
        d.logger.Warn("Request timeout",
            "suggestion", "Try breaking into smaller requests")
    }

    // Auth error → config guidance
    if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "API key") {
        d.logger.Error("Authentication failed",
            "suggestion", "Check API keys in config/models.json5")
    }

    // Default fallback
    return d.defaultFallback(input)
}
```

## Implementation Priority

1. **HIGH**: Add `classifier` alias with fallback models ✓ COMPLETE
2. **MEDIUM**: Agent loop empty response handling with model rotation ✓ COMPLETE
3. **LOW**: User guidance messages for common failures

## Implementation Status

### Phase 1: Classifier Alias - COMPLETE

Added to `config/models.json5`:
```json5
"model_aliases": {
  "classifier": {
    "models": [
      "local/lfm-code",      // Primary: fast local model
      "zai/glm-4.5-air",     // Fallback 1: remote API
      "ollama/llama3.2"      // Fallback 2: local Ollama
    ],
    "timeout": 10,
    "max_fails": 2
  },
  "summarizer": {
    "models": [
      "local/lfm-code",
      "zai/glm-4.5-air",
      "ollama/llama3.2"
    ],
    "timeout": 15,
    "max_fails": 2
  }
}
```

Updated `internal/daemon/components.go`:
- Added `createAuxiliaryLLMClientWithResolver()` function
- Modified classifier/summarizer client creation to use resolver
- Changed `classifier_model` and `summarizer_model` to use alias references

### Phase 2: Agent Loop Empty Response Handling - COMPLETE

Modified `internal/agent/loop.go` (line 2528-2545):
```go
if l.nudgeAttempts >= l.detectionConfig.MaxNudgeAttempts {
    // Try model rotation before giving up
    if l.modelRef != "" && l.resolver != nil && l.resolver.HasAlias(l.modelRef) {
        newModel, rotateErr := l.resolver.RotateToNextModel(l.modelRef)
        if rotateErr == nil {
            l.logger.Info("Empty response - rotated to alternate model",
                "alias", l.modelRef,
                "new_model", newModel.ModelID,
            )
            l.resolver.RecordAliasFailure(l.modelRef, fmt.Errorf("empty response"))
            l.nudgeAttempts = 0  // Reset counter for new model
            continue
        }
    }
    // Return error if rotation failed or not available
    return "", fmt.Errorf("agent failed to produce output after %d attempts", l.nudgeAttempts)
}
```

### Phase 3: User Guidance - TODO

Still need to implement user-facing error messages for common failures:
- Connection refused → "Local model not running. Start with: `llama.cpp --port 8080`"
- Timeout → "Request too long. Try breaking into smaller requests"
- Auth error → "Authentication failed. Check API keys in config/models.json5"

## Related Files

- `internal/agent/dispatcher.go` - LLM classifier wiring
- `internal/agent/llm_classifier.go` - Classification logic
- `internal/agent/loop.go` - Agent loop nudging
- `internal/llm/resolver.go` - Model alias resolution
- `internal/daemon/components.go` - Auxiliary client creation
- `config/models.json5` - Model configuration
