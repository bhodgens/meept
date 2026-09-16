# Docs and Config Templates - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD-style verification (docs: verify
> cross-references resolve). Do NOT commit — the orchestrator handles all
> git operations after review. Do NOT use read_file on existing source
> files — explore with search_files or terminal cat. After writing a file,
> do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Documentation and config template updates for refusal_model.
- **Dependencies:** 02-refusal-model-slot.md (slot exists)
- **Estimated Context:** 35K
- **Concurrency Group:** C

## Goal

The feature is discoverable and configurable: models.json5 template gains
the slot, the docs gain the workflow page section and reference entries,
and AGENTS.md documents the new cross-boundary invariant (refusals are not
alias failures) per its own maintenance rule.

## Context

Meept requires doc updates in the same change as feature code
(AGENTS.md "Feature Documentation Requirements"). Config ships via
templates in `config/` merged to ~/.meept by make install / sync-config.

Key files to understand before implementing:
- config/models.json5 - the shipped template (search for extract_model to
  find the slots block)
- docs/workflows/security.md and docs/reference/generated/security.md -
  where tirith (command scanning) is documented; the refusal fallback is a
  DIFFERENT layer (model selection), document it in the LLM/config docs
- docs/configuration/ - models.json5 reference pages
- AGENTS.md - Critical Invariants (quota section is the model to follow)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Documentation only. Files (exact set):

1. `config/models.json5` - add commented slot next to extract_model:
   `"refusal_model": ""` with a 2-line comment: provider/id or alias name
   the agent re-dispatches to when the serving model refuses; empty = off.
2. `docs/workflows/llm-refusal-fallback.md` - NEW page (~60 lines):
   what triggers a fallback (typed signals only: finish_reason
   content_filter, stop_reason refusal, typed safeguard bodies), the one-hop
   rule, the agent.model_escalated event with reason refusal_fallback, the
   config slot, and the explicit NON-goal (no content sniffing — cite the
   false-positive failure mode this avoids).
3. `docs/configuration/` models reference page (find the page documenting
   extract_model via search_files) - add refusal_model to the slot table.
4. `AGENTS.md` - under Critical Invariants, inside the quota-resilience
   section's list, add one bullet: "Refusal is not a failure. A
   *llm.RefusalError must never reach Resolver.RecordAliasFailure; the
   loop's refusal branch re-dispatches once to models.json5 refusal_model
   (default off) and surfaces the refusal if the fallback also refuses."
   Plus: add the new docs page to the appropriate docs index if one exists.
5. `docs/workflows/` index (if the workflows dir has an index/README) -
   link the new page.

### What This Leaf Consumes

Final behavior from leaves 01-04 (write docs to match what WAS built; if a
detail differs from this spec, the CODE is the truth — flag the mismatch
in the report).

## Tasks

### Task 1: Config template + configuration reference

**Objective:** Slot discoverable in the shipped template and reference docs.

**Files:** config/models.json5; the docs/configuration slot-reference page.

**Step 1:** Read (terminal cat / search_files) the slots block and slot
table.

**Step 2:** Edit both files.

**Step 3:** Verify: `grep -n refusal_model config/models.json5 docs/configuration/*.md`
shows both edits; JSON5 still parses — `./bin/meept config get models.refusal_model`
returns empty string (build exists from make build) OR validate with the
project's json5 loader test if the binary is absent.

### Task 2: Workflow page

**Objective:** The feature has its spec page.

**Files:** docs/workflows/llm-refusal-fallback.md (new).

**Step 1-3:** Write per contract; verify all cross-referenced paths exist
(loop_refusal.go, errors_refusal.go, verification_escalation.go topic).

### Task 3: AGENTS.md invariant + index links

**Objective:** The invariant is captured where agents read it.

**Files:** AGENTS.md; docs index files per contract.

**Step 1-3:** Edit; verify the bullet sits in the quota-resilience list
and markdown links resolve (`grep -rn "llm-refusal-fallback" docs/` shows
page + links).

## Self-Verification Checklist

- [ ] All five file targets edited/created
- [ ] Cross-references resolve (every path named in the new docs exists)
- [ ] config/models.json5 still valid (parse check)
- [ ] No contradiction with AGENTS.md's existing invariants
- [ ] Hyphens used in prose, not em-dashes (project copy convention)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Docs describe the BUILT behavior (checked against leaves 01-04 code)
- [ ] The invariant bullet matches the enforcement in loop_refusal.go
- [ ] No new docs in docs/generated/ (auto-generated; make graphs owns it)
- [ ] Copy uses hyphens, short sentences, no marketing words

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The docs/reference/generated/ pages are mage/gomarkdoc generated — do
  NOT hand-edit; `make docs-generate` at integration (orchestrator's job,
  needs mage + gomarkdoc installed; skip with a note if tools are absent).
- make graphs-check must pass at integration; the tree adds no bus topics
  so no graph diff is expected.
