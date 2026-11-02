# Docs + build + AGENTS.md - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> This is a DOCS/BUILD leaf: step 1 read current content, step 2 confirm
> old state, step 3 write content, step 4 verify cross-refs resolve and
> builds pass. Do NOT commit. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. Write once, do not
> read back.

## Meta

- **Parent:** master.md
- **Scope:** Feature doc refresh, generated docs, Makefile install target for cmd/meept-voice, AGENTS.md maintenance (root + internal/voice), CLI status surface stub decision, connectivity graphs
- **Dependencies:** leaf 15 (all code + suites landed)
- **Estimated Context:** 60K
- **Concurrency Group:** E (after 15)
- **Files this leaf owns:** docs/workflows/voice-sidecar.md (status flip + as-built deltas), docs/features.md row, Makefile (install target), AGENTS.md (root package table row), internal/voice/AGENTS.md (new), docs/reference/generated/* (regenerated), docs/generated/* (regenerated)

## Goal

The repo rule: a feature is incomplete without docs and wiring surfaces.
Flip the spec to Implemented with an as-built section recording every
deviation the completion reports named. Add the voice child to make
install. Write the package invariant doc. Regenerate both generated trees.

## Context

- AGENTS.md maintenance rule (root): package table gains internal/voice,
  internal/voicechild, internal/voiceframe, cmd/meept-voice; build
  commands gain the child; invariants gain the voice summary (child
  confinement: jargo only in child packages; secrets: token in daemon
  only; keys to child via socket; one live call per employee; supervisor
  fail-closed). Per-package internal/voice/AGENTS.md carries the full
  contract (frames, reorder, pacing, barge-in, release fan-out) and the
  root file carries the summary - same-commit rule.
- Makefile: `make install` builds+installs meept-voice next to
  meept-daemon (child lookup is daemon-adjacent, leaf 13 note);
  `make build` gains the child binary.
- docs-generate: schema.go changed (leaf 01) so
  docs/reference/generated/config.md is stale - run make docs-generate
  (needs mage + gomarkdoc; CI version pinned - install if missing per repo
  lint-fix workflow note).
- make graphs + graphs-check: leaf 14 ran graphs; re-run here after any
  final route/topic drift and confirm freshness.
- CLI surface: spec defers `meept voice` status to a later change - record
  that deferral explicitly in the feature doc (wiring requirement allows
  deferral when the PR notes it; the employee tool voice_call IS the user
  surface in this tree).
- docs/features.md: add the voice-sidecar row in the integrations table.

## Verified Anchors

- AGENTS.md - Key Components table + maintenance rule
- Makefile - install/build targets (read the meept-daemon lines first)
- docs/workflows/voice-sidecar.md - rev 2 (this tree's spec)
- docs/reference/git-hooks.md:116 - workflow-doc mapping table (add internal/voice row if the table enumerates packages)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Docs/build only. No Go API. Cross-refs resolve; make build, make
docs-check, make graphs-check green.

### What This Leaf Consumes

- Completion reports of leaves 01-15 (deviation lists feed the as-built section)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Spec status flip + as-built deltas section (every recorded deviation from leaves 02/06/07/08/11/12/14) | Modify: docs/workflows/voice-sidecar.md | cross-ref check (grep anchors resolve) | - | done | Given completion reports; When section written; Then each deviation cited with leaf id |
| T2 | internal/voice/AGENTS.md: full contracts (C1-C10 pointers, frame reorder, pacing, barge-in, release fan-out, child confinement, secrets lanes) | Create: internal/voice/AGENTS.md | - | - | done | Given the package; When an agent reads the chain AGENTS.md; Then voice invariants load |
| T3 | Root AGENTS.md: package table rows + invariant summary + build command (meept-voice) - same commit as T2 (repo rule) | Modify: AGENTS.md | - | - | done | Given the diff; When reviewed; Then summary and doc updated together |
| T4 | Makefile: build + install meept-voice; bin/ output alongside daemon | Modify: Makefile | make build | - | bin/meept-voice exists | Given make build; When finished; Then child binary present |
| T5 | Regenerate: make docs-generate + make graphs; both -check targets green | - | make docs-check && make graphs-check | - | exit 0 | Given schema+routes landed; When regenerated; Then fresh |
| T6 | docs/features.md row + ascii check on every touched doc | Modify: docs/features.md | python3 ord-scan on touched files | - | ASCII clean | Given new docs; When scanned; Then zero bytes >127 |

## Self-Verification Checklist

- [ ] Root AGENTS.md and internal/voice/AGENTS.md updated in the SAME commit wave (orchestrator enforces)
- [ ] As-built section names every deviation from the completion reports (none silently dropped)
- [ ] make build produces bin/meept-voice; make install copies it
- [ ] docs-check and graphs-check both green
- [ ] ASCII-only in every touched doc (em-dash grep)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines of each check
3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] AGENTS.md completeness review per the root maintenance rule (package table, build commands, invariants, analyzers/scripts unaffected)
- [ ] No stale statement left in the spec (non-goals still true after as-built)
- [ ] Generated trees actually regenerated (diff shows voice entries, not just timestamps)
- [ ] Deferral of `meept voice` CLI explicitly recorded

Output: APPROVED or specific gaps.

## Notes

docs/reference/git-hooks.md maps packages to workflow docs - add the
internal/voice -> docs/workflows/voice-sidecar.md row if that table is
enumerative (check before editing; the hook may derive it).
