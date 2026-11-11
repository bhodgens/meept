# Research Docs Import — Implementation Leaf (08, docs-only)

> **For the implementing agent:** You are the implementer for this leaf.
> DOCS-ONLY: 1) read current content; 2) confirm old state; 3) write content;
> 4) verify cross-refs resolve. Do NOT commit. Do NOT use read_file on
> existing source files — use terminal cat/search_files.

## Meta

- **Parent:** ./master.md
- **Scope:** Import 4 speech research docs from ~/git/uptractor-jobfile branch claude/uptractor-repo-organization-5jusi4 (backend/research/) into meept docs/research/ with provenance headers.
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A (docs/ only, file-disjoint)

## Goal

Preserve the research corpus: unified-speech-to-speech.md (pipeline-vs-S2S
verdict + benchmarks), voice-stt-tts-apple-silicon.md (model/license tables,
Kokoro-82M data point), voice-coordination-threat-assessment.md, and
recommendation.md. These corroborate meept's cascaded local pipeline and feed
future STT/TTS provider decisions (leaf 07's eval lane guards the swaps).

## Context

Source: `git -C ~/git/uptractor-jobfile show origin/claude/uptractor-repo-organization-5jusi4:backend/research/<file>`.
Destination: ~/git/meept/docs/research/2026-10-28-uptractor-<topic>.md (date
prefix = source doc date, matching docs/research naming convention).

## Verified Anchors

- docs/research/ — existing naming convention (2026-10-24-agent-parity-audit.md etc.)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Four new files under docs/research/, each with a 3-line provenance header
(source repo, branch, import date 2026-11-10, "imported verbatim; findings
predate meept-specific work").

### What This Leaf Consumes

- Nothing.

## Tasks

| Task ID | Objective | Files | Verify command | PASS state |
|---|---|---|---|---|
| T1 | Import 4 docs verbatim + provenance headers | Create docs/research/2026-10-28-uptractor-unified-speech-to-speech.md, ...-voice-stt-tts-apple-silicon.md, ...-voice-coordination-threat-assessment.md, ...-recommendation.md | `ls docs/research/*uptractor*` + grep provenance | 4 files, headers present, content byte-identical below header |

## Self-Verification Checklist

- [ ] Content verbatim below provenance header (diff against git show output)
- [ ] Filenames match docs/research convention
- [ ] No edits to any existing file

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** 2. **Verify command + output** 3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] Verbatim (spot diff two files)
- [ ] Provenance headers complete
- [ ] Cross-refs inside docs resolve or are marked as source-repo paths

Output: APPROVED or specific gaps.
