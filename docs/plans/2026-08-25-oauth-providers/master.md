# OAuth Provider Expansion - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 3 branches (8 leaves total)
- **Scope:** Add xAI/Grok device login, ChatGPT/Codex device login + Responses transport, and Claude Pro/Max subscription OAuth to meept's existing OAuth provider system.

## Goal

meept today supports three OAuth providers: `github-models`, `google-oauth`, and `google-calendar` (registry: `internal/auth/providers.go`, CLI: `meept config oauth connect <provider>`). The user's list names five targets; three are missing from meept:

1. **ChatGPT / Codex** — "Sign in with ChatGPT" (Plus/Pro subscription billing, not platform API). Non-standard device flow against `auth.openai.com` + Codex Responses transport against `chatgpt.com/backend-api/codex`. Hermes reference implementation: `~/git/hermes-agent/hermes_cli/auth.py` (`_codex_device_code_login`, lines 7797-7960) and `agent/auxiliary_client.py` (`_codex_cloudflare_headers`, line 780).
2. **Claude Code / Max** — Anthropic CLI OAuth (subscription billing, not Console). PKCE browser flow against `claude.ai/oauth/authorize` with paste-back code, token exchange at `platform.claude.com/v1/oauth/token`, Bearer auth + beta headers on the Messages API. Hermes reference: `~/git/hermes-agent/agent/anthropic_adapter.py` (`run_hermes_oauth_login_pure` ~line 1468, `refresh_anthropic_oauth_pure` line 1083, constants at lines 1443-1464).
3. **SuperGrok / X Premium+** — `accounts.x.ai` device login. Near-standard RFC 8628 flow against `auth.x.ai/oauth2/device/code` with OIDC discovery for the token endpoint, scopes `openid profile email offline_access grok-cli:access api:access`. Hermes reference: `~/git/hermes-agent/hermes_cli/auth.py` lines 7612-7730 (`_xai_oauth_request_device_code`, `_xai_oauth_poll_device_token`) and `_xai_oauth_discovery` (line 4639).

Already covered (no work): GitHub Models (`github-models` exists; placeholder client ID pending app registration), Google Gemini user OAuth (`google-oauth` exists), Google Calendar (`google-calendar` exists).

## Architecture

All providers extend one subsystem: `internal/auth` (device flows, token store, refresh) → `internal/llm` (chatter selection, token resolution) → `internal/daemon/components.go` (`registerOAuthProviders` dynamically registers OAuth-backed providers) → `cmd/meept/oauth.go` (CLI). Three flow archetypes:

- **Standard device flow** (existing): `StartDeviceFlow` → `PollForToken` → store. xAI fits this with two deviations: form-encoded device request (not JSON body) and token endpoint resolved via OIDC discovery rather than fixed.
- **Non-standard device flow** (Codex): POST JSON to `auth.openai.com/api/accounts/deviceauth/usercode`, poll a custom token-poll endpoint that returns an authorization code + PKCE verifier, then exchange at `auth.openai.com/oauth/token` with `authorization_code` grant. Custom flow implementation needed.
- **PKCE authorization-code paste flow** (Claude): build authorize URL with S256 challenge, user opens browser and pastes back a code, exchange at `platform.claude.com/v1/oauth/token`. New flow type in `internal/auth`.

Transport integration: Codex uses the Codex Responses API (`chatgpt.com/backend-api/codex`) — a new `codex_responses` chatter (constant `TransportCodexResponses` already exists in `provider_registry.go:9` but has no implementation). Claude OAuth uses the existing Anthropic Messages transport (`internal/llm/anthropic.go`) with Bearer auth + `anthropic-beta: oauth-2025-04-20` header instead of `x-api-key`. xAI uses the standard OpenAI-compatible transport with Bearer token.

## Interface Contracts

### Contract 1: OAuthProviderConfig extensions (internal/auth/providers.go)

```go
// FlowKind selects the login flow driver for a provider.
type FlowKind string

const (
    FlowDeviceRFC8628 FlowKind = "device_rfc8628" // existing StartDeviceFlow/PollForToken
    FlowDeviceCodex   FlowKind = "device_codex"   // OpenAI deviceauth usercode flow
    FlowPKCEPaste     FlowKind = "pkce_paste"     // Claude browser authorize + paste code
)

// Added fields to OAuthProviderConfig:
type OAuthProviderConfig struct {
    // ... existing fields ...
    Flow FlowKind        // defaults to FlowDeviceRFC8628 when empty
    // Codex: endpoints for the non-standard device flow
    DeviceUserCodeEP string // POST https://auth.openai.com/api/accounts/deviceauth/usercode
    DevicePollEP     string // POST https://auth.openai.com/api/accounts/deviceauth/token
    // Claude PKCE: authorize URL and verification hint
    AuthorizeURL string   // https://claude.ai/oauth/authorize
    VerifyHint   string   // text shown to the user ("authorize and paste the code shown")
    // xAI: OIDC discovery URL for token endpoint resolution
    DiscoveryURL string   // https://auth.x.ai/.well-known/openid-configuration
    // Per-provider token refresh request encoding override
    RefreshJSON bool      // true: send refresh as JSON body (Anthropic platform.claude.com)
}

// Provider IDs (new registry entries):
//   "xai-oauth"          — SuperGrok chat via api.x.ai/v1
//   "openai-codex"       — ChatGPT Plus/Pro via chatgpt.com/backend-api/codex
//   "anthropic-sub"      — Claude Pro/Max via api.anthropic.com

// Owner: 01-xai-device-flow/01-registry-extension.md (type + fields)
//        02-chatgpt-codex/01-codex-flow.md (codex entry)
//        03-claude-sub-oauth/01-anthropic-flow.md (anthropic-sub entry)
// Consumers: all leaves, cmd/meept/oauth.go, internal/daemon/components.go
```

Frozen constants (verified against Hermes source):

```go
// xAI (hermes_cli/auth.py:106-114)
const xaiClientID = "b1a00492-073a-47ea-816f-4c329264a828"
const xaiScope = "openid profile email offline_access grok-cli:access api:access"
// Token EP via discovery: https://auth.x.ai/.well-known/openid-configuration -> token_endpoint

// Codex (hermes_cli/auth.py:86,102-103)
const codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
const codexTokenURL = "https://auth.openai.com/oauth/token"
// device usercode EP: https://auth.openai.com/api/accounts/deviceauth/usercode
// device poll EP: https://auth.openai.com/api/accounts/deviceauth/token
// verify URL shown to user: https://auth.openai.com/codex/device

// Anthropic (agent/anthropic_adapter.py:1443-1464)
const anthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
var anthropicTokenURLs = []string{
    "https://platform.claude.com/v1/oauth/token",
    "https://console.anthropic.com/v1/oauth/token", // fallback; primary 404s since migration
}
const anthropicScope = "org:create_api_key user:profile user:inference"
const anthropicRedirectURI = "https://console.anthropic.com/oauth/code/callback"
// Token endpoint requires User-Agent NOT starting with "claude-code/"
// (rate-limited 429); use "axios/1.7.9". Inference DOES use claude-code UA.
```

### Contract 2: Flow driver interfaces (internal/auth)

```go
// internal/auth/codex_flow.go — owner: 02-chatgpt-codex/01-codex-flow.md
func StartCodexDeviceFlow(ctx context.Context, clientID string) (*CodexDeviceResult, error)
func PollCodexAuthorization(ctx context.Context, clientID, deviceAuthID, userCode string) (*CodexAuthGrant, error)
func ExchangeCodexToken(ctx context.Context, clientID string, grant *CodexAuthGrant) (*TokenResult, error)

type CodexDeviceResult struct {
    UserCode     string
    DeviceAuthID string
    VerifyURL    string // https://auth.openai.com/codex/device
    Interval     time.Duration
}
type CodexAuthGrant struct {
    AuthorizationCode string
    CodeVerifier      string // PKCE verifier returned by the poll endpoint
}

// internal/auth/pkce.go — owner: 03-claude-sub-oauth/01-anthropic-flow.md
func GeneratePKCE() (verifier, challenge string)
func BuildAnthropicAuthorizeURL(clientID, challenge, state string) string
func ExchangeAnthropicCode(ctx context.Context, clientID, code, verifier string) (*TokenResult, error)

// internal/auth/discovery.go — owner: 01-xai-device-flow/01-registry-extension.md
func ResolveTokenEndpoint(ctx context.Context, discoveryURL string) (string, error)
```

### Contract 3: Codex Responses chatter (internal/llm)

```go
// internal/llm/codex.go — owner: 02-chatgpt-codex/02-codex-transport.md
// Wire shape (POST {base}/responses):
//   model, input (string), instructions (string), stream (bool),
//   tools (array), tool_choice, reasoning {effort}, store: false
// Response: output array with items {type: "message"|"reasoning"|"function_call", ...}
// Error shape: {error: {code, message}}

func NewCodexClient(cfg *ModelConfig, opts ...CodexClientOption) *CodexClient
// Implements llm.Chatter (Chat + ChatStream with compile-time assertion
// `var _ Chatter = (*CodexClient)(nil)`).

// Codex Cloudflare headers (constant table; verified hermes auxiliary_client.py:780-812):
//   User-Agent: codex_cli_rs/0.0.0 (Hermes meept)
//   originator: codex_cli_rs
//   ChatGPT-Account-ID: <from access-token JWT claim
//     "https://api.openai.com/auth"."chatgpt_account_id"> (omit on parse failure)

// Owner: 02-chatgpt-codex/02-codex-transport.md
// Consumers: createChatterFor (provider_manager.go:115), registerOAuthProviders
```

### Contract 4: Anthropic Bearer auth mode (internal/llm/anthropic.go)

```go
// owner: 03-claude-sub-oauth/02-anthropic-bearer.md
// When cfg.OAuthProvider != "" on an Anthropic transport config:
//   - resolve token via TokenResolver (same WithTokenResolver path as client.go)
//   - send Authorization: Bearer <token> (NOT x-api-key)
//   - send anthropic-beta: oauth-2025-04-20
//   - keep anthropic-version header
// ModelConfig gains no new fields — OAuthProvider + TokenResolver already exist.
```

### Contract 5: Registration & CLI wiring (cross-branch)

```go
// registerOAuthProviders (internal/daemon/components.go:5868) needs NO changes:
// it iterates OAuthProviders registry entries with BaseURL != "" and registers
// ModelConfig{ProviderID, BaseURL, ModelID: providerID, OAuthProvider, ExtraHeaders}.
// New providers become usable by: (a) registry entry exists, (b) token stored,
// (c) chatter selection handles the transport.
// createChatterFor (provider_manager.go:115) gains dispatch for:
//   cfg.ProviderID == "openai-codex" -> NewCodexClient
//   isAnthropic(cfg) && cfg.OAuthProvider != "" -> AnthropicClient with bearer mode

// cmd/meept/oauth.go runOAuthConnect dispatches on providerCfg.Flow:
//   FlowDeviceRFC8628 (default) -> existing path
//   FlowDeviceCodex   -> codex three-step flow
//   FlowPKCEPaste     -> paste flow
// TUI config UI (internal/configui/sections_oauth.go) picks up new providers
// automatically via RegisteredProviders().
```

### Contract 6: TokenStore/Refresh extensions

```go
// internal/auth/refresh.go RefreshManager.refreshOne currently calls
// RefreshTokenRequest(ctx, flowCfg, token.RefreshToken) with form encoding.
// Extended behavior (owner: 03-claude-sub-oauth/01-anthropic-flow.md + shared):
//   - providerCfg.RefreshJSON == true -> JSON body {grant_type, refresh_token, client_id}
//     to anthropicTokenURLs (try primary, fall back to console host)
//   - xai: refresh uses form encoding to discovered token endpoint; tokens are
//     short-lived (~6h) so RefreshManager margin must cover it (default 10m OK;
//     config value wins)
//   - codex: refresh via RefreshTokenRequest form encoding to codexTokenURL
//     (grant_type refresh_token, client_id, refresh_token)
// TokenStore.Expiry handling unchanged (TokenResult.Expiry set from expires_in).
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-xai-device-flow/orchestrator.md | branch | none | (waits) | — |
| 02 | 02-chatgpt-codex/orchestrator.md | branch | none | (waits) | — |
| 03 | 03-claude-sub-oauth/orchestrator.md | branch | none | (waits) | — |

Branches are fully independent (different files except `providers.go` registry map entries and `cmd/meept/oauth.go` dispatch — both are append-only edits; see Conflict Avoidance below). Dispatch all three branches concurrently.

**Conflict avoidance rule:** every leaf that edits `internal/auth/providers.go` touches ONLY its own registry-map literal block. Every leaf that edits `cmd/meept/oauth.go` touches ONLY its own `case` in the flow dispatch. The registry-extension leaf (01-xai/01) owns the `FlowKind` type + struct fields edit and MUST land first within branch 01; branches 02/03 leaves depend on that type existing, so branch 01's leaf 01 is the root-wave gate. Practical dispatch: run branch 01 leaf 01 first (small, ~20 min), then everything else in parallel batches of 3.

### Leaf-level dependency graph

```
01-xai/01-registry-extension (FlowKind + fields + discovery)   [WAVE 1 - gate]
  -> 01-xai/02-xai-provider-entry + CLI                        [WAVE 2, group A]
  -> 02-codex/01-codex-flow                                     [WAVE 2, group A]
  -> 03-claude/01-anthropic-flow                                [WAVE 2, group A]
01-xai/02 done -> 02-codex/02-codex-transport + chatter wiring  [WAVE 3, group B]
             -> 03-claude/02-anthropic-bearer                   [WAVE 3, group B]
Waves 2+3 reviewed -> 04-registry-docs leaf (docs + AGENTS.md + e2e verify)   [WAVE 4]
```

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Wave 1 — Foundation gate

1. **Read** `01-xai-device-flow/01-registry-extension.md` and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-registry-extension.md"
   - Context: full leaf doc + Contract 1 + Contract 2 (discovery.go part) + coding conventions + current `providers.go` source INLINED
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files — explore with search_files or terminal cat. If you read a file, never feed its output into write_file."
   - Include: "After writing a file, do NOT read it back to verify. Write once and stop."

### Phase 2: Wave 2 — Three independent flows (parallel, max 3 batch)

2. Dispatch `01-xai-device-flow/02-xai-provider-entry.md`, `02-chatgpt-codex/01-codex-flow.md`, `03-claude-sub-oauth/01-anthropic-flow.md` simultaneously with the same context rules as above (each gets its own contracts inlined).

### Phase 3: Wave 3 — Transport integration (parallel)

3. After Wave 2 reviews pass, dispatch `02-chatgpt-codex/02-codex-transport.md` and `03-claude-sub-oauth/02-anthropic-bearer.md` simultaneously. Both need `internal/llm` context inlined (`createChatterFor`, `client.go` token-resolution block, `anthropic.go` header block).

### Phase 4: Wave 4 — Docs + end-to-end

4. Dispatch the docs leaf (root-level `04-registry-docs.md`): updates `docs/workflows/` + `docs/reference/cli.md` + AGENTS.md package table/invariants + adds `xai-oauth`/`openai-codex`/`anthropic-sub` to provider registry docs, then runs the full end-to-end verification.

### Phase 5: Review and Commit Each Child

After each implementation agent returns, the orchestrator reviews in-session (the main model reviews directly, NOT a delegated subagent):

1. **Orchestrator reviews in-session:** read changed files, check against leaf spec + contracts + Review Checklist, run `go build ./...` and leaf tests.
2. **If review finds gaps:** re-dispatch with specific feedback; max 3 cycles.
3. **If review passes:** commit the leaf's exact files: `git add <paths> && git commit -m "feat(oauth): implement [leaf name]"`. Update tracking table → REVIEWED.

### Phase 6: Integration Review

After ALL children reach REVIEWED:

1. Run `go build ./... && go test ./internal/auth/ ./internal/llm/ ./cmd/... -count=1`.
2. Verify contracts: compile-time chatter assertions, registry entries resolve via `ResolveProviderConfig`, `Flow` dispatch covers all three kinds.
3. `make graphs` regenerates; `gofmt -l` on changed dirs empty.
4. Verify no line-number corruption: `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/auth internal/llm cmd/meept` returns zero.
5. Commit integration: `git add -A docs/plans/2026-08-25-oauth-providers docs && git commit -m "feat(oauth): integrate three new OAuth providers"`.
6. Update tracking table: all children → COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied (especially the frozen constants — diff against Hermes source lines cited)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues (tokens never logged; no `slog` with token values)
- [ ] No debug artifacts: no fmt.Println in non-CLI code, no TODOs, no placeholder values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source files
- [ ] UI text lowercase (CLI output, TUI strings)

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go (go.mod module `github.com/caimlas/meept`). No new dependencies — stdlib `crypto/rand`, `crypto/sha256`, `encoding/base64`, `encoding/json`, `net/http`, `net/url` cover everything.
- **Error handling:** wrap with `%w`, return early, no `_ =` ignored errors (pre-commit blocks), two-value type assertions on `map[string]any`.
- **HTTP:** reuse the package-level `deviceFlowHTTPClient` pattern (30s timeout, per-request context). Never hold a mutex across I/O (mutexio analyzer).
- **IDs:** never `time.Now().UnixNano()`/`math/rand` for state/nonce — use `crypto/rand` (predid analyzer).
- **Setters:** every `Set*` method nil-guards.
- **Naming:** exported PascalCase, unexported camelCase, provider IDs kebab-case strings.
- **Testing:** table-driven, stdlib testing (project uses no testify), `_test.go` alongside source.
- **Formatting:** `gofmt` before reporting completion.
- **Secrets:** client IDs are public (not secrets); access/refresh tokens never logged, never in error messages returned to callers.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-xai/01-registry-extension | PENDING | 0 | |
| 01-xai/02-xai-provider-entry | PENDING | 0 | |
| 02-codex/01-codex-flow | PENDING | 0 | |
| 02-codex/02-codex-transport | PENDING | 0 | |
| 03-claude/01-anthropic-flow | PENDING | 0 | |
| 03-claude/02-anthropic-bearer | PENDING | 0 | |
| 04-registry-docs (root leaf) | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` — full tree compiles.
2. `go test ./internal/auth/ -count=1 -v` — all flow tests pass (httptest servers simulate each endpoint shape).
3. `go test ./internal/llm/ -count=1` — codex chatter + anthropic bearer tests pass.
4. `go vet ./internal/auth/ ./internal/llm/ ./cmd/meept/` clean; `gofmt -l internal/auth internal/llm cmd/meept` empty.
5. Manual smoke (documented in docs leaf, requires real accounts — mark optional):
   - `./bin/meept config oauth connect xai-oauth` prints verification URL, polls, saves token.
   - `./bin/meept config oauth connect openai-codex` prints `auth.openai.com/codex/device` + code.
   - `./bin/meept config oauth connect anthropic-sub` prints authorize URL, prompts paste.
   - `./bin/meept config oauth status` lists all three.
6. `make graphs` — regenerated topology committed fresh.

## Structural Completeness Check (Before Dispatch)

After writing every document in this tree, run:

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans/2026-08-25-oauth-providers --strict-leaves
```

Required for every orchestrator (this file + 3 branch orchestrators): `## Dispatch Protocol`, `## Interface Contracts`, `## Review Checklist`, `## Coding Conventions`, `## Completion Tracking Table`, `## Integration Test Plan`. Every leaf: "Do NOT commit" + Self-Verification Checklist (strict).

## Open Questions

| # | Question | Rec | Impact if reversed |
|---|----------|-----|--------------------|
| 1 | Codex chatter streaming: full SSE now or error-stub first pass? (leaf 02 Task 5) | Minimal SSE for `response.output_text.delta` — Chatter likely requires ChatStream | Stub = streaming requests fail on Codex provider until follow-up |
| 2 | `FormEncoded` derivation via `ProviderID == "xai-oauth"` check vs explicit struct field? | Provider-ID check — one provider uses form encoding today | Explicit field = one more registry field all entries must consider |
| 3 | Live-login smoke test with real accounts before merging? | Optional — httptest covers wire shapes; live flows risk 429 rate-limit noise | Skipping = small risk of endpoint-shape drift the simulators missed |
| 4 | Register real OAuth apps to replace github-models/google placeholder client IDs? | Out of scope (separate task) | None on this tree — existing placeholders keep working via env overrides |

## Notes

- **Placeholder client IDs:** github-models/google use placeholder IDs pending app registration; the three NEW providers use real, public first-party client IDs from Hermes (they are embedded in shipped CLIs, not secrets).
- **Hermes source is the spec.** Where this plan cites `hermes_cli/auth.py:NNN` or `anthropic_adapter.py:NNN`, the implementer should verify constants against `~/git/hermes-agent/` (read via terminal cat, not read_file) if anything seems inconsistent. Frozen constants in Contract 1 are the binding values.
- **Codex 429 retry:** the usercode endpoint rate-limits (Hermes retries 4x with Retry-After-honoring backoff). Implement `parseRetryAfter` helper.
- **Anthropic token-endpoint UA:** must NOT start with `claude-code/` (429 rate limit); use `axios/1.7.9`. Inference path keeps `claude-code/` UA + `x-app: cli`-style fingerprint — do NOT reuse the axios UA on messages requests.
- **xAI short-lived tokens:** ~6h expiry, refresh works via discovered token endpoint. The RefreshManager 10m default margin is fine; no per-provider margin machinery needed.
- **Do not modify** `internal/calendar/`, `google-oauth`, or `google-calendar` entries. Do not add image/video providers — `xai` API-key provider for grok-imagine stays untouched; `xai-oauth` is a separate chat provider.
- **AGENTS.md maintenance:** the docs leaf must update AGENTS.md if any invariant changes (none expected — no new bus topics, no session-ID semantics).
