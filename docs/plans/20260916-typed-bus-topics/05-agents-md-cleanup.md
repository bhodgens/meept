# AGENTS.md Cleanup - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. This is a DOCS-ONLY leaf: no code, no tests to
> run - verification is cross-reference checks. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use read_file
> on AGENTS.md - use terminal cat or search_files. After writing, do NOT
> read the file back to verify - write once and stop. Note: AGENTS.md has
> UNRELATED pre-existing modifications in the worktree (branch
> classifier-iteration); do not revert, reformat, or touch any hunk outside
> your three targeted edits.

## Meta

- **Parent:** master.md
- **Scope:** Update AGENTS.md to reflect typed topics and marker-based WS classification; delete the now-obsolete manual-verification instructions.
- **Dependencies:** 01-04 all COMPLETE (the code the doc describes exists and is committed)
- **Estimated Context:** 15K
- **Concurrency Group:** E

## Goal

AGENTS.md is the repo's standing instruction set for AI coding agents - the file's own header says it must be validated on every commit and that stale guidance causes bugs. After leaves 01-04, three statements in it are stale: the manual "verify they land in the correct bucket" instructions for WS events, the absence of typed topics from the package table, and the absence of a typed-topic invariant. This leaf fixes exactly those three things and nothing else.

## Context

AGENTS.md is ~45K chars at repo root. Relevant regions (locate with grep, do not assume line numbers):

- "### WS event type classification" - contains the quota/model_escalated/turn. prefix paragraphs each ending in instructions for "future agents adding new bus topics"
- "Key Components" table (Architecture Overview) - the layer/package table where Server/Bus rows live
- "Critical Invariants" - section where a new invariant paragraph goes

The orchestrator's master.md Interface Contracts section (Contract 6) contains the exact edit list - follow it verbatim.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly three AGENTS.md edits (full text in master.md Contract 6):

1. WS event type classification section: remove the three manual-verification sentences; replace with marker-derivation statement (classification from `WSClass()` in `internal/comm/wsclass`, enforced by `exhaustive` in CI); KEEP the `chat.response` exclusion sentence (still true - RPC reply, not relayed).
2. Key Components table: mention `internal/bus/topic.go` typed topics (Topic[T], PublishT/SubscribeT).
3. Critical Invariants: add the typed-topic invariant paragraph, including the scope-discipline sentence - raw path stays for wildcards, request/response bus patterns, and caller-defined payloads; do not wrap `map[string]any` in a Topic ("ceremony without a guarantee").

### What This Leaf Consumes

- Committed state of leaves 01-04 (the doc must describe what exists - before editing, verify each claim against the code: `grep -n 'func PublishT' internal/bus/topic.go`, `grep -rn 'WSClass()' internal/ --include='*.go' | head`, `grep -n 'exhaustive' .golangci.yml`. If any check fails, STOP and report BLOCKED - the doc must not describe aspirational code.)

## Tasks

### Task 1: Verify the described code exists (docs-only "confirm old state")

**Objective:** Every claim the new text makes is true on disk before writing it.

**Files:** none modified

**Step 1: Read current content**

- `terminal("grep -n 'WS event type classification' -A 40 AGENTS.md")` - the section as it stands
- `terminal("grep -n 'chat.response' AGENTS.md")` - locate the exclusion sentence to preserve
- Key Components table region; Critical Invariants region

**Step 2: Confirm old state (verification gate)**

Run the three greps from Context. Also confirm the prefix table in server.go still carries the legacy-fallback comment (it should - leaf 03 kept it): `grep -n 'legacy fallback' internal/comm/http/server.go`.

**Step 3: Record findings**

Your report lists: what the current section says (quote the three manual-verification sentences you will delete), the confirmed code evidence for each new claim.

**Step 4: Halt condition**

Any verification failure -> report BLOCKED with the failing grep. Do not edit the doc to match reality that does not exist.

### Task 2: Apply the three edits

**Objective:** Contract 6 edits, surgical, nothing else.

**Files:**
- Modify: `AGENTS.md` (three regions only)

**Step 1: Prepare exact old/new strings**

From Task 1's captures, write the exact current text of each of the three regions and the exact replacement text (from master Contract 6). Preserve surrounding markdown structure and the section's other content byte-for-byte.

**Step 2: Confirm old state**

Re-grep each region immediately before editing (the file is dirty in the worktree from unrelated work - if it changed since Task 1, re-read and re-derive).

**Step 3: Write implementation**

Apply with three targeted `patch` operations (old_string unique + exact, new_string per Contract 6). One edit per region. Do not reflow, reformat, or "improve" adjacent text.

**Step 4: Verify**

- `grep -c 'verify they land in the correct bucket' AGENTS.md` -> 0
- `grep -c 'chat.response' AGENTS.md` -> unchanged from Task 1 count (sentence preserved)
- `grep -n 'internal/bus/topic.go' AGENTS.md` -> present (table row)
- `grep -n 'map\[string\]any' AGENTS.md` -> present (invariant's scope-discipline sentence)
- `git diff --stat AGENTS.md` -> changes confined to the three regions (read the full diff; every hunk must be one of your three edits plus the pre-existing unrelated hunks which you must NOT have touched)

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All three edits applied per Contract 6, verbatim intent
- [ ] chat.response exclusion sentence retained
- [ ] The three deleted manual-verification sentences are gone (grep count 0)
- [ ] No other AGENTS.md changes (full diff reviewed)
- [ ] Pre-existing unrelated dirty hunks untouched
- [ ] Every factual claim in the new text verified against code in Task 1

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Diff shows exactly three new hunks (plus pre-existing ones, untouched)
- [ ] New invariant includes the raw-path exceptions (wildcards, request/response, caller-defined) and the map[string]any prohibition
- [ ] Marker-derivation statement names internal/comm/wsclass and the exhaustive linter
- [ ] No aspiration: every claim checked against committed code
- [ ] Markdown structure intact (table rows render, section headings unchanged)

Output: APPROVED or specific gaps with file + line references.

## Notes

- This leaf is the tree's cleanup deliverable - it is what makes the new mechanism discoverable to every future agent. The invariant text is load-bearing; do not paraphrase Contract 6 loosely.
- If master.md's tracking table records a topics-file location different from Contract 6's wording (leaf 02 chose pkg/models), adjust the path in the invariant text to match reality - reality wins over contract wording.
