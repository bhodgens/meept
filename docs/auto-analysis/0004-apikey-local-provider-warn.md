# Local/No-Auth Providers Silently Excluded from Broker Failover

**Date**: 2026-05-15
**Phase**: 1 (daemon startup & core transport)
**Severity**: medium
**Component**: `internal/daemon/components.go` (line 1928)

## Description

Providers without an API key are silently skipped when building the broker's failover model list. This prevents local servers (which don't require authentication) from being included in model failover chains.

Additionally, the warning message for providers without API keys always references `GALA_API_KEY` regardless of the actual provider, producing misleading log output.

## Reproduction

1. Configure a provider without `apiKey` in `models.json5`:
```json
"local": {
  "api": "openai",
  "options": { "baseURL": "http://127.0.0.1:8080/v1" },
  ...
}
```
2. Start daemon
3. Observe logs:
```
DEBUG msg="Skipping provider without API key" provider=local
```
4. The provider works for direct resolution but is excluded from failover

## Evidence

`internal/daemon/components.go:1927-1931`:
```go
// Skip if no API key
if provider.Options.APIKey == "" {
    logger.Debug("Skipping provider without API key", "provider", providerID)
    continue
}
```

And the misleading warning in `resolveModelRef()` at line 1598-1601:
```go
if !hasKey {
    logger.Warn("API key not set or not expanded",
        "expected_env", "GALA_API_KEY",  // hardcoded, wrong for non-gala providers
        "hint", "Set GALA_API_KEY environment variable",
    )
}
```

## Root Cause

The API key check was written assuming all providers need authentication. Local servers (llama.cpp, etc.) don't require API keys.

The warning message hardcodes `GALA_API_KEY` instead of checking which provider is being resolved.

## Proposed Fix

1. Add a boolean `"no_auth"` option to provider config to explicitly mark providers that don't need API keys
2. Change the skip condition: `if provider.Options.APIKey == "" && !provider.Options.NoAuth`
3. Fix the warning message to reference the actual provider name, not a hardcoded env var

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
