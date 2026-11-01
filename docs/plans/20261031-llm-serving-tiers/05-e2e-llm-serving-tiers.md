# E2E Suite: llm-serving-tiers (Tier Policy + Semantic Cache + Config + TTFT) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** master.md
- **Scope:** Hermetic e2e suite `llm-serving-tiers` covering tier depth ceilings + degraded-tier metadata, semantic cache hit/exemption/degradation, --cache-reuse config load, and ttft_ms persistence; plus the manifest entry.
- **Dependencies:** 01-tier-policy.md, 02-semantic-cache.md, 03-cache-reuse-config.md, 04-ttft-metrics.md (ALL must be REVIEWED first)
- **Estimated Context:** 90K
- **Concurrency Group:** D
- **Audit references:** proposal 9 + proposal 7 (integration coverage)

## Goal

The repo's e2e policy requires new feature tests in the hermetic e2e tier.
This leaf lands ONE suite, e2e/suites/llm-serving-tiers/, exercising the
four features end-to-end through the daemon harness with stub
OpenAI-compatible endpoints and a stub embedder dependency shape, plus the
manifest entry so `make e2e-affected` selects it for internal/llm/,
internal/config/, and internal/metrics/ changes.

## Context

- Suite pattern: e2e/suites/<name>/ with the e2e build tag; manifest +
  path->suite mapping in e2e/manifest.json. Read
  docs/workflows/e2e-testing.md (terminal cat) FIRST for tiers, manifest
  format, and the coverage rule.
- Best in-repo example of a custom-topology suite:
  e2e/suites/session-affinity/ (helpers_test.go + *_test.go) - it boots the
  harness-built binary against its OWN two-provider stub topology instead
  of harness.Start's single shape. This suite needs the same pattern
  (multi-alias stub topology + a stub embedder-capable endpoint).
- The scratch-bin harness builds the daemon once per test process into
  $TMPDIR/meept-e2e-bin* with a detached reaper - do NOT create your own
  os.MkdirTemp bin dirs and do NOT spawn raw unmanaged daemons.

## Verified Anchors

- e2e/suites/session-affinity/ - custom-topology harness pattern to copy.
- e2e/manifest.json - suites[] entries {name, dir, status}; path_map
  prefixes -> suite names.
- e2e/harness/daemon.go - scratch-bin build + reaper (use the harness; never raw exec).
- internal/llm AGENTS.md - the health-checker canary never routes through
  the LLM client (stub endpoints must answer /health themselves).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e/suites/llm-serving-tiers/ (package llmservingtiers, //go:build e2e)
  helpers_test.go      - stub topology builders (multi-alias OpenAI-compat
                         stubs; stub embedder endpoint shape per leaf 02's
                         adapter)
  tier_policy_test.go  - depth ceiling, precedence regression, metadata keys
  semantic_cache_test.go - paraphrase hit, tool-call exemption, embedder
                         failure degradation, disabled-identical run
  config_ttft_test.go  - models.json5 --cache-reuse load; ttft_ms row
                         populated (>= 0) for a streaming call, -1 otherwise
e2e/manifest.json      - suites[] entry {name: "llm-serving-tiers", dir:
                         "e2e/suites/llm-serving-tiers", status:
                         "implemented"} + path_map additions routing
                         internal/llm/, internal/config/,
                         internal/metrics/ (append the suite name to the
                         existing arrays; do not remove existing mappings)
```

### What This Leaf Consumes

- C1-C7 from master.md (the four feature leaves' contracts) - test against
  the OBSERVABLE shapes: config keys, metadata keys, error classification
  (errors.Is on the daemon-visible shape), DB column contents.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Suite skeleton + manifest: suite dir with helpers (multi-alias stub topology builder over the session-affinity pattern), manifest suites[] entry, path_map additions for internal/llm/, internal/config/, internal/metrics/ | Create: e2e/suites/llm-serving-tiers/helpers_test.go; Modify: e2e/manifest.json | `go test -p 2 -tags e2e ./e2e/suites/llm-serving-tiers/ -run TestHelpersSanity` (a trivial sanity test proving the topology boots) | suite dir absent | sanity green; manifest parses (`python3 -c "import json;json.load(open('e2e/manifest.json'))"`) | Given the stub topology builder; When a test boots the daemon against it; Then /health answers and one chat turn completes |
| T2 | Tier policy scenarios: (a) alias chain of 3 stub members all returning empty responses with llm.tiers max_depth=2 -> turn fails classified at the floor (never dispatches member 3 more than the policy allows); (b) succeed-on-member-2 topology -> completed turn whose step metadata carries llm_tier / llm_tier_degraded=true / llm_tier_depth=1; (c) precedence regression: member-1 quota-blocked -> skipped with no dispatch and depth 0, member-2 serves with llm_tier_degraded=false | Create: e2e/suites/llm-serving-tiers/tier_policy_test.go | `go test -p 2 -tags e2e ./e2e/suites/llm-serving-tiers/ -run TestTierPolicy` | features absent -> scenarios fail | all three green | Given each topology; When a chat turn runs; Then the observable outcomes match (a) the classified floor failure, (b) the three metadata keys with exact values, (c) member-1 skipped + degraded=false on the primary-served turn |
| T3 | Semantic cache scenarios: (a) stub embedder endpoint + enabled llm.cache.semantic -> paraphrase repeat of an earlier prompt hits semantically (second turn served without the stub model being re-consulted for a fresh completion - assert via stub request counter); (b) prior response carrying tool calls is NEVER served semantically (stub returns a tool-call response, then a paraphrase prompt -> fresh dispatch, not the cached tool-call reply); (c) embedder endpoint returns 500 -> turn completes normally via the live path (exact-only degradation, no error, no park); (d) semantic disabled (default config) -> repeat prompts produce TWO fresh model dispatches (exact-only unchanged) | Create: e2e/suites/llm-serving-tiers/semantic_cache_test.go | `go test -p 2 -tags e2e ./e2e/suites/llm-serving-tiers/ -run TestSemanticCache` | features absent -> scenarios fail | all four green | Given each stub configuration; When the prompt sequence runs; Then (a) request count shows the second turn skipped a fresh completion, (b) the paraphrase triggers a fresh dispatch, (c) the turn completes with the live response, (d) two dispatches occur |
| T4 | Config + TTFT scenarios: (a) models.json5 fixture carrying --cache-reuse 256 boots through the harness (proves loader compatibility); (b) a streaming turn against a stub that emits multiple deltas -> query the llm_calls store (sqlite via the daemon's data dir) and assert ttft_ms >= 0 on the newest row; (c) a non-streaming or cache-hit turn -> ttft_ms == -1 | Create: e2e/suites/llm-serving-tiers/config_ttft_test.go | `go test -p 2 -tags e2e ./e2e/suites/llm-serving-tiers/ -run TestConfigAndTTFT` | features absent -> scenarios fail | all three green | Given each fixture; When the turns run; Then (a) the daemon boots, (b) the newest llm_calls row has ttft_ms >= 0, (c) it has ttft_ms == -1 |

**Worked example (T2b shape):**

1. Build the topology: stub server with two alias members; member 1 returns
   HTTP 200 with an empty completion; member 2 returns a normal completion.
   Config fixture enables llm.tiers (max_depth 2, signal_degraded true).
2. Drive one chat turn through the harness; read the turn's step metadata
   via the daemon API surface the session-affinity suite uses for its
   assertions.
3. Assert llm_tier == member-2 model id, llm_tier_degraded == true,
   llm_tier_depth == 1.
4. Verify: `go test -p 2 -tags e2e ./e2e/suites/llm-serving-tiers/ -run TestTierPolicy`
   Expected: PASS.

## Self-Verification Checklist

- [ ] All scenarios implemented and passing (`go test -p 2 -tags e2e` on the suite)
- [ ] Manifest entry + path_map additions present and JSON-valid
- [ ] Uses the harness scratch-bin rig (no raw exec daemons, no ownerless MkdirTemp)
- [ ] Stub endpoints answer /health themselves (canary never routes through the LLM client)
- [ ] Assertions target observable shapes (metadata keys, request counters,
      sqlite column values), not internal function calls
- [ ] Every feature leaf (01-04) has at least one scenario exercising it
- [ ] gofmt clean; ASCII only in every touched file
- [ ] No scope creep; no deviations (or deviations documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [suite run; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Suite + manifest entry exactly as contracted; manifest JSON parses
- [ ] All scenarios pass; suite runs hermetically (no network, no real models)
- [ ] Custom topology follows the session-affinity pattern (own configs, harness binary)
- [ ] Feature coverage complete: tier ceiling, metadata, precedence
      regression, semantic hit/exemption/degradation/disabled,
      cache-reuse load, ttft_ms >= 0 and -1 cases
- [ ] No scratch-bin leaks; ASCII only; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- Run with -p 2 (macOS ephemeral-port discipline) even for a single suite.
- If a scenario exposes a REAL bug in a feature leaf, do NOT fix the
  implementation here - report it in the completion report; the
  orchestrator re-dispatches the owning leaf.
- Stub the embedder as an OpenAI-compatible /v1/embeddings endpoint only
  if leaf 02's adapter speaks that shape; otherwise mirror whatever
  in-process adapter leaf 02 shipped and say which in the report.
- Keep scenarios independent (each test builds its own topology) so -run
  filters work.
