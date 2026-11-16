# 03 Streamed Tool-Call Validation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test (report suspected-wrong gate tests to the
> orchestrator instead). Do NOT commit - the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. After writing a file, do NOT
> read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Add validation of assembled tool calls (Contract C9): a call naming a function that was never offered, or carrying arguments that are not a JSON object, is rejected before the executor sees it. Covers both the non-streaming path and the SSE streaming path (validate only after the stream completes).
- **Dependencies:** 00T gate tests committed (RED). **No dependency on leaf 01** - this leaf does not touch the selection chain.
- **Estimated Context:** 50K
- **Concurrency Group:** C (wave 1 - file-disjoint from 01/02/04)
- **Source reference:** Sovereign Router `src/validation.py:78-126` (`validate_response`, `ToolCallAssembler`, `validate_streamed_text_outcome`)

## Goal

meept catches an *empty* completion everywhere - `ErrEmptyResponse`, classified by every client, retried within the short budget, then rotated (contract in `internal/llm/AGENTS.md`). It does **not** catch a *malformed* tool call. A model that emits

```json
{"name": "shel", "arguments": "{not json"}
```

reaches the tool executor, which then fails or, worse, silently does nothing. Sovereign Router's `validate_response` (`src/validation.py:78-110`) checks three things before the call is trusted:

1. `unknown_function:<name>` - the name is absent from the request's offered tool list
2. `tool_args_invalid_json` - arguments are not valid JSON (or not a JSON object)
3. `json_mode_parse_fail` - JSON mode was requested and the content does not parse

This leaf implements 1 and 2 for both the non-streaming and the streaming paths. Item 3 is out of scope (meept's `ToolChoice`/`SchemaMode` machinery is a different mechanism; adding it here would couple this leaf to the request builder).

**Streaming rule, verbatim from `validation.py:113-126`:** streamed tool-call chunks are buffered to completion and validated *before anything is sent to the client*; streamed text commits at the first content chunk, after which only `empty_response` / `length_no_content` can be logged, not escalated. meept's equivalent: tool-call deltas are already assembled somewhere on the streaming path - find that assembly point and validate its output once, at stream end.

## Context

- `internal/llm/client.go` - the OpenAI-compatible client. `Chat` (non-streaming), `ChatWithProgress`, `ChatWithDeltaCallback` (streaming-delta). `parseResponseWithTools` is the non-streaming parse site named in `internal/llm/AGENTS.md`.
- `internal/llm/anthropic.go` - `parseResponse` + `buildResponseFromBlocks`. Anthropic tool calls arrive as `tool_use` blocks; validate those too.
- `internal/llm/codex.go` / `codex_sse.go` - `parseResponse` / `parseResponsesSSE`; tool calls are `function_call` items.
- `internal/llm/content_parts.go` - content-part assembly helpers.
- `internal/security/secret_scanner.go` - unrelated; do not touch.
- `internal/llm/AGENTS.md` - the empty-completion contract. **Read it before editing any retry loop.** The ordering rule is load-bearing: refusal/quota/overflow early-exits keep precedence over the empty branch. Tool-call validation is a NEW check that must sit AFTER a successful parse and BEFORE the executor, and must not disturb that ordering.

## Verified Anchors

- `internal/llm/client.go` - `parseResponseWithTools` (non-streaming parse; named in `internal/llm/AGENTS.md`)
- `internal/llm/client.go:2211` - `captureFirstDelta`, the streaming first-delta timestamp (`ttft_ms`); the stream terminator is the validation point
- `internal/llm/anthropic.go` - `parseResponse`, `buildResponseFromBlocks`, `classifyAnthropicEmpty`
- `internal/llm/codex.go` / `codex_sse.go` - `parseResponse` / `parseResponsesSSE`
- `internal/llm/AGENTS.md` - "Empty/whitespace completions are provider flakes, not fatal errors" - the ordering contract this leaf must preserve
- `internal/llm/errors_tier.go:15-45` - the sentinel + typed-error pattern (for reference; this leaf needs only a sentinel)
- `e2e/suites/sovereign-parity/toolcheck_test.go` - gate tests T19, T20, T21

Frozen constants quoted from `src/validation.py`:

```
failure codes: "empty_response", "length_no_content",
               "unknown_function:<name>", "tool_args_invalid_json",
               "json_mode_parse_fail"
```

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm/stream_toolcheck.go
var ErrToolCallInvalid = errors.New("llm: tool call failed validation")

// ValidateToolCalls returns ErrToolCallInvalid when any tool call names a
// function absent from `offered`, or when its arguments do not parse as a
// JSON object. A response with no tool calls returns nil.
//
// The returned sentinel MUST be the bare value (pointer identity), matching
// the ErrEmptyResponse contract in internal/llm/AGENTS.md - never a wrap.
func ValidateToolCalls(resp *Response, offered []string) error

// OfferedToolNames extracts the function names from the request's tool
// definitions, in declaration order, de-duplicated.
func OfferedToolNames(tools []ToolDef) []string
```

### What This Leaf Consumes

- `*llm.Response` - the parsed response type carrying `ToolCalls`.
- The tool-definition type the request builder uses. **Survey first** (`internal/llm/models.go`, `content_parts.go`) and use that exact type in `OfferedToolNames`.

### Frozen semantics

```
- no tool calls                 -> nil (nothing to validate)
- offered list EMPTY            -> nil for ANY call (the caller offered no
                                   tools, so there is no contract to violate;
                                   a call here is the model hallucinating a
                                   tool it was never told about, which is a
                                   DIFFERENT defect and out of scope)
- name not in offered           -> ErrToolCallInvalid
- arguments are a JSON object   -> pass
- arguments are valid JSON but
  NOT an object (array/string)  -> ErrToolCallInvalid
- arguments unparseable         -> ErrToolCallInvalid
- arguments empty string        -> pass (some providers send "" for a
                                   no-argument call)
```

The "offered list EMPTY -> nil" rule is deliberate and must be commented: meept has many no-tool turns, and a call arriving there is a different bug class that this leaf must not conflate with a malformed call.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `ErrToolCallInvalid` sentinel with a doc comment pinning the pointer-identity rule | Create: `internal/llm/stream_toolcheck.go` | `go build ./internal/llm/` | RED (compile) | GREEN | Given the sentinel; When compared; Then `err == llm.ErrToolCallInvalid` is true for the bare value |
| T2 | Implement `OfferedToolNames(tools []ToolDef) []string` - declaration order, de-duplicated | Modify: `internal/llm/stream_toolcheck.go` | `go test -p 2 ./internal/llm/ -run TestOfferedToolNames` | RED (undefined) | GREEN | Given duplicate tool names; When extracted; Then each appears once, in order |
| T3 | Implement `ValidateToolCalls` for the non-streaming response: unknown name, non-object args, unparseable args | Modify: `internal/llm/stream_toolcheck.go` | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestToolCheck_'` | RED (undefined) | GREEN after this task | Given tools `[a,b]` and a call to `c`; When validated; Then `ErrToolCallInvalid` by pointer identity |
| T4 | Empty-offered and empty-arguments pass-through (the frozen table) | same | `-run 'TestToolCheck_FrozenTable'` | RED | GREEN | Given each row of the frozen table; When validated; Then the row holds |
| T5 | Wire validation into the non-streaming path after parse, before the response returns to the loop | Modify: `internal/llm/client.go` (`parseResponseWithTools` caller) | `go test -p 2 ./internal/llm/ -run TestClient_ToolCallValidation` | RED | GREEN | Given a malformed call from a fake upstream; When `Chat` returns; Then `ErrToolCallInvalid` and the executor is not invoked |
| T6 | Wire validation into the streaming path at the stream terminator (after the delta assembly completes, before the assembled `Response` is handed to the loop) | Modify: `internal/llm/client.go` (`ChatWithDeltaCallback` terminator) | `-run TestToolCheck_Streamed` | RED | GREEN | Given a streamed call split across chunks; When the stream ends; Then validation runs once on the assembled call |
| T7 | Anthropic path: validate assembled `tool_use` blocks in `buildResponseFromBlocks` | Modify: `internal/llm/anthropic.go` | `go test -p 2 ./internal/llm/ -run TestAnthropic_ToolCallValidation` | RED | GREEN | Given a `tool_use` block naming an unoffered tool; When parsed; Then `ErrToolCallInvalid` |
| T8 | Codex path: validate `function_call` items in `parseResponse` and `parseResponsesSSE` | Modify: `internal/llm/codex.go`, `internal/llm/codex_sse.go` | `go test -p 2 ./internal/llm/ -run TestCodex_ToolCallValidation` | RED | GREEN | Given a `function_call` with unparseable arguments; When parsed; Then `ErrToolCallInvalid` |
| T9 | Ordering preservation: an empty completion still yields `ErrEmptyResponse` (not `ErrToolCallInvalid`), and a refusal still yields `RefusalError` | Modify: none (assert only) | `go test -p 2 ./internal/llm/ -run 'TestEmptyCompletion|TestRefusal'` | n/a | GREEN, existing tests unmodified | Given an empty 200 body; When parsed; Then `ErrEmptyResponse`, never `ErrToolCallInvalid` |
| T10 | Executor-never-invoked proof via the e2e gate (counter on the fake tool executor) | - | `-run TestToolCheck_ExecutorNotInvoked'` | RED | GREEN | Given a malformed call; When the turn runs; Then the executor counter is 0 |

## Self-Verification Checklist

- [ ] T1-T10 complete; gate tests T19, T20, T21 GREEN.
- [ ] `ErrToolCallInvalid` is the BARE sentinel on every return path (no wrap).
- [ ] Empty completions still classify as `ErrEmptyResponse`; the retry ordering is unchanged.
- [ ] Refusals still classify as `RefusalError` and keep their one-hop policy.
- [ ] All three clients covered: OpenAI-compatible, Anthropic, Codex.
- [ ] Streaming validates ONCE, at stream end, never per chunk.
- [ ] The "offered list empty -> nil" rule is implemented and commented.
- [ ] No new retry loop; no new sleep; no new bus topic.
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] Frozen table matches master.md C9 row for row.
- [ ] Pointer identity holds: `err == llm.ErrToolCallInvalid`.
- [ ] The three clients each validate, and each keeps its own empty/refusal precedence.
- [ ] Streaming validates after assembly, not mid-stream (no false pass, no false fail).
- [ ] A no-tool turn is unaffected (offered-empty rule).
- [ ] No existing test assertion was edited.
- [ ] Comments name the incident shape (a hallucinated function name reaching the executor).

## Do NOT commit

The orchestrator commits this leaf after review.