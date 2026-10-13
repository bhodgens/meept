# L04 — OpenAI prompt_cache_key + Deterministic Tool Order (P4)

**Proposal:** P4. **Depends on:** none. **Files (3):**
- Modify: `internal/llm/client.go` (request payload builder, ~:480-538)
- Modify: tool serialization site — locate via `search_files` for where the
  tools array is built for OpenAI-compatible requests (likely client.go or
  content_parts.go)
- Test: new `internal/llm/client_openai_cache_test.go`

**Problem:** (a) OpenAI supports an optional `prompt_cache_key` routing hint
(request-level string); requests sharing a key route to the same backend,
raising prefix-hit rates for per-session prefixes. Meept never sends it.
(b) Tool-list instability is the #1 prefix-cache buster locally (tools render
before system under `--jinja`); any map-iteration nondeterminism in tool
assembly shuffles the wire bytes. Go sorts map keys when marshaling maps, but
tool definitions assembled from a slice-of-structs registry preserve insertion
order — registration order, not alphabetical — which can differ across init
paths.

**Contracts:** `prompt_cache_key` = session ID string (root knob: always sent
when a session id is known). Tool ordering = stable sort by tool name before
serialization, applied ONLY to the OpenAI-compatible payload builder (Anthropic
path has its own tools marshaling — out of scope here unless trivially shared).

### Tasks

**Task 1 — failing tests:** (i) `TestOpenAIPayloadIncludesPromptCacheKey`:
request config carrying a session id must marshal a top-level
`"prompt_cache_key"` field equal to it; absent session id → field omitted.
(ii) `TestToolsSerializedStableOrder`: same tool set registered in two
different insertion orders must produce byte-identical tools JSON.
Run: `go test -p 2 ./internal/llm/ -run 'TestOpenAIPayloadIncludesPromptCacheKey|TestToolsSerializedStableOrder'`
— expect FAIL.

**Task 2 — add the field:** extend the OpenAI-compatible request struct
(client.go payload) with `PromptCacheKey string \`json:"prompt_cache_key,omitempty"\``
and populate from the request config's session id (locate the session id on
ChatConfig/ChatOptions — search_files for `SessionID` in internal/llm/interface.go).

**Task 3 — stable sort:** at the tools serialization site, `sort.Slice` by
`Function.Name` (or the equivalent) immediately before marshaling. Confirm the
sort does not mutate shared registry state (copy the slice first).

**Task 4 — llama-server probe (manual, in report):** two identical follow-up
requests against local llama-server; confirm server log line shows cached-token
reuse on the second (`n_past` far below total prompt tokens). Include the log
lines.

**Task 5 — full package + report.** `go test -p 2 ./internal/llm/`. Do NOT commit.

## Self-Verification Checklist
- [ ] prompt_cache_key omitempty — never an empty string on the wire
- [ ] Tools sort is a copy; registry order untouched
- [ ] Anthropic path unchanged

## Review Checklist (for orchestrator)
- [ ] Field name exactly `prompt_cache_key` (OpenAI spec)
- [ ] llama-server probe log lines attached to report
