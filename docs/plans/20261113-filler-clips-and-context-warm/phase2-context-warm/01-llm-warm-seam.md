# Leaf 01 - LLM Warm Seam

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD in the
> isolation worktree (path in your dispatch context). Do NOT run `git commit`
> or `git add`. Make the 00T gate tests green without editing them (report
> any contract contradiction to the orchestrator instead).

## Meta

- **Parent:** orchestrator.md (phase2-context-warm)
- **Depends on:** 00T TESTS_VALIDATED
- **Estimated context:** ~55K
- **Parallel group:** Wave A (with 02; file-disjoint, seam-pinned)
- **Requirements:** R8, C4

## Goal

`internal/llm`: the `Warm` request path per root Contract C4, with the three
hard constraints (no cache traffic, no retry loop, no identity side effects)
pinned by tests.

## Scope

Signature, verbatim:

```go
func (c *Client) Warm(ctx context.Context, messages []ChatMessage, opts ...ChatOption) error
```

Facts already verified in the code - do not re-derive, and do not "correct"
them back:
- The message type is `llm.ChatMessage` (`internal/llm/models.go:23`).
- There is no alias parameter. Alias resolution belongs to the Resolver /
  ProviderManager layer (`internal/llm/provider_manager.go`,
  `resolver.go`). The caller resolves, then passes the same opts to Warm.
- The turn payload builder is the non-streaming path in
  `Client.Chat` (`client.go:765` onward); the tools block is serialized at
  `client.go:714-716` and `prompt_cache_key` at `:725`.
- Response caching has three sites to avoid: exact-key `Get` at `:785`,
  exact-key `Put` at `:977`, semantic `Get` at `:383` and `Store` at `:410`.
  No bypass flag exists today - you are adding one.

## Tasks

1. `internal/llm/warm.go` (new file): build the request with the SAME
   serializer the non-streaming turn uses (reuse the function, do not
   rebuild a payload by hand - prefix identity is the whole feature). Force
   `max_tokens=1`, non-streaming, carry the caller's `sessionID` so both the
   affinity header and `prompt_cache_key` match the real turn, and carry the
   caller's `tools` / `tool_choice` / model unchanged.
2. **Cache bypass.** Add an additive `chatOptions` field (for example
   `noCache bool`, set by a `WithNoCache()` ChatOption) and gate BOTH the
   exact-key tier and the semantic tier on it, in all chat variants that
   already consult them. Warm passes it. Why this is not optional: a
   `max_tokens=1` response stored under the exact key is SERVED to the real
   turn as the completed answer whenever the final transcript matches the
   partial. Pinned test: call Warm, then call Chat with identical messages
   against a fake upstream that counts requests - the turn must reach the
   upstream.
3. **No retry.** Warm must not enter the short-retry loop
   (`client.go:817`) and must not re-dispatch `ErrEmptyResponse`. A
   `max_tokens=1` reply frequently looks empty. Pinned test: empty-text fake
   upstream sees exactly ONE request per Warm call.
4. **No identity side effects.** Warm never records a Resolver alias
   failure, never arms an endpoint cooldown, never writes the sticky
   per-session endpoint map, and never publishes an `llm_tier*` degradation.
   It also never blocks in `budget.WaitForRateLimit` (`client.go:801-802`):
   a warm is skippable traffic, not a queued turn. If a budget check reports
   exceeded, Warm silently returns.
5. **Slot posture and deadline.** `priority=false` on every warm
   (`chatOptions.priority`, `client.go:1291-1297`; llm AGENTS.md D11), and
   Warm owns a bounded deadline (`default 1200 ms`, overridable for tests) so
   a hung upstream cannot pin a caller goroutine.
6. **Endpoint support gate.** One predicate decides whether an endpoint can
   reuse a prompt prefix: llama.cpp started with `--cache-reuse`
   (`config/models.json5:259`, `:318`). MLX (`mlx_lm server`,
   `config/models.json5:185`) and cloud providers return no-op. Do not
   scatter this check across call sites; expose it once and have leaf 02 use
   it too.
7. **Metrics.** A completed warm may record usage/cost through the existing
   `c.budget.RecordUsageWithScope` (it is real provider compute), but it must
   NOT write an `llm_calls` row that looks like a turn. Prefer a
   `warm=true`-style dimension if the metrics schema allows it cheaply;
   otherwise record no row and say so in the report.
8. **Client classes.** Anthropic and Codex are out of scope: voice turns ride
   the OpenAI-compatible local endpoint. Warm on a non-supporting client
   class is a DEBUG no-op (pin with a test) so leaf 02 needs no type
   knowledge.

## Interface Contract (exposes to siblings)

`Client.Warm` exactly per C4, plus the endpoint-support predicate from task 6.
Leaf 02 calls only these.

## Self-Verification Checklist

- [ ] `go build ./internal/llm/...`
- [ ] `go test -p 2 ./internal/llm/... -count=1` green
- [ ] Payload snapshot test: the warm request equals the same turn's
      request except for `max_tokens` and `stream` (prefix identity proof,
      tools included).
- [ ] Cache test: warm-then-turn reaches upstream; `tokenCache` holds no
      entry keyed on the warm's messages.
- [ ] Retry test: empty-text upstream sees exactly 1 request.
- [ ] Deadline test: a stalled fake upstream returns within the bound.
- [ ] `gofmt -l` empty.

## Review Checklist (for the orchestrator)

- [ ] The cache bypass is additive: with `noCache` unset, all three chat
      variants behave byte-identically (diff review plus existing tests green).
- [ ] Warm cannot emit WS relay events, session messages, or audit entries.
- [ ] Warm never touches the Resolver, endpoint blocks, or the sticky map.
- [ ] `priority=false` and no `WaitForRateLimit` call on the warm path.
- [ ] Typed-nil/mutex conventions clean; `setters_test` unaffected.