# Docs and Config Templates - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD-style verification (docs: verify
> cross-references resolve). Do NOT commit — the orchestrator handles all
> git operations after review. Do NOT use read_file on existing source
> files — explore with search_files or terminal cat. After writing a file,
> do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Documentation + config templates + ALL FOUR capability surfaces (features.md, feature-comparison-matrix.md, meept.dev charts, README.md).
- **Dependencies:** 02-refusal-model-slot.md (slot exists)
- **Estimated Context:** 55K
- **Concurrency Group:** C

## Goal

The feature is discoverable and configurable: models.json5 template gains
the slot, the docs gain the workflow page and reference entries, AGENTS.md
documents the new invariant, AND the capability appears in all four
marketing/reference surfaces with an honest comparison against the 8
competitor harnesses (user requirement 2026-09-16).

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
- docs/features.md - the full feature reference (linked from meept.dev)
- docs/feature-comparison-matrix.md - parity matrix vs 8 competitors;
  "Model & Cost" section ~line 98-108
- meept.dev/index.html - the website's "Model & Cost" comparison chart,
  "Model failover chain" row ~line 549
- README.md - Models row ~line 66, LLM management summary ~line 293

## Interface Contracts (From Parent)

### What This Leaf Exposes

Documentation only. Files (exact set):

1. `config/models.json5` - add commented slot next to extract_model:
   `"refusal_model": ""` with a 2-line comment: global default refusal
   fallback (provider/id or alias); a per-agent spec refusal_model
   overrides it; empty = no default.
2. `docs/workflows/llm-refusal-fallback.md` - NEW page (~70 lines):
   what triggers a fallback (typed signals only: finish_reason
   content_filter, stop_reason refusal, typed safeguard bodies), the
   per-agent + global configuration with precedence (spec > global > off),
   the one-hop rule, the agent.model_escalated event with reason
   refusal_fallback, the reply-text disclosure note, and the explicit
   NON-goal (no content sniffing - cite the false-positive failure mode
   this avoids).
3. `docs/configuration/` models reference page (find the page documenting
   extract_model via search_files) - add refusal_model to the slot table.
4. `AGENTS.md` - under Critical Invariants, inside the quota-resilience
   section's list, add one bullet: "Refusal is not a failure. A
   *llm.RefusalError must never reach Resolver.RecordAliasFailure; the
   loop's refusal branch re-dispatches once to the agent's refusal_model
   (per-agent spec field, else the global models.json5 slot; default off)
   and surfaces the refusal if the fallback also refuses."
   Plus: add the new docs page to the appropriate docs index if one exists.
5. `docs/workflows/` index (if the workflows dir has an index/README) -
   link the new page.
6. `docs/features.md` - NEW "Refusal Fallback" subsection in the LLM/model
   management area (locate the model-alias/failover section via
   search_files "failover" in features.md). Cover: per-agent config,
   precedence, one-hop rule, disclosure note, typed-signals-only
   detection. Match the section style of its neighbors.
7. `docs/feature-comparison-matrix.md` - add ONE row to the "Model & Cost"
   matrix: `| Refusal fallback (per-agent) | X (typed signals, one-hop, reply disclosure) | ... |`.
   Competitor cells: "-" by default, BUT verify before writing any non-"-"
   mark: check each competitor's repo/docs (web_search) for an equivalent
   content-refusal-driven model fallback. The 2026-08-29 audit found none;
   refresh that check and cite evidence in the report for any X/~ given.
8. `meept.dev/index.html` - add ONE row to the "Model & Cost" comparison
   table after the "Model failover chain" row (~line 549), same label
   "Refusal fallback (per-agent)", Meept cell dot-yes (no cell-note
   needed), competitor cells dot-no (same verification rule as #7 - the
   dot classes are dot-yes / dot-no / dot-partial; mirror the failover
   row's markup exactly).
9. `README.md` - Models row (~line 66): Meept's cell gains
   "+ refusal fallback"; the LLM management summary row (~line 293)
   gains "refusal fallback" in the capability list. Match cell style.

### What This Leaf Consumes

Final behavior from leaves 01-04 (write docs to match what WAS built; if a
detail differs from this spec, the CODE is the truth - flag the mismatch
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

### Task 4: features.md capability subsection

**Objective:** The feature reference documents refusal fallback.

**Files:** docs/features.md.

**Step 1:** Locate the model-management section (search_files "failover"
in docs/features.md) and read its neighbor style (terminal sed, not
read_file, for the section range).

**Step 2:** Write the subsection per contract item 6.

**Step 3:** Verify section heading renders in the doc's structure (grep
the heading level matches siblings).

### Task 5: Comparison matrix + website chart + README (competitor comparison)

**Objective:** The capability appears in all three comparison surfaces
with honest, evidence-backed competitor marks.

**Files:** docs/feature-comparison-matrix.md; meept.dev/index.html;
README.md.

**Step 1:** For each of the 8 competitors (FrontierAgent, duckagent,
atomic-agent, prime-agent, Hermes, OpenCode, oh-my-pi, Claude Code),
verify via web_search + repo docs whether a refusal-driven model fallback
exists. Default "-"; any X/~ needs cited evidence in the leaf report.

**Step 2:** Add the row to the matrix (contract item 7), the chart row to
meept.dev/index.html (item 8, mirror the failover row markup), and the
README edits (item 9).

**Step 3:** Verify: `grep -n "Refusal fallback" docs/feature-comparison-matrix.md meept.dev/index.html README.md`
shows all three; the HTML row has 9 `<td>` cells matching the table.

## Self-Verification Checklist

- [ ] All nine file targets edited/created
- [ ] Cross-references resolve (every path named in the new docs exists)
- [ ] config/models.json5 still valid (parse check)
- [ ] No contradiction with AGENTS.md's existing invariants
- [ ] Hyphens used in prose, not em-dashes (project copy convention)
- [ ] Competitor marks evidence-backed (report cites sources for every non-"-")

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Docs describe the BUILT behavior (checked against leaves 01-04 code)
- [ ] The invariant bullet matches the enforcement in loop_refusal.go
- [ ] No new docs in docs/generated/ (auto-generated; make graphs owns it)
- [ ] Copy uses hyphens, short sentences, no marketing words
- [ ] features.md section matches neighbor style and depth
- [ ] Matrix row, HTML row, and README cells are consistent with each other
- [ ] HTML row markup mirrors the failover row exactly (9 cells, dot classes)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The docs/reference/generated/ pages are mage/gomarkdoc generated — do
  NOT hand-edit; `make docs-generate` at integration (orchestrator's job,
  needs mage + gomarkdoc installed; skip with a note if tools are absent).
- make graphs-check must pass at integration; the tree adds no bus topics
  so no graph diff is expected.
