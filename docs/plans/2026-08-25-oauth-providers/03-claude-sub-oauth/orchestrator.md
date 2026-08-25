# Claude Pro/Max Subscription OAuth - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 2 leaves
- **Scope:** Add `anthropic-sub` provider — Claude PKCE paste flow (browser authorize → paste code) plus Bearer-auth mode on the existing Anthropic Messages transport.

## Goal

Claude Pro/Max subscribers authenticate with `meept config oauth connect anthropic-sub`. The flow: generate PKCE (S256) + state, print `https://claude.ai/oauth/authorize?...` (browser opens if graphical), user authorizes and pastes the displayed code into the terminal, meept exchanges code+verifier at `https://platform.claude.com/v1/oauth/token` (fallback `console.anthropic.com/v1/oauth/token`), stores tokens. Refresh uses the same endpoints with `grant_type=refresh_token` — form-encoded by default, JSON accepted; the token endpoint REQUIRES a User-Agent not starting with `claude-code/` (429 otherwise; use `axios/1.7.9`).

Inference: existing `AnthropicClient` (`internal/llm/anthropic.go`) against `https://api.anthropic.com` with three changes when OAuth is active: `Authorization: Bearer <token>` instead of `x-api-key`, `anthropic-beta: oauth-2025-04-20` header added, token resolved per-request via TokenResolver. Subscription model IDs are the verbatim catalog names (`claude-sonnet-4-5` etc.) — already in `models_catalog.go` under provider `anthropic`.

## Architecture

Leaf 01: `internal/auth/pkce.go` (PKCE helpers + exchange) + `RefreshJSON` refresh path in `refresh.go`/`token_store.go` + registry entry + CLI `FlowPKCEPaste` case (reads pasted code from stdin). Leaf 02: `internal/llm/anthropic.go` Bearer mode + `createChatterFor` wiring (Anthropic branch gains TokenResolver support) + `CanonicalProviders` entry. Independent file sets; leaf 02 needs leaf 01's registry entry only for end-to-end, not compilation.

## Interface Contracts

### Contract 1: PKCE + exchange functions

```go
// internal/auth/pkce.go — owner: leaf 01
// GeneratePKCE returns a random 32-byte urlsafe verifier and its S256 challenge.
func GeneratePKCE() (verifier, challenge string)  // crypto/rand, NOT math/rand (predid)
// GenerateState() string  // crypto/rand token_urlsafe-equivalent, 32 chars

func BuildAnthropicAuthorizeURL(clientID, challenge, state string) string
    // https://claude.ai/oauth/authorize?code=true&client_id=..&response_type=code
    //   &redirect_uri=https%3A%2F%2Fconsole.anthropic.com%2Foauth%2Fcode%2Fcallback
    //   &scope=org%3Acreate_api_key+user%3Aprofile+user%3Ainference
    //   &code_challenge=..&code_challenge_method=S256&state=..

func ExchangeAnthropicCode(ctx context.Context, clientID, code, verifier string) (*TokenResult, error)
    // POST form to token URLs [platform.claude.com, console.anthropic.com] in order;
    // first success wins; both fail -> combined error.
    // Fields: grant_type=authorization_code, code, redirect_uri, client_id, code_verifier.
    // Headers: Content-Type form, User-Agent axios/1.7.9, Accept application/json.

func RefreshAnthropicToken(ctx context.Context, clientID, refreshToken string) (*TokenResult, error)
    // POST form (RefreshJSON config selects JSON body when true):
    //   form: grant_type=refresh_token&refresh_token=..&client_id=..
    //   json: {"grant_type":"refresh_token","refresh_token":"..","client_id":".."}
    // Same endpoint list + UA rule. Response: access_token, refresh_token?, expires_in.
```

### Contract 2: Registry entry

```go
// internal/auth/providers.go — owner: leaf 01. ONLY this map entry.
"anthropic-sub": {
    ClientIDDefault: "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
    AuthorizeURL:    "https://claude.ai/oauth/authorize",
    TokenEP:         "https://platform.claude.com/v1/oauth/token",
    Scopes:          []string{"org:create_api_key", "user:profile", "user:inference"},
    ProviderID:      "anthropic-sub",
    Flow:            FlowPKCEPaste,
    RefreshJSON:     false,
    Transport:       llm.TransportAnthropicMessages,
    BaseURL:         "https://api.anthropic.com",
    VerifyHint:      "authorize and paste the code shown",
},
```

Refresh fallback host: `RefreshAnthropicToken` internally tries `console.anthropic.com/v1/oauth/token` after the primary — do not encode both in the registry entry.

### Contract 3: Anthropic Bearer mode

```go
// internal/llm/anthropic.go — owner: leaf 02
// AnthropicClient gains: tokenResolver TokenResolver + oauthProvider string
// (option WithAnthropicTokenResolver(tr TokenResolver, provider string)).
// In BOTH request paths (non-stream ~line 890-905, stream ~line 1050-1060):
//   if tokenResolver != nil && oauthProvider != "":
//     token := resolve;  httpReq.Header.Set("Authorization", "Bearer "+token)
//     httpReq.Header.Set("anthropic-beta", "oauth-2025-04-20")
//     (x-api-key NOT set — even if config.APIKey non-empty)
//   else: existing x-api-key path unchanged.
// Keep anthropic-version in both paths. Bedrock guard (skips x-api-key) stays.

// createChatterFor (provider_manager.go:115): the isAnthropic branch gains
//   if tr != nil && cfg.OAuthProvider != "" {
//       opts = append(opts, WithAnthropicTokenResolver(tr, cfg.OAuthProvider))
//   }
```

### Contract 4: CanonicalProviders entry

```go
// internal/llm/provider_registry.go — owner: leaf 02
{
    ID:        "anthropic-sub",
    Name:      "Claude (Pro/Max subscription)",
    Transport: TransportAnthropicMessages,
    AuthType:  AuthOAuthDevice,
    BaseURL:   "https://api.anthropic.com",
    DocURL:    "https://docs.anthropic.com",
    Supports:  []string{CapStreaming, CapTools, CapImages, CapThinking},
},
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-anthropic-flow.md | leaf | master-wave-1 gate (FlowKind, RefreshJSON, AuthorizeURL fields) | 55K | WAVE 2 group A |
| 02 | 02-anthropic-bearer.md | leaf | 01 (registry entry) | 55K | WAVE 3 group B |

## Dispatch Protocol

### Phase 1: Leaf 01

1. **Read** `01-anthropic-flow.md`, dispatch via `delegate_task` with full leaf text + Contracts 1-2 + conventions + `device_flow.go` refresh/exchange patterns INLINED + hermes `anthropic_adapter.py` excerpts (constants 1443-1464, `refresh_anthropic_oauth_pure` 1083-1140, `run_hermes_oauth_login_pure` 1468-1530) INLINED.
   - "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - "Do NOT use read_file on existing source files — explore with search_files or terminal cat."
   - "After writing a file, do NOT read it back to verify. Write once and stop."

### Phase 2: Leaf 02

2. After leaf 01 review passes, dispatch `02-anthropic-bearer.md` with Contracts 3-4 + both anthropic.go request-path blocks + createChatterFor body INLINED. Same rules.

### Phase 3: Review and Commit Each Child

Main model reviews in-session: changed files vs contracts, `go build ./... && go test ./internal/auth/ -count=1` (leaf 01) / `./internal/llm/` (leaf 02). Gaps → re-dispatch (max 3). Pass → commit exact paths; tracking → REVIEWED.

### Phase 4: Integration Review

1. `go build ./... && go test ./internal/auth/ ./internal/llm/ -count=1`
2. `ResolveProviderConfig("anthropic-sub")` → Flow PKCE + correct client ID; `createChatterFor` for `anthropic-sub` ModelConfig returns `*AnthropicClient` with resolver set.
3. gofmt clean; no line-number corruption. Report COMPLETE to master.

## Review Checklist

- [ ] Client ID `9d1c250a-e61b-44d9-88ed-5944d1962f5e` verbatim; scope string exact
- [ ] PKCE uses crypto/rand; challenge is S256 base64url no padding
- [ ] Token endpoint UA `axios/1.7.9` on ALL token-endpoint calls; never on messages calls
- [ ] Fallback endpoint tried on failure; combined error when both fail
- [ ] Bearer mode suppresses x-api-key entirely; beta header present; version kept
- [ ] Stdin paste read has timeout + trim + non-empty validation
- [ ] Tokens never logged; no debug artifacts; TDD; conventions per master

Output: APPROVED or list of specific gaps.

## Coding Conventions

Per master `## Coding Conventions`. Additions: `crypto/rand` for verifier/state (predid analyzer — never math/rand); no new deps; stdin via `bufio.Scanner` on `os.Stdin`.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-anthropic-flow | PENDING | 0 | |
| 02-anthropic-bearer | PENDING | 0 | |

## Integration Test Plan

1. `go test ./internal/auth/ -run TestAnthropic -v` — PKCE shape, authorize URL params (exact query string), exchange happy/fallback/both-fail, refresh form vs JSON bodies, UA header asserted.
2. `go test ./internal/llm/ -run TestAnthropicBearer -v` — Bearer + beta headers set, x-api-key absent, resolver invoked, api-key path regression (no resolver → x-api-key set).
3. `go build ./cmd/meept && ./bin/meept config oauth connect 2>&1 | grep anthropic-sub` lists provider.
4. `go test ./internal/llm/ -count=1` full (no anthropic regressions).

## Open Questions

- See ../../master.md Open Questions #3 (live-login smoke optional). RefreshAnthropicToken signature gained a `jsonBody bool` param beyond the parent contract — accepted in leaf 01 as documented deviation.

## Notes

- The paste flow is interactive; automated tests cover exchange + URL building, NOT the stdin read (unit-test a `readPastedCode(io.Reader)` helper with a strings.Reader instead).
- `models_catalog.go` already lists anthropic models under `anthropic` provider ID; `anthropic-sub` ModelConfig created by `registerOAuthProviders` uses provider-ID-as-model-ID placeholder like other OAuth providers — users pick real model IDs via `models.json5` overrides or the placeholder routes server-side. No catalog changes needed.
- Heredoc/README: subscription billing ≠ Console billing — errors about `anthropic_billing` should surface the hint to use API-key auth instead. Add to error path only if trivial; do not build account-type detection.
