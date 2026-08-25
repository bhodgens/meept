# ChatGPT / Codex OAuth + Responses Transport - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 2 leaves
- **Scope:** Add `openai-codex` provider — OpenAI's non-standard device flow ("Sign in with ChatGPT") plus a Codex Responses chatter against `chatgpt.com/backend-api/codex`.

## Goal

ChatGPT Plus/Pro subscribers authenticate with `meept config oauth connect openai-codex`. The flow differs from RFC 8628: POST JSON `{client_id}` to `auth.openai.com/api/accounts/deviceauth/usercode` → user visits `auth.openai.com/codex/device` and enters the code → poll `.../deviceauth/token` (403/404 = pending, 200 = grant containing `authorization_code` + `code_verifier`) → exchange at `auth.openai.com/oauth/token` with `authorization_code` grant, PKCE verifier, and `redirect_uri=https://auth.openai.com/deviceauth/callback`. Tokens refresh via standard `refresh_token` form grant at the same token URL.

Inference goes to the Codex Responses API (`POST {base}/responses`) — a new chatter (`TransportCodexResponses`, constant already defined at `internal/llm/provider_registry.go:9`, currently unimplemented). Cloudflare in front of the backend whitelists first-party originators; requests must pin `originator: codex_cli_rs` + codex-shaped User-Agent + `ChatGPT-Account-ID` extracted from the access-token JWT.

## Architecture

Leaf 01 builds the flow (pure `internal/auth`, httptest-simulated endpoints, 429 retry with Retry-After). Leaf 02 builds the chatter (pure `internal/llm`, Responses wire format, Cloudflare headers, JWT claim extraction) and the `createChatterFor` dispatch. The two are independent file sets; leaf 02 needs leaf 01 only for the registry entry existing so `createChatterFor` dispatch compiles against a resolvable provider — actually the chatter dispatch keys off `cfg.Transport`/`ProviderID`, so leaves are file-independent; sequencing is WAVE 2 (leaf 01) then WAVE 3 (leaf 02) per master.

## Interface Contracts

### Contract 1: Codex flow functions

```go
// internal/auth/codex_flow.go — owner: leaf 01
type CodexDeviceResult struct {
    UserCode     string
    DeviceAuthID string
    VerifyURL    string        // always https://auth.openai.com/codex/device
    Interval     time.Duration // max(3, server interval)
}
type CodexAuthGrant struct {
    AuthorizationCode string
    CodeVerifier      string
}

func StartCodexDeviceFlow(ctx context.Context, userCodeEP, clientID string) (*CodexDeviceResult, error)
    // POST JSON {"client_id": clientID}; 429 -> retry up to 4 attempts honoring
    // Retry-After (exponential 2s,4s,8s fallback, capped 60s); response fields:
    // user_code, device_auth_id, interval.

func PollCodexAuthorization(ctx context.Context, pollEP, deviceAuthID, userCode string, interval time.Duration) (*CodexAuthGrant, error)
    // Poll POST JSON {"device_auth_id", "user_code"}; 403/404 = keep polling;
    // 200 = {"authorization_code", "code_verifier"}; 15min deadline; ctx cancel honored.

func ExchangeCodexToken(ctx context.Context, tokenEP, clientID string, grant *CodexAuthGrant) (*TokenResult, error)
    // POST form: grant_type=authorization_code, code, redirect_uri=<issuer>/deviceauth/callback,
    // client_id, code_verifier. Response: access_token, refresh_token, expires_in.
```

### Contract 2: Registry entry

```go
// internal/auth/providers.go — owner: leaf 01. ONLY this map entry.
"openai-codex": {
    ClientIDDefault: "app_EMoamEEZ73f0CkXaXp7hrann",
    DeviceUserCodeEP: "https://auth.openai.com/api/accounts/deviceauth/usercode",
    DevicePollEP:     "https://auth.openai.com/api/accounts/deviceauth/token",
    TokenEP:          "https://auth.openai.com/oauth/token",
    ProviderID:       "openai-codex",
    Flow:             FlowDeviceCodex,
    Transport:        llm.TransportCodexResponses,
    BaseURL:          "https://chatgpt.com/backend-api/codex",
},
```

### Contract 3: Codex Responses chatter

```go
// internal/llm/codex.go — owner: leaf 02
func NewCodexClient(cfg *ModelConfig, opts ...CodexClientOption) *CodexClient
// options mirror Client: WithCodexLogger, WithCodexBudget, WithCodexTimeout,
// WithCodexTokenResolver(tr, provider)
// var _ Chatter = (*CodexClient)(nil)

// Cloudflare header table (frozen; hermes auxiliary_client.py:780-812):
//   User-Agent: codex_cli_rs/0.0.0 (Hermes meept)
//   originator: codex_cli_rs
//   ChatGPT-Account-ID: <jwt claim https://api.openai.com/auth . chatgpt_account_id>
//     (base64url decode payload segment; on ANY parse failure omit the header)

// createChatterFor (provider_manager.go:115) dispatch:
//   cfg.ProviderID == "openai-codex" || cfg.Transport == TransportCodexResponses
//     -> NewCodexClient(cfg, ...) with token resolver + budget + timeout options
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-codex-flow.md | leaf | master-wave-1 gate (FlowKind) | 55K | WAVE 2 group A |
| 02 | 02-codex-transport.md | leaf | 01 (registry entry for wiring) | 70K | WAVE 3 group B |

## Dispatch Protocol

### Phase 1: Leaf 01

1. **Read** `01-codex-flow.md`, dispatch via `delegate_task` with full leaf text + Contracts 1-2 + coding conventions + `device_flow.go` request/poll code INLINED + hermes `_codex_device_code_login` (auth.py:7797-7960) excerpt INLINED.
   - "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - "Do NOT use read_file on existing source files — explore with search_files or terminal cat."
   - "After writing a file, do NOT read it back to verify. Write once and stop."

### Phase 2: Leaf 02

2. After leaf 01 review passes, dispatch `02-codex-transport.md` with Contracts 3 + the `createChatterFor` function body, `client.go` token-resolution block (lines ~755-780), and `Chatter` interface INLINED. Same do-not-commit rules.

### Phase 3: Review and Commit Each Child

Main model reviews in-session: read changed files, contracts check, `go build ./internal/... && go test ./internal/auth/ -count=1` (leaf 01) or `./internal/llm/` (leaf 02). Gaps → re-dispatch (max 3). Pass → commit exact paths, tracking → REVIEWED.

### Phase 4: Integration Review

1. `go build ./... && go test ./internal/auth/ ./internal/llm/ -count=1`
2. `resolveProviderConfig("openai-codex")` → `Flow: FlowDeviceCodex`; `createChatterFor` returns `*CodexClient` for it.
3. gofmt clean; no line-number corruption.
4. Report COMPLETE to master.

## Review Checklist

- [ ] Frozen constants verbatim (client ID, endpoints, redirect URI, header table)
- [ ] 429 retry honors Retry-After, capped, max 4 attempts
- [ ] Poll treats 403/404 as pending; 15-minute deadline; ctx cancellation honored
- [ ] Chatter maps Responses output items to ChatResponse; tool calls surfaced
- [ ] JWT account-ID extraction fails soft (header omitted, no error)
- [ ] Tokens never logged; no debug artifacts; TDD; conventions per master

Output: APPROVED or list of specific gaps.

## Coding Conventions

Per master `## Coding Conventions`. Additions: no new deps (stdlib JWT parse = split + base64url + json.Unmarshal); reuse `deviceFlowHTTPClient` style; table-driven tests with httptest.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-codex-flow | PENDING | 0 | |
| 02-codex-transport | PENDING | 0 | |

## Integration Test Plan

1. `go test ./internal/auth/ -run TestCodex -v` — flow tests against httptest-simulated usercode/poll/token endpoints, including 429-retry and pending-then-grant sequences.
2. `go test ./internal/llm/ -run TestCodex -v` — chatter request shape (model, input, instructions, stream:false, store:false), header table, response parsing (message/reasoning/function_call), error mapping.
3. `go build ./cmd/meept && ./bin/meept config oauth connect 2>&1 | grep openai-codex` lists provider.
4. Cross-check: `internal/daemon/components.go registerOAuthProviders` needs no edit — `openai-codex` has BaseURL so it auto-registers with `OAuthProvider` set; `createChatterFor` handles transport.

## Open Questions

- See ../../master.md Open Questions #1 (Codex streaming scope — decided by leaf 02 Task 5 at implementation time).

## Notes

- The poll endpoint returns 200 ONLY on completion; hermes treats other 2xx/4xx as fatal except 403/404 pending (auth.py:7882-7905). Mirror that.
- `store: false` on Responses requests — Codex backend rejects `store: true` for CLI clients.
- Reasoning items: map `type: "reasoning"` outputs to thinking/empty content; do not drop silently — include as reasoning text if present.
- Do NOT touch `TransportCodexResponses` constant (already exists) or `provider_registry.go` beyond (optionally) adding the provider def entry — the registry `CanonicalProviders` entry for `openai-codex` (AuthType: AuthOAuthDevice, BaseURL per Contract 2) is leaf 02's to add so `ListProviders`-based UIs see it.
