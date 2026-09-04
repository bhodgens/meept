# Graph Orphan Annotation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Annotate the known `agent.result` orphan publisher in the connectivity-graph generator and regenerate the graph docs.
- **Dependencies:** none
- **Estimated Context:** 10K
- **Audit references:** observation O3 (`bus: Publish with no subscribers topic=agent.result` recurring in meept.log)

## Goal

`agent.result` is published by the agent loop with no daemon-side
subscriber (the result reaches chat via chat.response, not this topic), so
every regeneration lists it as an orphan and the log warns on each publish.
Per AGENTS.md, intentional external-only surfaces get an entry in
`annotated_orphans` (scripts/gen-connectivity-graph.py:701) instead of code
changes. This leaf adds the entry and regenerates.

## Context

The generator is a standalone Python script run by `make graphs`; CI runs
`make graphs-check` to verify docs/generated freshness. The annotation map
sits at scripts/gen-connectivity-graph.py:701-712 with two existing entries
as style examples.

Key files to understand before implementing:
- scripts/gen-connectivity-graph.py - annotated_orphans map (line 701)
- docs/generated/bus-topology.md - regenerated output (do not hand-edit)
- Makefile - graphs / graphs-check targets

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// scripts/gen-connectivity-graph.py annotated_orphans gains:
//   "agent.result": "<reason>"  — reason documents: published by agent
//   loops on RunOnce completion; results reach the chat path via
//   chat.response; topic is an external/debug surface (TUI event taps),
//   no daemon subscriber by design.
// docs/generated/* regenerated via make graphs.
// Owner: 09. Consumers: make graphs-check CI.
```

### What This Leaf Consumes

```
// make graphs / make graphs-check
```

## Tasks

### Task 1: annotation entry

**Objective:** Add the map entry in the established style.

**Files:**
- Modify: `scripts/gen-connectivity-graph.py:701-712`

**Step 1: Confirm current state**

Run: `grep -n 'agent.result' scripts/gen-connectivity-graph.py docs/generated/bus-topology.md | head -5`
Note whether agent.result currently appears in the orphan lists.

**Step 2: Edit** — inside annotated_orphans add:

```python
        # Agent loops publish run results on agent.result; the chat reply
        # path uses chat.response instead. The topic is an external/debug
        # tap (TUI event subscriptions), so no daemon subscriber is expected.
        "agent.result": "external/debug tap; chat replies flow via chat.response",
```

**Step 3: Verify**

Run: `make graphs && make graphs-check`
Expected: regeneration succeeds; graphs-check exits 0; agent.result no
longer in published_not_subscribed (grep the md/json to confirm).

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] Entry added in established style with a reason string
- [ ] `make graphs && make graphs-check` both succeed
- [ ] docs/generated/* updated by the regen (not hand-edited)
- [ ] No scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Only the annotation map changed in the script
- [ ] Generated docs refreshed and consistent
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- If `make graphs` regenerates unrelated diffs (drift from other sessions),
  report it — the orchestrator decides whether to commit the drift
  separately BEFORE this leaf's commit.
