# xAI / SuperGrok OAuth Device Flow - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 2 leaves
- **Scope:** Add `xai-oauth` provider — RFC 8628 device login against auth.x.ai with OIDC-discovered token endpoint, chat via api.x.ai/v1.

## Goal

SuperGrok / X Premium+ subscribers can log in with `meept config oauth connect xai-oauth`. The flow requests a device code at `https://auth.x.ai/oauth2/device/code` (form-encoded, unlike the JSON body the existing `StartDeviceFlow` sends), shows the verification URI, and polls a token endpoint whose URL is resolved at connect time via OIDC discovery (`https://auth.x.ai/.well-known/openid-configuration` → `token_endpoint`). Refresh tokens are stored and refreshed by the existing `RefreshManager`.

The chat side needs no new transport: `api.x.ai/v1` is OpenAI-compatible, bearer-auth; the existing `internal/llm/client.go` already resolves OAuth tokens and sets `Authorization: Bearer`.

## Architecture

Two leaves: (1) the shared foundation — `FlowKind` type, new `OAuthProviderConfig` fields, and the OIDC discovery resolver — landed first because branches 02 and 03 depend on the `FlowKind` type existing; (2) the `xai-oauth` registry entry + CLI wiring + refresh encoding notes. Deviations from the standard flow are form-encoding on the device request and discovery-resolved token endpoint; both are handled by extending `DeviceFlowConfig` with an optional `FormEncoded bool` and `TokenEPOverride` resolved at flow start.

## Interface Contracts

### Contract 1: FlowKind + OAuthProviderConfig fields

From master Contract 1 — this branch's leaf 01 owns the edit:

```go
// internal/auth/providers.go
type FlowKind string
const (
    FlowDeviceRFC8628 FlowKind = "device_rfc8628"
    FlowDeviceCodex   FlowKind = "device_codex"
    FlowPKCEPaste     FlowKind = "pkce_paste"
)
// OAuthProviderConfig gains: Flow FlowKind, DeviceUserCodeEP, DevicePollEP,
// AuthorizeURL, VerifyHint, DiscoveryURL string, RefreshJSON bool
```

### Contract 2: OIDC discovery

```go
// internal/auth/discovery.go — owner: leaf 01
// ResolveTokenEndpoint fetches <discoveryURL> and returns the "token_endpoint" member.
func ResolveTokenEndpoint(ctx context.Context, discoveryURL string) (string, error)
```

### Contract 3: DeviceFlowConfig extensions

```go
// internal/auth/device_flow.go — owner: leaf 01
// DeviceFlowConfig gains:
type DeviceFlowConfig struct {
    ClientID     string
    ClientSecret string
    DeviceEP     string
    TokenEP      string
    Scopes       []string
    FormEncoded  bool // true: device request as application/x-www-form-urlencoded (xAI)
}
// StartDeviceFlow sends form body {"client_id", "scope"} when FormEncoded,
// JSON {"client_id","scope"} otherwise. PollForToken unchanged (already form).
```

### Contract 4: xai-oauth registry entry

```go
// internal/auth/providers.go — owner: leaf 02. ONLY this map entry; no other edits.
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
```

Poll grant type: `urn:ietf:params:oauth:grant-type:device_code` (matches existing `PollForToken` — verify; if the existing poll sends `grant_type=urn:ietf:params:oauth:grant-type:device_code&client_id&device_code` it needs no change; hermes auth.py:7673-7680 uses exactly that shape).

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-registry-extension.md | leaf | none | 45K | WAVE 1 (gate) |
| 02 | 02-xai-provider-entry.md | leaf | 01 | 40K | WAVE 2 group A |

## Dispatch Protocol

### Phase 1: Leaf 01 (gate — must complete before ANY Wave 2 leaf in any branch)

1. **Read** `01-registry-extension.md`, dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-registry-extension.md"
   - Context: full leaf text + Contracts 1-3 above + coding conventions from master + current `providers.go` + `device_flow.go` request-building code INLINED
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files — explore with search_files or terminal cat."
   - Include: "After writing a file, do NOT read it back to verify. Write once and stop."

### Phase 2: Leaf 02

2. After leaf 01 review passes, dispatch `02-xai-provider-entry.md` similarly, with Contracts 3-4 + the CLI dispatch snippet from master Contract 5 inlined.

### Phase 3: Review and Commit Each Child

After each implementation agent returns, review in-session (main model, NOT a delegated subagent):

1. Read changed files; check against leaf spec + contracts above; run `go build ./internal/auth/ && go test ./internal/auth/ -count=1`.
2. Gaps → re-dispatch with specific feedback (max 3 cycles).
3. Pass → `git add <exact paths> && git commit -m "feat(oauth): [leaf name]"`; tracking table → REVIEWED.

### Phase 4: Integration Review

Both children REVIEWED:

1. `go build ./... && go test ./internal/auth/ -count=1`
2. Verify `ResolveProviderConfig("xai-oauth")` resolves; `Flow` dispatch in `runOAuthConnect` covers `FlowDeviceRFC8628` path with form encoding.
3. `gofmt -l internal/auth` empty; no line-number corruption in changed files.
4. Report branch COMPLETE to master.

## Review Checklist

- [ ] Leaf 01: `FlowKind` constants exact; struct fields added without touching existing entries; discovery + form-encoding tested via httptest
- [ ] Leaf 02: registry entry matches Contract 4 verbatim; CLI help lists `xai-oauth`; no other registry entries modified
- [ ] Tokens never logged
- [ ] Tests passing, TDD followed
- [ ] No debug artifacts, no TODOs, no placeholders
- [ ] No line-number corruption

Output: APPROVED or list of specific gaps.

## Coding Conventions

Inherited from master `## Coding Conventions`. Key repeats: stdlib only; `%w` error wrapping; two-value type assertions; `crypto/rand` for any nonce; gofmt before reporting; table-driven stdlib tests.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-registry-extension | PENDING | 0 | |
| 02-xai-provider-entry | PENDING | 0 | |

## Integration Test Plan

1. `go test ./internal/auth/ -run 'TestDiscovery|TestStartDeviceFlow_FormEncoded|TestXai' -v` — discovery parses a served `.well-known/openid-configuration`, form-encoded device request has correct body, xai-oauth config resolves with expected endpoints.
2. `go build ./cmd/meept && ./bin/meept config oauth connect 2>&1 | grep xai-oauth` lists the provider (compile + help text only; live login is optional manual smoke).
3. Registry integrity: existing `github-models`/`google-*` entries byte-identical (diff the map literal regions).

## Open Questions

- See ../../master.md Open Questions #2 (FormEncoded derivation owns this branch's leaf 02 Task 1).

## Notes

- Leaf 01 is the root-wave gate: branches 02 and 03 import `FlowKind`. Land it first, commit, then fan out.
- xAI tokens ~6h expiry; existing 10m refresh margin suffices.
- The poll endpoint 403/404-vs-200 semantics differ per provider; hermes treats non-200-with-`authorization_pending` as continue. Our existing `PollForToken` handles RFC 8628 errors; confirm it maps `authorization_pending`/`slow_down` correctly for form responses (it does — tokenResponse has Error/ErrorDesc fields).
