# Anthropic PKCE Paste Flow + Refresh - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** PKCE helpers, authorize-URL builder, code exchange with endpoint fallback, JSON/form refresh, `anthropic-sub` registry entry, CLI paste flow.
- **Dependencies:** 01-xai-device-flow/01-registry-extension.md (FlowKind, AuthorizeURL/VerifyHint/RefreshJSON fields)
- **Estimated Context:** 55K (exploration 15K + generation 25K + iteration 10K + overhead 5K)
- **Concurrency Group:** WAVE 2 group A
- **Audit references:** none

## Goal

The Claude Pro/Max login: print an authorize URL (PKCE S256 + state), user opens browser, authorizes, pastes the resulting code into the terminal; meept exchanges it for tokens. Refresh keeps tokens alive via the same token endpoints. All token-endpoint requests use User-Agent `axios/1.7.9` (Anthropic 429-rate-limits `claude-code/`-prefixed UAs there — verified empirically in hermes `anthropic_adapter.py:1451-1460`).

## Context

`internal/auth/device_flow.go` shows the form-POST + tokenResponse patterns. Leaf 01-xai/01 added `Flow`, `AuthorizeURL`, `VerifyHint`, `RefreshJSON` fields and the `FlowPKCEPaste` constant. `cmd/meept/oauth.go` has the Flow switch skeleton with a placeholder `case FlowPKCEPaste:`. `internal/auth/refresh.go` `refreshOne` currently calls `RefreshTokenRequest` (form) — this leaf routes `RefreshJSON`/anthropic providers through `RefreshAnthropicToken` instead.

Frozen constants (hermes anthropic_adapter.py:1443-1464 — the binding values):

```go
const anthropicOAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
var anthropicTokenEPs = []string{
    "https://platform.claude.com/v1/oauth/token",
    "https://console.anthropic.com/v1/oauth/token",
}
const anthropicOAuthRedirectURI = "https://console.anthropic.com/oauth/code/callback"
const anthropicOAuthScope = "org:create_api_key user:profile user:inference"
const anthropicTokenUA = "axios/1.7.9"
const anthropicOAuthBeta = "oauth-2025-04-20" // inference header, leaf 02
```

Key files:
- `internal/auth/device_flow.go` — patterns only
- `cmd/meept/oauth.go` — FlowPKCEPaste case
- `internal/auth/refresh.go` — refreshOne routing

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract 1 + Contract 2 from the branch orchestrator (GeneratePKCE, GenerateState, BuildAnthropicAuthorizeURL, ExchangeAnthropicCode, RefreshAnthropicToken, registry entry). Plus:

```go
// cmd/meept/oauth.go helper (unit-testable):
func readPastedCode(r io.Reader) (string, error) // trims space, errors on empty
```

### What This Leaf Consumes

`FlowPKCEPaste`, struct fields (leaf 01-xai/01); `TokenResult`, `tokenResponse`, `deviceFlowHTTPClient` (existing).

## Tasks

### Task 1: PKCE + state generation

**Objective:** S256 PKCE pair + random state via crypto/rand.

**Files:**
- Create: `internal/auth/pkce.go`
- Test: `internal/auth/pkce_test.go`

**Step 1: Failing test** — verifier: 43 chars (32 bytes base64url no padding), URL-safe; challenge: 43 chars; challenge == base64url(sha256(verifier)) recomputed in test; two calls give different values; state: 32 chars, different per call.

**Step 2:** FAIL. **Step 3:** `crypto/rand.Read(32)`, `base64.RawURLEncoding.EncodeToString`, `sha256.Sum256`. No math/rand (predid analyzer). **Step 4:** PASS.

### Task 2: Authorize URL builder

**Objective:** Exact query string.

**Files:**
- Modify: `internal/auth/pkce.go`
- Test: `internal/auth/pkce_test.go`

**Step 1: Failing test** — parse the built URL: host `claude.ai`, path `/oauth/authorize`, query values exactly `code=true`, `client_id`, `response_type=code`, `redirect_uri=https://console.anthropic.com/oauth/code/callback`, `scope=org:create_api_key user:profile user:inference`, `code_challenge`, `code_challenge_method=S256`, `state`.

**Step 2:** FAIL. **Step 3:** Build with `url.Values` (scope NOT joined with `+` at the value level — it is one space-separated value; `url.Values.Encode` handles escaping). **Step 4:** PASS.

### Task 3: Exchange with fallback

**Objective:** `ExchangeAnthropicCode` tries platform then console.

**Files:**
- Modify: `internal/auth/pkce.go`
- Test: `internal/auth/pkce_test.go`

**Step 1: Failing tests** — (a) primary 200 `{"access_token":"at","refresh_token":"rt","expires_in":3600}` → TokenResult, only 1 call; (b) primary 404, fallback 200 → success, 2 calls; (c) both 500 → error mentioning the last status. Assert request: form body with 5 fields, header `User-Agent: axios/1.7.9`.

**Step 2:** FAIL. **Step 3:** Loop over `anthropicTokenEPs`; per endpoint form-POST (grant_type=authorization_code, code, redirect_uri, client_id, code_verifier), Accept json, UA constant. Missing access_token → error. **Step 4:** PASS.

### Task 4: Refresh (form + JSON) + refreshOne routing

**Objective:** `RefreshAnthropicToken` both encodings; `RefreshManager.refreshOne` uses it for anthropic-sub.

**Files:**
- Modify: `internal/auth/pkce.go`
- Modify: `internal/auth/refresh.go` (route: if `providerCfg.Flow == FlowPKCEPaste` or provider `anthropic-sub` → `RefreshAnthropicToken`)
- Test: `internal/auth/pkce_test.go` (+ refresh routing test if feasible without heavy mocks — a unit test on the routing decision helper is fine)

**Step 1: Failing tests** — form mode asserts body `grant_type=refresh_token&refresh_token=..&client_id=..`; JSON mode asserts `Content-Type: application/json` + JSON body; fallback endpoint logic same as Task 3; omitted refresh_token in response → old token reused (mirror `GetValidToken` convention — that logic lives in the caller; here just return what server sent, possibly empty).

**Step 2:** FAIL. **Step 3:** Implement both encodings behind `RefreshAnthropicToken(ctx, clientID, refreshToken string, useJSON bool)` — note signature extends the parent contract; the parent's 3-arg shape is called by refreshOne with the provider's `RefreshJSON` value. Keep it simple: `func RefreshAnthropicToken(ctx, clientID, refreshToken string, jsonBody bool) (*TokenResult, error)`. Route in refreshOne. **Step 4:** PASS.

### Task 5: Registry entry + CLI paste case

**Objective:** Register `anthropic-sub`; implement `case FlowPKCEPaste:` in `runOAuthConnect`.

**Files:**
- Modify: `internal/auth/providers.go` (map entry only, Contract 2 verbatim)
- Modify: `cmd/meept/oauth.go` (case body + helper `readPastedCode` + help list entry)
- Test: `internal/auth/providers_anthropic_test.go` (entry assertions) + `cmd/meept/oauth_anthropic_test.go` (`readPastedCode` cases: normal, whitespace-only → error, empty → error)

**Step 1: Failing tests** — entry: ClientIDDefault/AuthorizeURL/TokenEP/Flow/BaseURL/Transport/Scopes exact. Helper: `readPastedCode(strings.NewReader("  abc123  \n"))` → "abc123".

**Step 2:** FAIL. **Step 3:** Registry entry. CLI case:

```go
case FlowPKCEPaste:
    verifier, challenge := auth.GeneratePKCE()
    state := auth.GenerateState()
    authURL := auth.BuildAnthropicAuthorizeURL(flowCfg.ClientID, challenge, state)
    fmt.Printf("  visit: %s\n", authURL)
    fmt.Printf("  %s\n", providerCfg.VerifyHint)
    fmt.Print("  code: ")
    code, err := readPastedCode(os.Stdin)
    if err != nil { return fmt.Errorf("read pasted code: %w", err) }
    token, err := auth.ExchangeAnthropicCode(ctx, flowCfg.ClientID, code, verifier)
    if err != nil { return fmt.Errorf("exchange code: %w", err) }
    // save like default case; "anthropic-sub connected."
```

Add `anthropic-sub` to the help provider list. **Step 4:** `go build ./... && go test ./internal/auth/ -count=1`.

## Self-Verification Checklist

- [ ] 5 tasks done; `go test ./internal/auth/ -count=1` + `go build ./...` green
- [ ] Frozen constants verbatim (client ID, endpoints, redirect, scope, UA)
- [ ] crypto/rand everywhere for randomness
- [ ] Only this branch's registry entry + CLI case touched

**DO NOT COMMIT.**

**Deviations from spec:** [none / list — note the RefreshAnthropicToken signature extension if kept]

## Review Checklist (For Review Agent)

- [ ] All tests present + passing
- [ ] UA `axios/1.7.9` asserted in exchange AND refresh tests
- [ ] Fallback endpoint covered (primary-fails case)
- [ ] PKCE values recomputed independently in test (not self-asserted)
- [ ] readPastedCode handles empty/whitespace
- [ ] Registry entry byte-matches Contract 2
- [ ] No tokens logged; conventions per master; no debug artifacts; gofmt

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Verify hermes constants yourself if anything looks off: `terminal cat ~/git/hermes-agent/agent/anthropic_adapter.py | sed -n '1440,1470p'`.
- Do NOT implement the Bearer inference mode — leaf 02 owns `internal/llm`.
- The response may include `expires_in` only (no expiry timestamp) — set `TokenResult.Expiry = now + expires_in` in the exchange/refresh helpers.
