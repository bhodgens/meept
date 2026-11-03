# Office Library Research Gate - Research Leaf

> **For the implementing agent:** You are the RESEARCHER for this gate leaf.
> This is a RESEARCH leaf, not implementation - you gather empirical evidence
> about candidate Go libraries, you do NOT write product code. Your deliverable
> is ONE report file written to `06-office-skills/library-picks.md`. Do NOT
> commit. Do NOT git add. Write once and stop.

## Meta

- **Parent:** ../../06-office-skills/orchestrator.md
- **Scope:** Resolve, WITH EVIDENCE, the vetted OSS Go library for docx and for
  pptx (xlsx = excelize, confirm-only), satisfying the user license gate; write
  the GO/NO-GO report + chosen proven reference.
- **Dependencies:** none (disp 1st, in parallel with 00T-tests)
- **Estimated Context:** 18K
- **Concurrency Group:** Phase 0 (research gate)

## Goal

The branch ships an office skill bundle whose docx and pptx tools are backed by
a Go library. The pure-Go OOXML ecosystem is thin and plausibly lacks a
many-star maintained candidate that passes the license gate - so the library
choice MUST be verified empirically before any implementation leaf dispatches,
not assumed. This leaf resolves that choice and freezes it in
`library-picks.md`, the evidence-backed authority consumed by leaves 01/02.

xlsx is already resolved: `github.com/xuri/excelize/v2` (BSD-3-Clause, ~21k
stars) is pinned directly at v2.11.0 in `go.mod:45` and already builds into the
`spreadsheet_write` tool. Confirm-and-record only; do NOT re-litigate it.

## Context

- The dsh archetype (not a literal port) is a config/skills Office bundle whose
  model tools create/read/edit/validate OOXML documents. meept's runtime is Go
  libraries, so the tooling must be a Go library, not python-docx/LibreOffice.
- The user license gate (frozen master D6): GPL/AGPL acceptable ONLY as
  subprocess deps, never vendor/link into shipped binaries. Many permissive
  licenses (MIT/Apache-2.0/BSD-3-Clause) are fine. **unioffice is REJECTED**
  (commercial EULA) regardless of stars - never recommend it.
- Your report is the source of truth for library choice (canon C1-Trace /
  research-gated rule 2). Implementation leaves read `library-picks.md` at
  implementation time; any conflict in a leaf's seed library LOSES to it.

## Verified Anchors

- `/Users/caimlas/git/meept/go.mod:45` - `github.com/xuri/excelize/v2 v2.11.0`
  (direct dep); `go.mod:3` `go 1.26.0`; `go.mod:5` toolchain go1.26.5.
- `/Users/caimlas/go/pkg/mod/github.com/xuri/excelize/v2@v2.11.0/go.mod` -
  `go 1.25.0` (the excelize requirement that meept's 1.26.0 satisfies: verified
  fit, no conflict, no OPEN QUESTION).
- `/Users/caimlas/git/meept/internal/tools/builtin/spreadsheet_xlsx.go:24` -
  `writeXLSX(w io.Writer, sheets []SheetSpec)` (existing excelize usage).

## Tasks

Research leaves swap TDD for verification steps (research-gated plan-trees.md
rule 7): citations resolve, claims are evidence-backed with URLs, and the
reviewing orchestrator cross-checks key numbers.

**T1: Verify xlsx default (excelize) on the record.**
1. Confirm via the on-disk module (`.../excelize/v2@v2.11.0/go.mod`) the `go`
   directive. Compare against meept's `go 1.26.0` - record that it FITS (meept
   >= excelize requirement). Note this resolves OPEN QUESTION #3.
2. Record license + star count + last release + activity evidence with the
   repo URL and any release/API evidence page. Recommend excelize as the proven
   xlsx reference (NO-GO not applicable - already pinned and building).

**T2: Docx candidates - evidence table.**
1. Seed list to verify (each entry: confirm real, then license + stars +
   go-directive + last-release + a working create/paragraph/heading API
   signature): `github.com/nguyenthenguyen/docx` (MIT, minimal writer),
   `github.com/mikhail-tokarev/go-docx` (MIT fork), and any OTHER many-star Go
   docx creator/editor you find by searching "golang docx github stars" /
   "go docx create openxml go module". **unioffice must appear only as a
   REJECTED row** (commercial EULA) - verify its license page and record it,
   but it is never eligible.
2. For each candidate record: URL, license (and evidence), star count (with
   evidence/snapshot), go.mod `go` directive (does it fit meept 1.26.0?), last
   release / commit activity, and one VERIFIED API signature (from the module's
   exported docs or source) for creating a document and adding a heading /
   paragraph. If a candidate resolves to nothing real, mark it
   `OPEN QUESTION: <name> not found; closest alternative is <name>` - never
   silently substitute.
3. Recommend a winner with fallback order (a decision line: pick A; fallback B
   then C). If NO candidate passes BOTH the license gate AND the usability bar
   (can create + edit a modest Word document in Go), record a **NO-GO** for
   docx with the reason + the closest real alternatives ranked.

**T3: Pptx candidates - evidence table.**
1. Seed list: `github.com/...` Go presentation libs found by searching "golang
   pptx create github stars" / "go openxml pptx module". Verify any that
   surface; record the same evidence columns as T2 (license + stars + go
   directive + activity + a verified create-slide/add-text API signature).
   **unioffice again only as a REJECTED row.** Mark-not-found candidates as
   `OPEN QUESTION`. Rank alternates.
2. The Go PPTX authoring ecosystem is genuinely thin - if no candidate
   satisfies the gate, record a **NO-GO** for pptx with the ranked closest
   real alternatives and note the delta against the dsh python-pptx archetype.

**T4: Write the chosen proven reference file.**
Write `06-office-skills/library-picks.md` exactly (sales file, one artifact):
- Header: claim of the gate leaf + date + meept branch (06-office-skills).
- `## Decision` - one line per format: xlsx=excelize v2.11.0 (proven pin);
  docx=`<chosen>` (GO) or NO-GO; pptx=`<chosen>` (GO) or NO-GO; each with a
  one-line reason + fallback order.
- `## Evidence` - a table per format (candidate, license, stars+evidence, go
  directive, last release, verified API signature, verdict). Every claim
  carries its evidence URL. NO-GO rows carry the reason + ranked alternatives.
- `## OPEN QUESTION` - a numbered list of every unresolved item using the
  literal string `OPEN QUESTION:` (greppable, research-gated rule 4), including
  any NO-GO escalation for the user.
- `## Fallback` - the decision table mapping each format to its fallback order.

Verify: every seed resolved to a real repo or is marked `OPEN QUESTION`; every
`OPEN QUESTION:` literal present in plain ASCII; `unioffice` appears only as a
REJECTED row; no API key / secret written; every numeric claim (stars, go
directive) traceable to an evidence URL or the verified module source.

## Self-Verification Checklist

- [ ] `library-picks.md` written at the exact path with all four sections
- [ ] Every candidate library: license + stars + go-directive + activity each
      backed by evidence, or marked `OPEN QUESTION:`
- [ ] `unioffice` present ONLY as a REJECTED row (commercial EULA)
- [ ] Excelize fit recorded (meept go 1.26.0 vs excelize go 1.25.0 -> fits)
- [ ] Chosen reference per format with fallback order; NO-GO rows escalate to
      the user via `OPEN QUESTION:`
- [ ] ASCII only; no git commit, no git add

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** 06-office-skills/library-picks.md
2. **Verify:** the four section headers present; evidence URLs resolve;
   one numeric claim per format cross-checked by the orchestrator
3. **Deviations:** none / list with rationale (any library not in the seed
   list that you adopted MUST be named and justified)

## Review Checklist (For Review Agent)

- [ ] `library-picks.md` exists with `## Decision / ## Evidence / ## OPEN
      QUESTION / ## Fallback`
- [ ] Each format has a chosen library + fallback order (or a NO-GO escalated)
- [ ] Evidence is URL-or-module-verified; no invented stars/licenses/go
      directives
- [ ] unioffice appears only as REJECTED; no GPL/AGPL library is adopted for
      vendored/linked use
- [ ] The docx/pptx tool JSON contracts (orchestrator C11.2-C11.4) are confirmed
      implementable with the chosen library (tool contract is lib-agnostic and
      preserved on any pick)

Output: GO (chosen reference) or list of gaps.

## Notes

- xlsx is NOT re-litigated: excelize is the proven default (already pinned and
  building). Only docx/pptx are genuinely research-gated.
- Do not invent a library that does not exist (research-gated rule "verify the
  vendor EXISTED"): the 2026 audit precedent found a user-name that matched no
  real product. Surface mismatches as `OPEN QUESTION` with closest real
  alternatives ranked.
- The chosen reference must let the leaf author the tool's INTERNAL call only;
  the model-facing JSON contracts in the orchestrator stay fixed.
