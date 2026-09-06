# Skill Page Size and Docs - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Do NOT commit — the orchestrator handles
> all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. Write each
> file once; do not read it back.

## Meta

- **Parent:** master.md
- **Scope:** Align learn-from-video's paging size with the budget floor
  and document the floor + metadata-preservation behavior.
- **Dependencies:** 01-result-floor-and-map-keys.md COMMITTED (docs
  describe its behavior; the catalog test may pin slice sizes)
- **Estimated Context:** 20K
- **Concurrency Group:** B (alone)

## Goal

The 600-token budget floor (~1800 chars) can still clip a ~400-line
(~9k-char) file_read slice late in a turn. Whole pages that fit under
the floor are immune. This leaf shrinks the skill's slice guidance to
~2k chars and documents the floor mechanics operators will ask about.

## Context

learn-from-video step 2 currently says "~400-line slices" with
"~2-4k chars" notes guidance (config/skills/learn-from-video/SKILL.md).
The catalog test (internal/skills/learn_from_video_catalog_test.go)
asserts markers including possibly the slice phrasing — grep it first.
Docs: docs/workflows/external-integrations.md transcript sections
(File-backed output / Local summarization).

Key files:
- config/skills/learn-from-video/SKILL.md
- internal/skills/learn_from_video_catalog_test.go (only if it pins
  the old numbers)
- docs/workflows/external-integrations.md

## Interface Contracts (From Parent)

```
// SKILL.md step 2: "~400-line slices (offset/limit)" becomes
//   "~50-line slices (offset/limit) — keep each request under ~2k
//   chars so a whole slice survives even when the agent's tool-result
//   budget is at its floor". The notes-per-slice guidance (~2-4k chars
//   cumulative) stays.
// external-integrations.md: one sentence added to the File-backed
//   output section: "Tool results are never compressed below a tool's
//   declared floor — transcript_fetch declares 1400 tokens (~4200
//   chars), so a digest or preview arrives whole; non-content
//   metadata keys (path, offset, total_chars, chunk_count) always
//   survive compression intact."
// Catalog test: update any assertion pinning "400-line" to the new
//   phrasing (grep first; if it doesn't pin it, no test change).
```

## Tasks

### Task 1: Skill slice-size update

grep the catalog test for "400-line"/slice assertions; update test
FIRST if pinned (TDD: red), then edit SKILL.md per the contract
(~50-line / ~2k-char slices + floor rationale). Run
`go test ./internal/skills/ -run TestLearnFromVideo -count=1` green.

### Task 2: Docs sentence

Add the floor sentence to external-integrations.md per the contract
(verbatim semantics; exact wording may be adjusted for flow — the
numbers 1400 tokens / ~4200 chars and the metadata-key list are
contract).

### Task 3: Verification

- catalog test green
- `grep -rn "400-line" config/ docs/` → zero hits (or only the plan
  doc's own self-references)
- gofmt the test file if touched

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Slice guidance fits the 600-token floor (~2k chars per request)
- [ ] Floor sentence numbers match leaf-01 reality (1400 tokens,
      ~4200 chars; metadata keys named)
- [ ] Catalog test green; TDD order if the test pinned old numbers
- [ ] No .go production files touched

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- ~50 lines ≈ 2k chars is a heuristic for transcript text; the skill
  should say "~2k chars (about 50 lines)" so agents measure chars, not
  lines.
