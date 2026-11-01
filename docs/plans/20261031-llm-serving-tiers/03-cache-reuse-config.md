# KV-Cache Config: --cache-reuse 256 on llama-server Entries + Comment Correction - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** master.md
- **Scope:** Add `--cache-reuse 256` to both llama-server spawn_command entries in config/models.json5 and correct the WRONG "fresh cache per call" comment; update the user-facing doc.
- **Dependencies:** R0-research.md (findings doc Section 2: --cache-reuse semantics - READ FIRST)
- **Estimated Context:** 40K
- **Concurrency Group:** C
- **Audit references:** proposal 7 (KV-cache small pieces)

## Goal

llama.cpp prompt caching is ON by default (`--cache-prompt`); the existing
config comment claiming "llama.cpp builds a fresh cache per call" is WRONG.
This leaf adds explicit chunk-level KV reuse (`--cache-reuse 256`) to both
llama-server spawn entries and replaces the stale comment with the correct
semantics, citing the R0 findings doc. Config + docs only; no Go behavior
change beyond config parsing already exercised by existing tests.

## Context

- config/models.json5 defines local runtime spawn commands. Two
  llama-server entries exist: port 8084 (line ~282) and port 8080 with
  --jinja (line ~340). The stale comment sits at ~line 345.
- internal/llm/AGENTS.md documents upstream teardown (KV-cache erase via
  POST /slots/0?action=erase) - reuse and teardown are complementary, not
  conflicting; do not touch teardown.
- R0 findings Section 2 is the citation for what the 256 means (minimum
  chunk size in tokens for KV reuse).

## Verified Anchors

- config/models.json5:282 - llama-server spawn_command (port 8084) - gains
  the flag.
- config/models.json5:340 - llama-server spawn_command (port 8080, --jinja)
  - gains the flag.
- config/models.json5:~345 - WRONG comment "llama.cpp builds a fresh cache
  per call" - replaced.
- grep -c "cache-reuse" config/models.json5 == 0 today - verified absent.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// config/models.json5 - Contract C7
// BOTH llama-server spawn_command arrays gain: "--cache-reuse", "256"
// The stale comment (~:345) is REPLACED with: --cache-prompt is ON by
// default; --cache-reuse 256 enables chunk-level KV reuse (256-token
// minimum chunk) across calls sharing a prompt prefix.
// docs update: the affected doc page gains the same corrected statement.
```

### What This Leaf Consumes

- None from sibling leaves.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add "--cache-reuse", "256" to BOTH llama-server spawn_command entries (ports 8080 and 8084); keep JSON5 valid | Modify: config/models.json5 | `grep -c 'cache-reuse' config/models.json5` returns 2; `go test -p 2 ./internal/config/ -run TestConfigLoads` | flag absent (count 0) | count 2 + TestConfigLoads green | Given the two spawn entries; When the config loads; Then both arrays contain the flag in valid positions and the loader parses models.json5 without error (mind the existing TestConfigLoads collision-guard convention) |
| T2 | Replace the WRONG "fresh cache per call" comment with the corrected semantics (--cache-prompt default-ON; --cache-reuse 256 = chunk-level KV reuse, 256-token minimum chunk), citing R0 findings Section 2; ASCII only | Modify: config/models.json5 (comment only) | `grep -c 'fresh cache per call' config/models.json5` returns 0 | stale comment present | count 0; corrected comment present | Given the stale comment at ~:345; When this task lands; Then the phrase "fresh cache per call" is gone and the replacement comment states the default-ON prompt cache and the flag's chunk semantics |
| T3 | User-facing doc correction: update the local-runtime / models doc page that describes the llama-server entries with the same corrected statement (locate via search_files over docs/ for the spawn or llama-server reference) | Modify: the relevant docs/ page (exact path discovered in-task; note it in the report) | `grep -rn 'fresh cache per call' docs/ config/` returns nothing | stale doc text present (if present anywhere) | no stale phrase repo-wide; doc carries the corrected semantics | Given repo docs mention the llama-server cache behavior; When corrected; Then docs and config agree with llama.cpp reality and cite the flag |

**Worked example (T1 shape):**

1. Edit both spawn_command arrays: insert "--cache-reuse", "256" after the
   existing --ctx-size argument (port 8080) and at the end (port 8084) -
   argument order for llama-server flags is free; keep each array on its
   existing line-length conventions.
2. Verify JSON5 validity + loader: `go test -p 2 ./internal/config/ -run TestConfigLoads`
   Expected: PASS.
3. Verify count: `grep -c 'cache-reuse' config/models.json5` returns 2.

## Self-Verification Checklist

- [ ] Both llama-server entries carry --cache-reuse 256 (grep count 2)
- [ ] mlx_lm spawn entries untouched (only llama-server entries changed)
- [ ] Stale "fresh cache per call" phrase gone repo-wide (grep count 0)
- [ ] Corrected comment cites R0 findings Section 2 semantics
- [ ] TestConfigLoads (or equivalent config-load test) passes
- [ ] ASCII only in every touched file
- [ ] No scope creep - config + docs only, zero Go code changes
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [the greps + go test; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Both spawn entries carry the flag; no other entries touched
- [ ] JSON5 still valid (loader test green)
- [ ] Comment correction is factually right per R0 findings (default-ON
      --cache-prompt; chunk-level reuse)
- [ ] Doc page updated consistently
- [ ] ASCII only; no debug artifacts; no Go code touched

Output: APPROVED or specific gaps with file + line references.

## Notes

- Do NOT touch MLX (mlx_lm) spawn entries - they have no --cache-reuse
  equivalent.
- The R0 findings doc may note llama.cpp version sensitivity for the flag;
  if it does, include that caveat in the corrected comment.
- Keep the comment edit minimal - do not rewrite the surrounding
  ctx-size rationale block.
