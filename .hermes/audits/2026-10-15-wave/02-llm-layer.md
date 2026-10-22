# LLM Layer Bughunt Audit - 2026-10-15

## Findings

### F1: Cache key missing tool-order stability across client classes
- **Severity**: MEDIUM
- **Location**: `internal/llm/cache_key_builder.go:186` (Build method)
- **Mechanism**: The `Build` method sorts messages by their index in the slice, but tool calls within messages may not have a stable ordering guarantee across different client implementations. The openai client uses `BuildPrefixAwareKeyWithFiles` while anthropic/codex use `Build` - if tool call ordering differs between clients for the same logical request, cache keys collide incorrectly.
- **Test**: `go test -run TestCacheKeyBuilder ./internal/llm/` with a test that constructs identical logical requests with differently-ordered tool calls and verifies cache key equality.
- **Evidence**: SOURCE - `cache_key_builder.go` shows `sort.Strings` on file paths but no explicit tool-call ordering normalization in the prompt hash computation.

### F2: Empty completion handling in local/MLX client not verified
- **Severity**: LOW
- **Location**: `internal/llm/register_local_model.go` (local model registration)
- **Mechanism**: The local/MLX runtime path doesn't have a dedicated client class that explicitly classifies empty/whitespace completions as `ErrEmptyResponse`. The `reasoning_local_provider_test.go` exists but the actual client code path for mlx_lm doesn't show explicit empty-completion classification.
- **Test**: Run a local mlx_lm server with a model that returns blank responses and verify the client classifies it as an alias failure.
- **Evidence**: SOURCE - No explicit `ErrEmptyResponse` handling found in local/MLX client code paths.

### F3: Cache key sentinel in user content could cause instability
- **Severity**: MEDIUM
- **Location**: `internal/llm/client.go:522` (prompt_cache_key injection)
- **Mechanism**: The `prompt_cache_key` field is injected into the payload for session-based caching. If user content contains the sentinel pattern used for cache key splitting (as referenced in commit aee5e405), the cache key could become unstable. The fix splits on the sentinel, but if the sentinel appears in user-provided content, it could cause false splits.
- **Test**: Send a prompt containing the cache key sentinel pattern and verify the cache key remains stable across identical requests.
- **Evidence**: SOURCE - `client.go:522` injects `prompt_cache_key`; commit aee5e405 mentions cache key splits on sentinel.

## Checked and Clean

- `internal/llm/errors_quota.go` - QuotaResetError implementation and helpers
- `internal/llm/provider_manager.go` - Quota error handling in retry loops
- `internal/llm/resolver.go` - ErrAllModelsQuotaBlocked/ErrAllEndpointsBlocked usage
- `internal/llm/client.go` - OpenAI client empty completion handling
- `internal/llm/anthropic.go` - Anthropic client empty completion handling
- `internal/llm/codex.go` - Codex client empty completion handling
- `internal/llm/failure_policy.go` - Failure classification

## Gates run

- `grep -rn "QuotaResetError" internal/llm/` - Found 40+ references, all using `errors.AsType` or `IsQuotaResetError`
- `grep -rn "ErrAllModelsQuotaBlocked\|ErrAllEndpointsBlocked" internal/llm/` - All uses check with `errors.Is`
- `grep -rn "==.*ErrAll\|==.*Quota" internal/llm/` - No problematic `==` usage found for quota errors
- `go build ./internal/llm/...` - Builds successfully

## Not covered

- `internal/compress/` package - not in scope per task instructions
- `internal/context/` package - not in scope per task instructions
- Parking/slot gate/endpoint cooldown state machines - limited time, grep-only approach
- Detailed token accounting double-record investigation - would require tracing all call paths
