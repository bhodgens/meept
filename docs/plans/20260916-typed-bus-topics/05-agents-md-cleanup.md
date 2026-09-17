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

AGENTS.md is the repo's standing instruction set for AI coding agents - its own header says it must be validated on every commit and that stale guidance causes bugs. After leaves 01-04, three things in it are stale: the manual "verify they land in the correct bucket" instructions for WS events, the absence of typed topics from the package table, and the absence of a typed-topic invariant. This leaf fixes exactly those three things and nothing else.

## Context

AGENTS.md is ~45K chars at repo root. Relevant regions (locate with grep):

- "### WS event type classification" - paragraphs on quota (`agent.quota` prefix), model_escalated, and turn. topics, each ending in instructions for future authors to verify bucket placement; plus the `chat.response` exclusion note (KEEP) and the employee.* note (KEEP - it is a classification statement, still true via fallback).
- "Key Components" table (Architecture Overview) - Server/Bus rows.
- "Critical Invariants" - where the new invariant paragraph goes.

The master.md Interface Contracts section (Contract 6) holds the exact edit list. AMENDED FACTS the new text must reflect (verified 2026-09-16):
- Only `turn.terminal` is typed today (`TopicTurnTerminal` in internal/agent/topics.go on the frozen TurnTerminalEvent).
- `agent.quota_wait` is deliberately RAW (polymorphic payload: QuotaEvent, ParkTurnEvent, job-level map).
- WS classification: marker-first for payloads with `WSClass()` (internal/comm/wsclass, six classes), prefix table as legacy fallback, `exhaustive` linter enforces switch coverage in CI.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly three AGENTS.md edits:

1. **WS event type classification section**: replace the per-topic manual-verification sentences (quota/model_escalated/turn paragraphs' trailing "future agents must verify" instructions) with a statement that classification derives from each payload's `WSClass()` marker (see `internal/comm/wsclass`) where the payload is typed, with the topic-prefix table as legacy fallback for map-shaped payloads, enforced by the `exhaustive` linter in CI; new WS-visible payload types implement the marker. KEEP verbatim: the `chat.response` exclusion sentence, the employee.* leaf-11 note, and the quota_wait polymorphism note (add one clause: quota_wait is intentionally raw - it carries multiple payload shapes).
2. **Key Components table**: mention `internal/bus/topic.go` (Topic[T], PublishT/SubscribeT) and `internal/comm/wsclass` on the Server/Bus rows.
3. **Critical Invariants**: add the typed-topic invariant paragraph, including scope discipline - "New bus topics with stable, single-shape payloads are declared as `bus.Topic[T]` vars beside their payload structs (see internal/agent/topics.go); publishers use `bus.PublishT`, subscribers use `bus.SubscribeT`. Raw string Publish/Subscribe remains for wildcard subscriptions, request/response bus patterns (chat.request/chat.response), and multi-shape/open-ended topics (agent.quota_wait carries QuotaEvent, ParkTurnEvent, and job-level map payloads - deliberately raw). Do not wrap `map[string]any` in a Topic; that is ceremony without a guarantee."

### What This Leaf Consumes

- Committed state of leaves 01-04. Before editing, verify every claim against code (Task 1). If any check fails: report BLOCKED - the doc must not describe aspirational code.

## Tasks

### Task 1: Verify the described code exists (docs-only "confirm old state")

**Objective:** Every claim the new text makes is true on disk.

**Files:** none modified

**Step 1: Read current content**

- `terminal("grep -n 'WS event type classification' AGENTS.md")` then cat the section
- `terminal("grep -n 'chat.response' AGENTS.md")` and `terminal("grep -n 'employee' AGENTS.md")` - sentences to preserve
- Key Components table region; Critical Invariants region

**Step 2: Confirm old state (verification gate)**

- `grep -n 'func PublishT\|func SubscribeT\|type Topic\[' internal/bus/topic.go` - leaf 01 exists
- `grep -n 'TopicTurnTerminal' internal/agent/topics.go` - leaf 02 exists
- `grep -rn 'WSClass()' internal/ --include='*.go' | grep -v _test | head` - leaf 03 exists
- `grep -n 'exhaustive' .golangci.yml` - leaf 04 exists
- `grep -n 'legacy fallback' internal/comm/http/server.go` - fallback comment present

**Step 3: Record findings**

Report: quoted manual-verification sentences to be deleted; confirmation output for each new claim.

**Step 4: Halt condition**

Any failure -> report BLOCKED with the failing grep.

### Task 2: Apply the three edits

**Objective:** Contract edits, surgical, nothing else.

**Files:**
- Modify: `AGENTS.md` (three regions only)

**Step 1: Prepare exact old/new strings**

From Task 1 captures: exact current text per region + exact replacement (per Contracts, with the amended facts). Preserve surrounding markdown byte-for-byte.

**Step 2: Confirm old state**

Re-grep each region immediately before editing (the file is dirty from unrelated work; if it changed since Task 1, re-read and re-derive).

**Step 3: Write implementation**

Three targeted `patch` operations, one per region. No reflowing, no drive-by improvements.

**Step 4: Verify**

- `grep -c 'verify they land in the correct bucket' AGENTS.md` -> 0
- `grep -c 'chat.response' AGENTS.md` -> unchanged count (preserved)
- `grep -n 'internal/bus/topic.go\|internal/comm/wsclass' AGENTS.md` -> present
- `grep -n 'map\[string\]any' AGENTS.md` -> present (scope-discipline sentence)
- `git diff AGENTS.md` -> every new hunk is one of your three edits (pre-existing unrelated hunks untouched)

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] Three edits applied; manual-verification sentences gone (count 0)
- [ ] chat.response exclusion and employee.* note preserved
- [ ] New invariant includes: Topic[T] beside payload structs, PublishT/SubscribeT, raw-path exceptions (wildcards, request/response, quota_wait polymorphism), map[string]any prohibition
- [ ] No other AGENTS.md changes; pre-existing dirty hunks untouched
- [ ] Every factual claim verified against committed code in Task 1

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Diff shows exactly three new hunks (plus untouched pre-existing ones)
- [ ] Marker-derivation statement names internal/comm/wsclass + exhaustive
- [ ] quota_wait documented as intentionally raw with its three shapes
- [ ] No aspiration: every claim verified in Task 1
- [ ] Markdown structure intact

Output: APPROVED or specific gaps with file + line references.

## Notes

- This leaf makes the new mechanism discoverable to every future agent - the invariant text is load-bearing; do not paraphrase loosely.
- If leaf 02 placed topics.go somewhere other than internal/agent/topics.go, the master tracking table records reality - reality wins over contract wording.
