# Skill Rewrite and Workflow Docs - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Do NOT commit — the orchestrator handles
> all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After
> writing a file, do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Rewrite learn-from-video step 2 to the real (file-backed)
  workflow; document output_path and summarize in both workflow docs.
  Docs describe the CONTRACTED behavior of leaves 01+02 (this leaf
  ships in parallel with them; the orchestrator reconciles any drift at
  review).
- **Dependencies:** none (contracts in master.md are frozen)
- **Estimated Context:** 25K
- **Concurrency Group:** A — another agent concurrently edits
  internal/tools/builtin/transcript_fetch*.go and
  internal/config/schema.go. Do NOT touch any .go files.

## Goal

The skill currently promises "summarize it in overlapping ~40k chunks"
— impossible: tool results cap at ~9k chars and the 30k-token iteration
window evicts older pages mid-turn. This leaf makes the shipped skill
teach the workflow the architecture actually supports, and documents the
two new transcript_fetch knobs where operators will look for them.

## Context

The learn-from-video skill (config/skills/learn-from-video/SKILL.md) is
parsed by internal/skills and guarded by
internal/skills/learn_from_video_catalog_test.go, which asserts: name,
non-empty description/body, sections "when to use", "workflow",
"decision rules", "verification", references to transcript_fetch +
skills_create + skills_patch, and the "SHOW THE DRAFT" confirmation
step. Keep all of those. The test asserting the OLD chunking text (grep
it first) must be UPDATED by this leaf to assert the NEW workflow
markers instead — this leaf owns that test file.

Key files:
- config/skills/learn-from-video/SKILL.md
- internal/skills/learn_from_video_catalog_test.go (update assertions)
- docs/workflows/external-integrations.md (transcript section)
- docs/workflows/skills.md (composition-flow section)

## Interface Contracts (From Parent)

```
// SKILL.md workflow changes (frontmatter unchanged):
//   step 1: transcript_fetch with the URL; for learn workflows add
//           output_path=<path in the session working dir>
//   step 2 REPLACED: "Read the transcript file with file_read in
//           ~400-line slices (offset/limit). After each slice, write
//           structured notes: steps, decision rules, tool/API names,
//           numbers, and the WHY. Keep notes short enough to hold the
//           whole video's notes in mind at synthesis (~2-4k chars)."
//   step 2.5 (new bullet between 2 and 3): "If the user asks to
//           'describe the shape' / summarize the video rather than
//           learn it, call transcript_fetch with summarize=true
//           instead and work from the digest; page the file only for
//           verbatim detail."
//   steps 3-6 unchanged in intent; step 3's "chunk" phrasing (if any)
//           now refers to notes, not raw transcript
//   decision rules: add "Full transcript needed verbatim -> output_path
//           + file_read paging. Gist only -> summarize=true."
//
// docs/workflows/external-integrations.md — after the existing config
// table, two new subsections:
//   "File-backed output": output_path param — resolution order
//   (session working dir -> ~/.meept/media fallback), full text on
//   disk, bounded preview + path in the result, paging via file_read
//   "Local summarization": summarize param + [transcript]
//   summarize_enabled / summarize_model keys — map-reduce inside the
//   tool via the summarizer model (summarizer_model -> small_model
//   chain), ~4k digest in the result, full text still written when
//   output_path is also given; disabled/unconfigured -> explicit
//   config error
//
// docs/workflows/skills.md — "the composition flow" section: update
// any chunk-summarization phrasing to the file-backed workflow
```

## Tasks

### Task 1: learn-from-video skill rewrite

**Files:**
- Modify: config/skills/learn-from-video/SKILL.md
- Modify: internal/skills/learn_from_video_catalog_test.go

**Step 1:** Update the catalog test FIRST: replace any assertion on the
old chunking text with assertions on the new markers — "output_path",
"file_read", "summarize=true" (or the exact phrasing you put in the
skill), while KEEPING the existing assertions (sections, tool refs,
SHOW THE DRAFT). Run: `go test ./internal/skills/ -run
TestLearnFromVideo -count=1` — FAIL (skill not yet updated).

**Step 2:** Rewrite SKILL.md per the contract. Keep lowercase section
conventions and the existing tone. Keep under ~90 lines.

**Step 3:** Run the test — PASS.

### Task 2: external-integrations doc

**Files:**
- Modify: docs/workflows/external-integrations.md

**Step 1:** Add the two subsections per the contract (after the
existing config-keys block). Behavior tables match the leaf-01/02
contracts verbatim where they describe observable behavior: resolution
order, write mode 0o644, preview + pointer line, digest cap 4000,
chunk_count/total_chars/path keys, the nil-chatter config error string.
Cross-link from skills.md.

### Task 3: skills.md composition flow

**Files:**
- Modify: docs/workflows/skills.md

**Step 1:** Update the composition-flow section: learn workflow =
output_path + file_read paging + notes; summarize=true for gist
requests. Remove/replace any "40k chunks" phrasing repo-wide in these
two docs (grep "40k" docs/ — the integration gate asserts zero hits).

### Task 4: Verification

- `go test ./internal/skills/ -run TestLearnFromVideo -count=1` — PASS
- `grep -rn "40k chunks" docs/ config/` — zero hits
- gofmt not applicable (no .go you authored besides the test edit —
  gofmt it if touched)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Skill teaches output_path paging as the primary learn workflow
- [ ] summarize=true positioned for gist/shape requests
- [ ] Catalog test updated BEFORE the skill rewrite (TDD) and green
- [ ] All pre-existing catalog assertions still present
- [ ] Docs match the master.md contracts (error strings, defaults)
- [ ] "40k chunks" gone from docs/ and config/
- [ ] No .go production files touched

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The skill must still refuse to improvise fallbacks when
  transcript_fetch errors (existing step 1 language) — keep it.
- Docs describe leaf-01/02 behavior as CONTRACT; if review finds drift,
  the orchestrator fixes docs or code, whichever is wrong, at
  integration.
