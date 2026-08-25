# Anthropic Bearer Mode + Wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Bearer-auth mode on `AnthropicClient` (both request paths), `WithAnthropicTokenResolver` option, `createChatterFor` wiring, `CanonicalProviders` entry.
- **Dependencies:** 01-anthropic-flow.md (registry entry exists)
- **Estimated Context:** 55K (exploration 25K + generation 15K + iteration 10K + overhead 5K)
- **Concurrency Group:** WAVE 3 group B
- **Audit references:** none

## Goal

When an Anthropic-transport `ModelConfig` has `OAuthProvider` set (the `anthropic-sub` provider), `AnthropicClient` authenticates with `Authorization: Bearer <resolved-token>` + `anthropic-beta: oauth-2025-04-20` instead of `x-api-key`. Token resolution goes through the shared `TokenResolver` per request, mirroring `client.go`'s pattern. API-key Anthropic configs behave exactly as before.

## Context

`internal/llm/anthropic.go` (1535 lines) implements the Messages transport. Two request paths set headers: non-streaming (~lines 890-905) and streaming (~lines 1050-1060). Both currently do:

```go
if c.config.ProviderID != ProviderIDBedrock {
    httpReq.Header.Set("x-api-key", c.config.APIKey)
    httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
}
```

`internal/llm/client.go` (lines 769-778, 1190-1198) shows the resolver pattern:

```go
if c.tokenResolver != nil && c.oauthProvider != "" {
    token, err := c.tokenResolver.ResolveToken(ctx, c.oauthProvider)
    if err != nil {
        return nil, &ClientError{Message: "failed to resolve OAuth token", Cause: err}
    }
    req.Header.Set("Authorization", "Bearer "+token)
} else if apiKey != "" {
    ...
}
```

`createChatterFor` (provider_manager.go:115-145) builds `AnthropicClient` via `WithAnthropicLogger/Budget/Timeout` options; it currently passes the TokenResolver ONLY to the OpenAI-compatible branch.

Key files:
- `internal/llm/anthropic.go` — struct + options + two header blocks
- `internal/llm/provider_manager.go:115` — createChatterFor
- `internal/llm/provider_registry.go` — CanonicalProviders list

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/llm/anthropic.go
func WithAnthropicTokenResolver(tr TokenResolver, provider string) AnthropicClientOption
// AnthropicClient struct gains unexported tokenResolver TokenResolver + oauthProvider string.

// Header logic (both request paths):
//   if c.tokenResolver != nil && c.oauthProvider != "" {
//       token, err := c.tokenResolver.ResolveToken(ctx, c.oauthProvider)
//       err -> &ClientError{Message: "failed to resolve OAuth token", Cause: err}
//       httpReq.Header.Set("Authorization", "Bearer "+token)
//       httpReq.Header.Set("anthropic-beta", "oauth-2025-04-20")
//   } else if c.config.ProviderID != ProviderIDBedrock {
//       httpReq.Header.Set("x-api-key", c.config.APIKey)
//       httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
//   }
//   NOTE: when OAuth is active, anthropic-version must STILL be set (only the
//   auth header + beta differ). Adjust: version header moves outside the auth
//   branch; x-api-key only in the non-OAuth branch.

// provider_manager.go createChatterFor, inside isAnthropic branch:
//   if tr != nil && cfg.OAuthProvider != "" {
//       opts = append(opts, WithAnthropicTokenResolver(tr, cfg.OAuthProvider))
//   }

// provider_registry.go CanonicalProviders entry (Contract 4 verbatim):
//   anthropic-sub / "Claude (Pro/Max subscription)" / TransportAnthropicMessages /
//   AuthOAuthDevice / https://api.anthropic.com / CapStreaming, CapTools, CapImages, CapThinking
```

### What This Leaf Consumes

`TokenResolver` (interface.go:70), `AnthropicClientOption` pattern, existing anthropic tests as templates.

## Tasks

### Task 1: Option + struct fields

**Objective:** Add resolver plumbing without behavior change.

**Files:**
- Modify: `internal/llm/anthropic.go`
- Test: `internal/llm/anthropic_bearer_test.go`

**Step 1: Failing test** — construct with `WithAnthropicTokenResolver(stub, "anthropic-sub")`; no compile error; resolver field set (observable via Task 2 behavior; for Task 1 just assert construction + existing tests still pass).

**Step 2:** FAIL. **Step 3:** Add fields + option (nil-guard the option per repo convention: only set when tr non-nil). **Step 4:** PASS + `go test ./internal/llm/ -count=1` (regression).

### Task 2: Non-streaming path Bearer headers

**Objective:** OAuth mode headers on the primary Chat path.

**Files:**
- Modify: `internal/llm/anthropic.go` (non-stream header block)
- Test: `internal/llm/anthropic_bearer_test.go`

**Step 1: Failing test** — httptest server; client with resolver returning "tok-1"; assert `Authorization: Bearer tok-1`, `Anthropic-Beta: oauth-2025-04-20`, `Anthropic-Version` present, `X-Api-Key` ABSENT. Resolver returning error → ClientError "failed to resolve OAuth token". Without resolver: `X-Api-Key` present, `Authorization` absent, beta absent (regression).

**Step 2:** FAIL. **Step 3:** Restructure the header block per the contract (version always for non-Bedrock; auth branch on resolver presence; Bedrock guard preserved: Bedrock still gets NO auth headers here). **Step 4:** PASS.

### Task 3: Streaming path Bearer headers

**Objective:** Same for the stream request.

**Files:**
- Modify: `internal/llm/anthropic.go` (stream header block ~1050-1060)
- Test: `internal/llm/anthropic_bearer_test.go`

**Step 1: Failing test** — mirror Task 2 against ChatStream (SSE body: minimal `event: message_start\ndata: {...}` then close; or however existing stream tests stub it — reuse their fixture pattern).

**Step 2:** FAIL. **Step 3:** Apply identical logic. Consider extracting a helper `applyAuthHeaders(req, ctx)` to avoid duplication between paths (both call sites share it; Bedrock guard inside). **Step 4:** PASS.

### Task 4: createChatterFor + registry entry

**Objective:** Wire the option; add the provider definition.

**Files:**
- Modify: `internal/llm/provider_manager.go` (isAnthropic branch)
- Modify: `internal/llm/provider_registry.go` (append entry)
- Test: `internal/llm/anthropic_bearer_test.go`

**Step 1: Failing test** — `createChatterFor(&ModelConfig{ProviderID: "anthropic-sub", BaseURL: "https://api.anthropic.com", OAuthProvider: "anthropic-sub"}, nil, slog.Default(), stubStore)` returns `*AnthropicClient` (type-assert; resolver wired — observable via a field accessor or a test-only hook; simplest: assert the type and rely on Tasks 2-3 for header behavior). `GetProviderByID("anthropic-sub")` → found, `AuthType == AuthOAuthDevice`.

**Step 2:** FAIL. **Step 3:** Add the 3-line wiring + registry entry (Contract 4 verbatim). **Step 4:** `go build ./... && go test ./internal/llm/ -count=1` full green.

## Self-Verification Checklist

- [ ] 4 tasks done; `go test ./internal/llm/ -count=1` green (existing anthropic tests unaffected)
- [ ] Bearer mode: x-api-key ABSENT, beta + version + Bearer present — both paths
- [ ] API-key mode byte-identical behavior (no beta header, no Authorization)
- [ ] Registry entry verbatim; no other CanonicalProviders entries touched

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] All tests passing; regression tests for API-key mode present
- [ ] Version header set in BOTH modes (common bug: dropping it in OAuth branch)
- [ ] Bedrock guard intact (Bedrock: no x-api-key, no Bearer, no beta)
- [ ] Option nil-guards (repo setter convention)
- [ ] Helper extraction (if used) keeps both call sites consistent
- [ ] No tokens logged; no debug artifacts; gofmt; no line-number corruption

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- `anthropic-beta` may already be set elsewhere for thinking/1M features — grep for existing `anthropic-beta` usage in anthropic.go; if the header is composed from a feature list, APPEND `oauth-2025-04-20` rather than overwrite (comma-joined).
- Do NOT touch the Anthropic request/response mapping, cache control, or metrics — auth headers only.
- `anthropicAPIVersion` constant stays as-is.
