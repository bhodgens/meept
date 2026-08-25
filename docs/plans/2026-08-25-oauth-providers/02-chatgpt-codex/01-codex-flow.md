# OpenAI Codex Device Flow - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Implement OpenAI's non-standard device flow (usercode → poll → PKCE exchange) + `openai-codex` registry entry + CLI wiring.
- **Dependencies:** 01-xai-device-flow/01-registry-extension.md (FlowKind type, DeviceUserCodeEP/DevicePollEP fields)
- **Estimated Context:** 55K (exploration 15K + generation 25K + iteration 10K + overhead 5K)
- **Concurrency Group:** WAVE 2 group A
- **Audit references:** none

## Goal

Three-step "Sign in with ChatGPT" login, matching hermes `_codex_device_code_login` (auth.py:7797-7960) exactly: request usercode, poll for authorization grant, exchange grant for tokens via authorization_code + PKCE. Plus the registry entry and the `FlowDeviceCodex` case in `runOAuthConnect`.

## Context

`internal/auth/device_flow.go` has the RFC 8628 flow; this leaf adds a parallel `codex_flow.go` because the endpoint shapes differ fundamentally (JSON bodies on usercode/poll; grant-not-token from poll; PKCE exchange). `cmd/meept/oauth.go` `runOAuthConnect` has a `switch providerCfg.Flow` skeleton (from 01-xai/02) with a placeholder `case FlowDeviceCodex:` returning an error — this leaf replaces that placeholder.

Key files:
- `internal/auth/device_flow.go` — patterns for HTTP + error handling (do not modify except reading)
- `cmd/meept/oauth.go` — the Flow switch (~line 126)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/auth/codex_flow.go
type CodexDeviceResult struct {
    UserCode     string
    DeviceAuthID string
    VerifyURL    string
    Interval     time.Duration
}
type CodexAuthGrant struct {
    AuthorizationCode string
    CodeVerifier      string
}
func StartCodexDeviceFlow(ctx context.Context, userCodeEP, clientID string) (*CodexDeviceResult, error)
func PollCodexAuthorization(ctx context.Context, pollEP, deviceAuthID, userCode string, interval time.Duration) (*CodexAuthGrant, error)
func ExchangeCodexToken(ctx context.Context, tokenEP, clientID string, grant *CodexAuthGrant) (*TokenResult, error)

// internal/auth/providers.go — registry entry (Contract 2 from parent, verbatim)
```

### What This Leaf Consumes

`FlowDeviceCodex`, `OAuthProviderConfig.DeviceUserCodeEP`/`DevicePollEP` (leaf 01-xai/01); `TokenResult`, `deviceFlowHTTPClient` pattern (existing).

## Tasks

### Task 1: StartCodexDeviceFlow with 429 retry

**Objective:** Request a usercode; retry on 429 honoring Retry-After.

**Files:**
- Create: `internal/auth/codex_flow.go`
- Test: `internal/auth/codex_flow_test.go`

**Step 1: Failing test** — httptest: first response 429 + `Retry-After: 1`, second 200 `{"user_code":"ABC-123","device_auth_id":"daid","interval":"5"}`; assert result fields + `VerifyURL == "https://auth.openai.com/codex/device"` + `Interval == 5s`. Second test: 429 four times → error mentioning rate limit. Third: non-200 → error mentioning status.

```go
func TestStartCodexDeviceFlow_Retries429(t *testing.T) {
    calls := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        if calls == 1 {
            w.Header().Set("Retry-After", "1")
            w.WriteHeader(429)
            return
        }
        ct := r.Header.Get("Content-Type")
        if ct != "application/json" { t.Errorf("content-type %q", ct) }
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, `{"user_code":"ABC-123","device_auth_id":"daid-1","interval":"5"}`)
    }))
    defer srv.Close()
    res, err := StartCodexDeviceFlow(context.Background(), srv.URL, "cid")
    if err != nil { t.Fatal(err) }
    if res.UserCode != "ABC-123" || res.DeviceAuthID != "daid-1" { t.Errorf("%+v", res) }
    if res.VerifyURL != "https://auth.openai.com/codex/device" { t.Errorf("verify %q", res.VerifyURL) }
    if res.Interval != 5*time.Second { t.Errorf("interval %v", res.Interval) }
    if calls != 2 { t.Errorf("calls %d", calls) }
}
```

**Step 2:** FAIL. **Step 3:** Implement: `parseRetryAfter(h http.Header) time.Duration` (seconds int; 0 when absent/invalid), loop max 4 attempts, sleep `retryAfter` else `2^attempt` capped [1,60]s, JSON body `{"client_id": clientID}`, `Content-Type: application/json`, `Accept: application/json`. Interval = `max(3, int)` per hermes. Errors wrap `%w`/fmt.Errorf; no token material in messages. **Step 4:** PASS.

### Task 2: PollCodexAuthorization

**Objective:** Poll until grant; 403/404 = pending; 15-minute deadline.

**Files:**
- Modify: `internal/auth/codex_flow.go`
- Test: `internal/auth/codex_flow_test.go`

**Step 1: Failing test** — server returns 404 twice then 200 `{"authorization_code":"ac","code_verifier":"cv"}`; use `interval=1ms`-style tiny interval injected via the parameter (pass `10*time.Millisecond`); assert grant fields + 3 calls. Test ctx-cancel mid-poll returns ctx error. Test deadline: server always 404, ctx with 50ms deadline → timeout error.

**Step 2:** FAIL. **Step 3:** Implement loop: sleep(interval) then POST JSON `{"device_auth_id":…,"user_code":…}`; 200 → unmarshal grant (error if fields empty); 403/404 → continue; other → error with status. Respect `ctx.Done()` between iterations. **Step 4:** PASS.

### Task 3: ExchangeCodexToken

**Objective:** Authorization-code + PKCE exchange at the token endpoint.

**Files:**
- Modify: `internal/auth/codex_flow.go`
- Test: `internal/auth/codex_flow_test.go`

**Step 1: Failing test** — assert form body: `grant_type=authorization_code`, `code=ac`, `redirect_uri=https://auth.openai.com/deviceauth/callback`, `client_id=cid`, `code_verifier=cv`; response `{"access_token":"at","refresh_token":"rt","expires_in":3600}` → TokenResult fields; missing access_token → error.

**Step 2:** FAIL. **Step 3:** Form POST (mirror `RefreshTokenRequest` encoding), redirect URI constant `https://auth.openai.com/deviceauth/callback`. **Step 4:** PASS.

### Task 4: Registry entry + CLI case

**Objective:** Register `openai-codex`; replace the placeholder `case FlowDeviceCodex:` in `runOAuthConnect`.

**Files:**
- Modify: `internal/auth/providers.go` (map entry only, Contract 2 verbatim)
- Modify: `cmd/meept/oauth.go` (case body: print VerifyURL + code, call the three functions, `store.Save`)
- Test: `internal/auth/providers_codex_test.go`

**Step 1: Failing test** — `ResolveProviderConfig("openai-codex")` returns exact ClientIDDefault/endpoint values/`Flow: FlowDeviceCodex`/`BaseURL: "https://chatgpt.com/backend-api/codex"`/`Transport: llm.TransportCodexResponses`.

**Step 2:** FAIL. **Step 3:** Add entry; implement CLI case mirroring the default flow's UX:

```go
case FlowDeviceCodex:
    dcr, err := auth.StartCodexDeviceFlow(ctx, providerCfg.DeviceUserCodeEP, flowCfg.ClientID)
    if err != nil { return fmt.Errorf("device code request failed: %w", err) }
    fmt.Printf("  visit: %s\n", dcr.VerifyURL)
    fmt.Printf("  enter code: %s\n\n", dcr.UserCode)
    fmt.Print("  waiting for authorization...")
    grant, err := auth.PollCodexAuthorization(ctx, providerCfg.DevicePollEP, dcr.DeviceAuthID, dcr.UserCode, dcr.Interval)
    if err != nil { /* ctx cancelled check like default case */ }
    token, err := auth.ExchangeCodexToken(ctx, flowCfg.TokenEP, flowCfg.ClientID, grant)
    // save like default case; "openai-codex connected."
```

Also add `openai-codex` to the help provider list (next to `xai-oauth`). **Step 4:** `go build ./... && go test ./internal/auth/ -count=1`.

## Self-Verification Checklist

- [ ] All 4 tasks done; `go test ./internal/auth/ -count=1` + `go build ./...` green
- [ ] Constants verbatim: client ID, both deviceauth endpoints, token URL, redirect URI, verify URL
- [ ] Only this branch's map entry + CLI case touched
- [ ] TDD followed (tests exist for retry, pending-poll, deadline, exchange shapes)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] All tests present and passing; no skipped tests
- [ ] 429 path: honors Retry-After; 4-attempt cap; error actionable
- [ ] Poll: 403/404 pending; non-200-non-403/404 fatal; deadline + ctx cancel
- [ ] Exchange: exact form fields incl. redirect_uri + code_verifier
- [ ] Registry entry byte-matches Contract 2
- [ ] CLI case prints lowercase UI text; saves via store; cancel handled
- [ ] No token values in any error string or log call
- [ ] Conventions: stdlib only, `%w`, gofmt, no debug artifacts, no line-number corruption

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Poll interval floor is 3s (hermes `max(3, int(interval))`); tests inject tiny intervals via the parameter — do NOT hardcode 3s inside the loop beyond the floor at Start.
- The usercode response's `interval` arrives as a STRING ("5") in hermes; decode defensively (json.Number / try string-then-number).
- Do not implement the Responses chatter here — leaf 02 owns `internal/llm`.
