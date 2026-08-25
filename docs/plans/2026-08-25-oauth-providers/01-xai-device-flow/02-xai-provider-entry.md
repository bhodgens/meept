# xai-oauth Provider Entry + CLI Wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Register the `xai-oauth` provider, wire form encoding + discovery into its connect path, and extend `runOAuthConnect` dispatch minimally.
- **Dependencies:** 01-registry-extension.md (FlowKind fields, FormEncoded, ResolveTokenEndpoint)
- **Estimated Context:** 40K
- **Concurrency Group:** WAVE 2 group A
- **Audit references:** none

## Goal

`meept config oauth connect xai-oauth` works end to end: registry entry resolves, device code requested form-encoded from auth.x.ai, token endpoint resolved via OIDC discovery at connect time, token polled + stored, `oauth status`/`disconnect` treat it like any provider. The daemon's `registerOAuthProviders` then auto-registers the chat endpoint (`api.x.ai/v1`, OpenAI-compatible, Bearer) with zero daemon changes.

## Context

`internal/auth/providers.go` maps provider IDs to config; leaf 01 added the flow fields. `cmd/meept/oauth.go` `runOAuthConnect` (lines ~96-163) currently calls `providerCfg.DeviceFlowConfig()` → `StartDeviceFlow` → `PollForToken` → `store.Save`. Discovery must override `flowCfg.TokenEP` before polling, since the registry has no fixed token endpoint for xAI. `internal/auth/refresh.go` `RefreshManager.refreshOne` builds `providerCfg.DeviceFlowConfig()` — it needs the same discovery override for refresh.

Key files:
- `internal/auth/providers.go` — registry (append entry only)
- `cmd/meept/oauth.go` — connect flow (add discovery override + Flow dispatch skeleton)
- `internal/auth/refresh.go` — refreshOne (add discovery override)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/auth/providers.go — the exact map entry:
"xai-oauth": {
    ClientIDDefault: "b1a00492-073a-47ea-816f-4c329264a828",
    DeviceEP:        "https://auth.x.ai/oauth2/device/code",
    DiscoveryURL:    "https://auth.x.ai/.well-known/openid-configuration",
    Scopes:          []string{"openid", "profile", "email", "offline_access", "grok-cli:access", "api:access"},
    ProviderID:      "xai-oauth",
    Flow:            FlowDeviceRFC8628,
    Transport:       llm.TransportOpenAIChat,
    BaseURL:         "https://api.x.ai/v1",
},

// cmd/meept/oauth.go — flow dispatch skeleton (this leaf adds the structure
// branches 02/03 will extend with their cases):
switch providerCfg.Flow {
case FlowDeviceCodex:   // leaf 02-codex/01 fills this
case FlowPKCEPaste:    // leaf 03-claude/01 fills this
default:               // FlowDeviceRFC8628 + "" — existing path
}
```

### What This Leaf Consumes

From leaf 01: `FlowKind`, `DiscoveryURL` field, `DeviceFlowConfig.FormEncoded`, `ResolveTokenEndpoint(ctx, url)`. Existing: `StartDeviceFlow`, `PollForToken`, `TokenStore`.

## Tasks

### Task 1: Registry entry + resolved flow config

**Objective:** Register `xai-oauth` with exact endpoints/scopes; `DeviceFlowConfig()` marks it form-encoded.

**Files:**
- Modify: `internal/auth/providers.go`
- Test: `internal/auth/providers_xai_test.go`

**Step 1: Failing test:**

```go
func TestXaiOAuthProviderEntry(t *testing.T) {
    cfg, err := ResolveProviderConfig("xai-oauth")
    if err != nil { t.Fatal(err) }
    if cfg.ClientIDDefault != "b1a00492-073a-47ea-816f-4c329264a828" { t.Errorf("client id") }
    if cfg.DeviceEP != "https://auth.x.ai/oauth2/device/code" { t.Errorf("device ep") }
    if cfg.DiscoveryURL != "https://auth.x.ai/.well-known/openid-configuration" { t.Errorf("discovery") }
    if cfg.BaseURL != "https://api.x.ai/v1" { t.Errorf("base url") }
    want := "openid profile email offline_access grok-cli:access api:access"
    if strings.Join(cfg.Scopes, " ") != want { t.Errorf("scopes: %v", cfg.Scopes) }
    fc := cfg.DeviceFlowConfig()
    if !fc.FormEncoded { t.Errorf("FormEncoded must be true for xai-oauth") }
}
```

**Step 2:** FAIL. **Step 3:** `DeviceFlowConfig()` sets `FormEncoded: c.DeviceEP != "" && c.DiscoveryURL != ""` — no; set it explicitly: add `FormEncoded` derivation only when provider uses form device requests. Simplest correct rule: `FormEncoded: c.ProviderID == "xai-oauth"`. Add the map entry verbatim from the contract. **Step 4:** PASS.

### Task 2: Discovery override in connect + refresh

**Objective:** Before polling (and before refresh), resolve the token endpoint when `DiscoveryURL` is set.

**Files:**
- Modify: `cmd/meept/oauth.go` (~10 lines inside `runOAuthConnect`, after `flowCfg := providerCfg.DeviceFlowConfig()`)
- Modify: `internal/auth/refresh.go` (`refreshOne`, same override)
- Test: `cmd/meept/oauth_xai_test.go` (unit-test a small helper, not the cobra command)

**Step 1: Failing test** for a helper `resolveFlowTokenEP(ctx, providerCfg, flowCfg) DeviceFlowConfig` exported from `internal/auth` (e.g. `func (c *OAuthProviderConfig) ResolveFlowConfig(ctx context.Context) (DeviceFlowConfig, error)` — returns `DeviceFlowConfig()` with `TokenEP` replaced by discovery result when `DiscoveryURL != ""`):

```go
// internal/auth/providers_xai_test.go
func TestResolveFlowConfig_Discovery(t *testing.T) {
    srv := httptest.NewServer(/* serves {"token_endpoint": srv.URL + "/tok"} */)
    defer srv.Close()
    c := OAuthProviderConfig{DiscoveryURL: srv.URL, DeviceEP: srv.URL + "/dev", Scopes: []string{"s"}}
    fc, err := c.ResolveFlowConfig(context.Background())
    if err != nil { t.Fatal(err) }
    if fc.TokenEP != srv.URL+"/tok" { t.Errorf("token ep %q", fc.TokenEP) }
}
func TestResolveFlowConfig_NoDiscovery(t *testing.T) {
    c := OAuthProviderConfig{TokenEP: "https://fixed/token"}
    fc, err := c.ResolveFlowConfig(context.Background())
    if err != nil || fc.TokenEP != "https://fixed/token" { t.Errorf("fixed: %v %q", err, fc.TokenEP) }
}
```

**Step 2:** FAIL. **Step 3:** Implement `ResolveFlowConfig` in `providers.go`. Use it in `runOAuthConnect` (replace the `flowCfg := providerCfg.DeviceFlowConfig()` line) and in `RefreshManager.refreshOne` (replace `flowCfg := providerCfg.DeviceFlowConfig()`). **Step 4:** PASS; `go build ./cmd/meept/`.

### Task 3: CLI help + flow dispatch skeleton

**Objective:** `oauth connect` help lists `xai-oauth`; the Flow switch skeleton exists for branches 02/03.

**Files:**
- Modify: `cmd/meept/oauth.go` (help text list + switch skeleton with `// filled by openai-codex / anthropic-sub leaves` comments)
- Test: none beyond build (help text is data)

**Step 1:** N/A (structural). **Step 2:** build fails before switch added only if written first — skip. **Step 3:** Add `xai-oauth` to the provider list in the `Long` help; wrap the existing flow body in `default:` of a `switch providerCfg.Flow` with empty `case FlowDeviceCodex:` / `case FlowPKCEPaste:` returning `fmt.Errorf("provider %s: flow not yet implemented", providerID)` placeholders (branches 02/03 replace those two lines each). **Step 4:** `go build ./... && go vet ./cmd/meept/`.

### Task 4: config UI pickup (verify, no code)

**Objective:** Confirm `internal/configui/sections_oauth.go` iterates `RegisteredProviders()` so the new provider appears automatically.

Run: `grep -n "RegisteredProviders" internal/configui/sections_oauth.go`. If present → no change. If it hardcodes provider IDs → add `xai-oauth` to that list (and note the deviation).

## Self-Verification Checklist

- [ ] Tasks 1-3 done; `go build ./... && go test ./internal/auth/ ./cmd/... -count=1` green
- [ ] Registry entry byte-matches Contract (parent) — client ID, endpoints, scopes, BaseURL
- [ ] Only the `xai-oauth` map entry added to providers.go; existing entries untouched
- [ ] No deviations undocumented

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Client ID `b1a00492-073a-47ea-816f-4c329264a828` verbatim
- [ ] Scopes exact six-element list
- [ ] Discovery override applied in BOTH connect and refreshOne paths
- [ ] Switch skeleton present with error placeholders (not silent fallthrough)
- [ ] `github-models`/`google-*` entries byte-identical (diff map region)
- [ ] No token values in logs or errors; conventions per master

Output: APPROVED or list of specific gaps.

## Notes

- Form encoding on the device request is required by auth.x.ai (JSON body gets 400/unsupported). Verified against hermes `auth.py:7612-7648`.
- The token poll response from xAI includes `id_token` etc.; `tokenResponse` ignores unknown fields — fine.
- If `ResolveFlowConfig` naming collides, `FlowConfigFor` is the fallback name — keep it consistent in both call sites and note the deviation.
