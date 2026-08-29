# Tracker script - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Stdlib Python script that verifies catalog evidence and renders markdown.
- **Dependencies:** 01-catalog.md (catalog JSON must exist and be valid)
- **Estimated Context:** 60K
- **Concurrency Group:** B

## Goal

`scripts/research-harness-lit.py` implements Contract 2. Default run verifies evidence then writes `docs/research/harness-techniques.md`. `--check` also fails on markdown drift. `--search` hits arXiv with urllib and writes a snapshot JSON; it does not edit the catalog.

## Context

Follow the style of `scripts/audit-utf8-byte-arithmetic.py`: shebang, module docstring, argparse or sys.argv, exit codes, repo-root detection (walk up from `__file__` until `.git` or `go.mod`).

No third-party packages. No pip install.

Key files:

- `docs/research/harness-techniques.json` — input
- `scripts/audit-utf8-byte-arithmetic.py` — CLI style to copy
- `scripts/gen-connectivity-graph.py` — `--check` freshness pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

`scripts/research-harness-lit.py` CLI per Contract 2.
Generated `docs/research/harness-techniques.md` per Contract 3.

### What This Leaf Consumes

Catalog JSON schema from Contract 1.

## Tasks

### Task 1: Failing --check without script

**Objective:** Prove `--check` is not implemented yet.

**Files:**

- none yet

**Step 1: Write failing test**

Run: `python3 scripts/research-harness-lit.py --check`

Expected: FAIL — file not found or non-zero (script missing).

**Step 2:** Confirm old state: `test ! -f scripts/research-harness-lit.py` or file is a stub.

### Task 2: Implement verify + render

**Objective:** Default invocation verifies all shipped/partial evidence and writes markdown.

**Files:**

- Create: `scripts/research-harness-lit.py`

**Step 3: Write minimal implementation**

Required behavior:

- Resolve repo root from script location.
- Load `docs/research/harness-techniques.json`.
- For each technique with status shipped or partial: every `evidence[].path` must exist (file or directory). If `must_contain` is set, the file (not dir) must contain that UTF-8 substring.
- Print `MISSING id path` lines to stderr on failure; exit 1.
- If verify passes, render markdown: banner comment, counts, one table per category, columns Technique | Status | Sources | Evidence | Notes.
- Sources: markdown links. Repo-relative urls stay relative. http(s) stay absolute.
- Write atomically (temp file then replace) to `docs/research/harness-techniques.md`.

**Step 4:** Run `python3 scripts/research-harness-lit.py` from repo root.

Expected: exit 0, markdown created.

### Task 3: --check freshness

**Objective:** `--check` exits 1 if on-disk markdown differs from a fresh render.

**Files:**

- Modify: `scripts/research-harness-lit.py`

**Step 1:** After a successful render, `--check` exits 0.

**Step 2:** Append a space to the md (in a copy in /tmp only, or patch then restore). `--check` must exit 1. Restore the generated file by re-running without `--check`.

Compare using normalized newlines. Do not depend on mtime.

### Task 4: --search snapshot (optional network)

**Objective:** `--search` queries arXiv for each `search_queries` entry and writes `docs/research/snapshots/harness-lit-YYYY-MM-DD.json`.

**Files:**

- Modify: `scripts/research-harness-lit.py`

**Implementation notes:**

- URL: `http://export.arxiv.org/api/query?search_query=all:URLENCODED&start=0&max_results=5`
- Timeout 20s per query. On network error, print warning and continue; exit 0 unless all queries fail then exit 1.
- Snapshot shape: `{ "date": "...", "queries": { query: [ {"id", "title", "url"} ] } }`
- Print to stdout any paper id whose url is not already in catalog `sources`.
- Create `docs/research/snapshots/` if missing. Do not gitignore unless already ignored.
- User-Agent: `meept-research-harness-lit/1.0`
- Determinism: sort results by arXiv id within each query so `--check` stays stable.
- Rate limit: sleep 3s between queries (arXiv asks for 1 request / 3 seconds).
- Failure handling: an empty/failed query is recorded as `[]` in the snapshot, not a crash.

If the environment has no network, skip live call but still accept the flag (document in deviations).

### Task 5: Render must be deterministic

**Objective:** Two consecutive renders produce byte-identical markdown (stable category order, stable technique order within a table).

**Files:**

- Modify: `scripts/research-harness-lit.py`

**Step 1:** Sort categories alphabetically (or a fixed list order) and techniques by id within each category. Never rely on dict insertion order.

**Step 2:** Run the script twice; `shasum` both md outputs; hashes must match.

Expected: identical. This is what makes `--check` trustworthy.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] `python3 scripts/research-harness-lit.py --check` exits 0 after a render
- [ ] Two consecutive renders are byte-identical (shasum match)
- [ ] Stdlib only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths)
- [ ] Code follows project conventions (naming, error handling, structure)
- [ ] No bugs, no security issues
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

Do not edit Makefile or AGENTS.md (leaf 03).
Do not add techniques to the catalog unless verify requires a path fix — that belongs to leaf 01.
