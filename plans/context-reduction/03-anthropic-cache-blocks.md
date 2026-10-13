# L03 — Anthropic Cache Breakpoints via Structured Blocks (P4)

**Proposal:** P4. **Depends on:** none. **Files (2):**
- Modify: `internal/llm/anthropic.go` (~:877-938 system block marshaling, :1183-1191 classification)
- Modify or extend tests: `internal/llm/anthropic_cache_test.go`

**Problem:** The Anthropic provider re-derives static/dynamic sections by
splitting the system prompt on `"\n\n"` (anthropic.go:1184). Any static section
containing a blank line silently re-partitions static/dynamic — the cache_control
breakpoint lands mid-section, invalidating the cached prefix. The structured
builder `BuildSystemPromptBlocks` (prompt_cache.go:60) already exists and does
this correctly; the provider just does not use it end-to-end. Additionally the
Session-scope block receives NO cache_control (only Static does, :1188-1189).

**Contract (root knob):** behavior follows the existing `prompt_cache` config
(default on). This leaf changes HOW blocks are derived, not whether caching is
enabled. Do NOT add a 3rd breakpoint decision here — if adding a trailing-turn
breakpoint is trivial (the messages array last user/assistant block), do it and
note it; otherwise leave for follow-up.

### Tasks

**Task 1 — failing test:** in anthropic_cache_test.go add
`TestCacheBlocksFromStructuredBuilder`: a system prompt assembled by
`BuildSystemPromptBlocks` (containing a static section WITH a blank line inside
it) must produce exactly one Static cache_control block whose content matches
the full static text, and one Session block; the blank-line static section must
NOT be split into two blocks. Run `go test -p 2 ./internal/llm/ -run
TestCacheBlocksFromStructuredBuilder` — expect FAIL against current :1184 logic.

**Task 2 — fix classification:** replace the `strings.Split(systemPrompt,
"\n\n")` + `ClassifyPromptSections` path with structured blocks carried through
from the caller (thread the already-built blocks through the request-assembly
path; if the provider only receives a flat string, add an optional
`SystemBlocks []SystemBlock` field on the request config that the client populates
when present, falling back to the string path when absent). Delete the
blank-line re-split.

**Task 3 — decide Session-block cache_control:** Anthropic allows up to 4
breakpoints; a breakpoint on the trailing Session block makes the growing tail
cacheable across turns. Emit cache_control on the Session block too, AFTER the
Static block, when prompt_cache is enabled (total 2 breakpoints — within limit).
Cover in the test from Task 1.

**Task 4 — full package:** `go test -p 2 ./internal/llm/` — all green, and
`go build ./...`.

**Task 5 — report.** Do NOT commit.

## Self-Verification Checklist
- [ ] No blank-line re-split remains in the cache classification path
- [ ] Static AND Session blocks carry cache_control when enabled (2 breakpoints)
- [ ] Existing anthropic tests (incl. streaming/refusal suites) green

## Review Checklist (for orchestrator)
- [ ] Wire format verified: system array marshals as before, cache_control placement per Anthropic docs
- [ ] Fallback string path still works for providers/paths that never build blocks
