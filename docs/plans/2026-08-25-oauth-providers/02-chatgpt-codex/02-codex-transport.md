# Codex Responses Chatter + Wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Implement the Codex Responses chatter (`internal/llm/codex.go`), Cloudflare headers, JWT account-ID extraction, and `createChatterFor`/registry wiring.
- **Dependencies:** 01-codex-flow.md (registry entry exists so wiring resolves)
- **Estimated Context:** 70K (exploration 25K + generation 30K + iteration 10K + overhead 5K)
- **Concurrency Group:** WAVE 3 group B
- **Audit references:** none

## Goal

A `Chatter` implementation for the Codex Responses API. Request shape: `POST {base}/responses` with `{model, input, instructions, stream: false, store: false, tools, tool_choice, reasoning}`. Response shape: `{output: [{type: "message"|"reasoning"|"function_call", ...}], usage, error}`. Every request carries the Cloudflare header set (frozen table below) and a Bearer token from the TokenResolver. Dispatch: `createChatterFor` routes `openai-codex`/`TransportCodexResponses` here.

## Context

`internal/llm` selects chatters in `createChatterFor` (provider_manager.go:115-145): Anthropic vs OpenAI-compatible `Client`. `Client` (client.go) shows the TokenResolver pattern (lines 755-780: resolve → `Authorization: Bearer`), budget/metrics handling, and `Chatter` interface conformance. `Chatter` interface is in `interface.go` (Chat + ChatStream methods — check exact signatures with `grep -n "type Chatter" internal/llm/interface.go` and mirror `Client`'s method set; if ChatStream is required, implement it returning an error `streaming not supported` ONLY if other chatters do the same — check how `AnthropicClient` handles a missing stream first and match that precedent).

Key files:
- `internal/llm/client.go` — token resolution, budget, metrics patterns to mirror
- `internal/llm/provider_manager.go:115` — `createChatterFor` (add dispatch branch)
- `internal/llm/provider_registry.go` — add `CanonicalProviders` entry
- `internal/llm/errors.go` — ClientError types for error mapping

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/llm/codex.go
func NewCodexClient(cfg *ModelConfig, opts ...CodexClientOption) *CodexClient
type CodexClientOption func(*CodexClient)
func WithCodexLogger(l *slog.Logger) CodexClientOption
func WithCodexBudget(b *Budget) CodexClientOption
func WithCodexTimeout(d time.Duration) CodexClientOption
func WithCodexTokenResolver(tr TokenResolver, provider string) CodexClientOption
var _ Chatter = (*CodexClient)(nil)

// Frozen Cloudflare header table (hermes auxiliary_client.py:780-812):
//   User-Agent: codex_cli_rs/0.0.0 (Hermes meept)
//   originator: codex_cli_rs
//   ChatGPT-Account-ID: from access-token JWT payload
//     claim "https://api.openai.com/auth"."chatgpt_account_id"
//     (split on ".", base64url-decode segment 2 with padding fix, json parse;
//      ANY failure -> omit header silently)

// Request body (POST {base}/responses):
//   model: cfg.ModelID (e.g. "gpt-5" — Codex maps names server-side)
//   input: string (message content; last user message text)
//   instructions: string (system prompt)
//   stream: false
//   store: false
//   tools: [{type:"function", name, description, parameters}] (when request has tools)
//   tool_choice: "auto" (when tools present)
//   reasoning: {effort: "medium"} (when cfg/reasoning set; omit otherwise)

// Response mapping (response.output[] items):
//   {type:"message", content:[{type:"output_text", text}]}  -> Response content
//   {type:"reasoning", summary:[{type:"summary_text", text}]} -> reasoning text (append to thinking/reasoning field if ChatResponse has one; else prepend "[reasoning] " to content — match how Client surfaces thinking)
//   {type:"function_call", name, arguments, call_id} -> ToolCall entries
//   usage: {input_tokens, output_tokens} -> Usage fields
```

### What This Leaf Consumes

From branch leaf 01: registry entry `openai-codex` (`OAuthProvider` set by `registerOAuthProviders`). Existing: `ModelConfig`, `TokenResolver`, `ChatRequest`/`ChatResponse`, `Budget`, `ClientError`, `Chatter`.

## Tasks

### Task 1: JWT account-ID extraction

**Objective:** `codexAccountID(token string) string` — extract claim or return "".

**Files:**
- Create: `internal/llm/codex.go` (start with just this helper + struct skeleton)
- Test: `internal/llm/codex_test.go`

**Step 1: Failing test** — build a JWT: base64url(header) + "." + base64url(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-1"}}`) + ".sig"; assert extraction returns "acct-1". Cases: two-segment garbage → ""; empty → ""; valid JSON without claim → "".

**Step 2:** FAIL. **Step 3:** Implement with `strings.Split`, `base64.RawURLEncoding.DecodeString` (fall back to padded decode by appending `=`), `encoding/json` into `map[string]any` with two-value assertions. **Step 4:** PASS.

### Task 2: Request build + headers

**Objective:** Correct URL, body, and frozen header table.

**Files:**
- Modify: `internal/llm/codex.go`
- Test: `internal/llm/codex_test.go`

**Step 1: Failing test** — httptest server captures method/URL/headers/body. `NewCodexClient(&ModelConfig{BaseURL: srv.URL, ModelID: "gpt-5", OAuthProvider: "openai-codex"}, WithCodexTokenResolver(stubResolver{"tok"}, "openai-codex"))` then `Chat(...)`. Assert:
- POST to `srv.URL + "/responses"`
- `Authorization: Bearer tok`
- `User-Agent: codex_cli_rs/0.0.0 (Hermes meept)`, `originator: codex_cli_rs`
- stubResolver returns a token WITH account claim → `Chatgpt-Account-Id`/`ChatGPT-Account-ID` header present (use canonical `ChatGPT-Account-ID`; Go canonicalizes on Set and Get)
- body JSON: `stream == false`, `store == false`, `model == "gpt-5"`, `input` contains user text, `instructions` contains system text

**Step 2:** FAIL. **Step 3:** Implement Chat: build payload map, marshal, request with context, resolve token (mirror client.go 769-778 incl. error wrap `failed to resolve OAuth token`), set headers, send. Parse response per contract. **Step 4:** PASS.

### Task 3: Response parsing (message/reasoning/function_call/usage/error)

**Objective:** Map all output item types + usage + errors.

**Files:**
- Modify: `internal/llm/codex.go`
- Test: `internal/llm/codex_test.go`

**Step 1: Failing tests** (table-driven; one httptest per row):
- message-only → content text, FinishReason "stop", usage counts
- reasoning + message → reasoning surfaced (see contract; match Client's thinking convention — grep how `ChatResponse` exposes reasoning: if there is a `Reasoning`/`Thinking` field use it, else prepend)
- function_call → `ToolCalls` entry with Name/Arguments/ID
- HTTP 401 → ClientError with status; body `{"error":{"code":"usage_limit","message":"m"}}` → error message contains "m"

**Step 2:** FAIL. **Step 3:** Implement parsing. Handle missing `output` array as error (malformed response). **Step 4:** PASS.

### Task 4: createChatterFor dispatch + provider registry entry

**Objective:** Route `openai-codex` to the new chatter; add `CanonicalProviders` entry.

**Files:**
- Modify: `internal/llm/provider_manager.go` (`createChatterFor`, before the `isAnthropic` check)
- Modify: `internal/llm/provider_registry.go` (append to `CanonicalProviders`)

**Step 1: Failing test** — `createChatterFor(&ModelConfig{ProviderID: "openai-codex", BaseURL: "https://chatgpt.com/backend-api/codex", OAuthProvider: "openai-codex"}, nil, logger, store)` returns `*CodexClient` (type assert). Also `GetProviderByID("openai-codex")` found with `AuthType: AuthOAuthDevice`, `Transport: TransportCodexResponses`.

**Step 2:** FAIL. **Step 3:**

```go
// createChatterFor, before isAnthropic check:
if cfg.ProviderID == "openai-codex" || cfg.Transport == TransportCodexResponses {
    opts := []CodexClientOption{WithCodexLogger(logger)}
    if budget != nil { opts = append(opts, WithCodexBudget(budget)) }
    if cfg.Timeout > 0 { opts = append(opts, WithCodexTimeout(cfg.Timeout)) }
    if tr != nil && cfg.OAuthProvider != "" { opts = append(opts, WithCodexTokenResolver(tr, cfg.OAuthProvider)) }
    return NewCodexClient(cfg, opts...)
}
```

Registry entry:

```go
{
    ID:        "openai-codex",
    Name:      "ChatGPT / Codex",
    Transport: TransportCodexResponses,
    AuthType:  AuthOAuthDevice,
    BaseURL:   "https://chatgpt.com/backend-api/codex",
    DocURL:    "https://developers.openai.com/codex",
    Supports:  []string{CapStreaming, CapTools, CapReasoning},
},
```

**Step 4:** `go build ./... && go test ./internal/llm/ -count=1`.

### Task 5: ChatStream decision

**Objective:** Satisfy the full `Chatter` interface.

Check `internal/llm/interface.go` for the exact method set. If ChatStream is part of Chatter: implement it using `stream: true` + SSE parsing of `response.output_text.delta` events IF `Client`/`AnthropicClient` stream natively; otherwise mirror whatever non-streaming chatter does (some return `ErrStreamingUnsupported` — grep for that string; if no precedent, implement SSE minimally: data lines with JSON `{type:"response.output_text.delta", delta}` accumulate + final `response.completed`). Keep this task's scope minimal and consistent with the interface contract; document the choice.

## Self-Verification Checklist

- [ ] Tasks 1-4 (+5 if applicable) done; `go test ./internal/llm/ -count=1` + `go build ./...` green
- [ ] Header table byte-exact; account-ID header omitted (not error) on bad JWT
- [ ] `store: false` and `stream: false` always in body
- [ ] `var _ Chatter = (*CodexClient)(nil)` present
- [ ] No deviations undocumented

**DO NOT COMMIT.**

**Deviations from spec:** [none / list — especially the Task 5 streaming decision]

## Review Checklist (For Review Agent)

- [ ] All tests passing; TDD followed
- [ ] Frozen constants: UA string, originator, endpoints, registry entry fields
- [ ] Token never logged; JWT parse failures soft
- [ ] Tool calls + reasoning mapped (not dropped)
- [ ] Dispatch placed before isAnthropic (codex base URL contains no "anthropic" so order is safe, but before keeps intent clear)
- [ ] Budget/metrics wiring mirrors Client patterns
- [ ] No debug artifacts; gofmt; no line-number corruption

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Responses API `input` may accept a message array; the minimal string form is sufficient and matches codex-cli behavior for simple turns. Tool-call HISTORY (multi-turn with prior function results) needs the array form — if `ChatRequest` carries prior tool results, map them: `{type:"function_call_output", call_id, output}` items appended after the user input. Grep how Client maps `ToolResults`/`tool` role messages and mirror; if the codebase has no tool-result plumbing, string-form only is acceptable — document as deviation.
- `reasoning.effort`: only when the request carries a reasoning override (grep `ReasoningConfig` usage in client.go); default omit.
- Budget recording: reuse the same metrics helper calls Client uses (`c.recordMetrics`-style); keep consistent names.
