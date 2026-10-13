# L01 — Prefix-Aware Response-Cache Keys (P2)

**Proposal:** P2. **Depends on:** none. **Files (2):**
- Modify: `internal/llm/client.go` (lookup/put path, ~:557-561)
- Modify: `internal/llm/cache_key_builder.go` (no signature change — wiring only)
- Test: `internal/llm/client_usage_store_test.go` OR new `internal/llm/client_prefix_cache_key_test.go`

**Problem:** The live client cache path keys on a hash of the ENTIRE message
list (`c.keyBuilder.Build("", cfg.ModelID, messages)` → `ComputePromptHash`,
cache_key_builder.go:190-196). In an appending conversation the hash changes
every turn, so only byte-identical re-asks hit. `BuildPrefixAwareKey`
(cache_key_builder.go:271-279) already produces
`staticHash[:16]+":"+dynamicHash[:16]+":"+messagesHash[:16]` but is test-only.

**Contract (root #1):** live Get/Put use the prefix-aware key. Old L2 rows age
out via TTL; no migration. Cache stays disabled unless `[llm.cache] enabled` —
this leaf changes NO default.

### Tasks

**Task 1 — failing test:** write `TestClientCacheUsesPrefixAwareKey`:
construct a client with an enabled L1 cache; call the chat path twice with a
SHARED system prompt but different trailing user message; assert the second
call's cache lookup key equals `BuildPrefixAwareKey(...)` output (inspect via a
keyBuilder test hook or by asserting cache Stats: second identical re-ask with
same messages must HIT, while a messages-differ call must MISS but share the
same static component). Run: `go test -p 2 ./internal/llm/ -run
TestClientCacheUsesPrefixAwareKey` — expect FAIL.

**Task 2 — wire it:** in client.go replace the cacheKey construction at the
lookup/put site with `c.keyBuilder.BuildPrefixAwareKey("", cfg.ModelID,
messages)`. If BuildPrefixAwareKey needs the system prompt split, use
`ClassifyPromptSections` (prompt_cache.go:37) — do NOT re-implement section
splitting. Keep file-aware keying behavior intact (client.go:306-314).

**Task 3 — regression:** run `go test -p 2 ./internal/llm/` full package.
Expect PASS, zero regressions in cache_key_builder_test.go.

**Task 4 — report:** list changed files + test output. Do NOT commit.

## Self-Verification Checklist
- [ ] Prefix-aware key used at BOTH Get and Put sites
- [ ] No default flipped: cache still requires `[llm.cache] enabled`
- [ ] Full internal/llm package green

## Review Checklist (for orchestrator)
- [ ] Contract 1 satisfied; no key-format change visible to L2 schema
- [ ] No debug artifacts; gofmt clean
