# Catalog JSON - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Finish `docs/research/harness-techniques.json` with regressively verified evidence paths.
- **Dependencies:** none
- **Estimated Context:** 50K
- **Concurrency Group:** A
- **Audit references:** Apple Note RESEARCH literature tracker; aborted JSON 2026-08-29

## Goal

A complete catalog JSON matching Contract 1. Every `shipped`/`partial` row has at least one evidence path that exists on disk. No `scratch/` paths. Categories and statuses use the frozen enums.

## Context

A file already exists at `docs/research/harness-techniques.json` from an aborted implement. Audit it. Fix bad evidence. Fill gaps from `analysis/pi-agent.md` and `docs/research/2026-08-24-agent-parity-audit.md`.

Known errors in the aborted file:

- `FilteredToolRegistry` is in `internal/agent/registry.go`, not `executor.go`
- `internal/browser` may not be a package path — confirm chromedp evidence
- `must_contain: "hashline"` in `file_edit.go` is case-sensitive; confirm

Key files:

- `docs/research/harness-techniques.json` — starting catalog
- `analysis/pi-agent.md` — Pi vs meept
- `docs/research/2026-08-24-agent-parity-audit.md` — parity audit
- `internal/llm/context_firewall.go`, `internal/agent/prompt.go`, `internal/tools/builtin/hashline.go`

## Interface Contracts (From Parent)

### What This Leaf Exposes

`docs/research/harness-techniques.json` valid per Contract 1 in master.md.

### What This Leaf Consumes

None from siblings.

## Tasks

### Task 1: Validate existing catalog JSON

**Objective:** Parse the aborted catalog and list every evidence path that does not exist.

**Files:**

- Modify: `docs/research/harness-techniques.json` only if invalid JSON

**Step 1:** Confirm old state

Run:

```
python3 -c "import json; json.load(open('docs/research/harness-techniques.json'))"
```

Expected: no exception. If the file is missing, recreate from Contract 1 with at least 20 techniques.

**Step 2:** List missing evidence

Run from repo root (stdlib):

```
python3 -c "
import json, os
c=json.load(open('docs/research/harness-techniques.json'))
for t in c['techniques']:
    if t['status'] not in ('shipped','partial'):
        continue
    for e in t.get('evidence') or []:
        p=e['path']
        if not os.path.exists(p):
            print('MISSING', t['id'], p)
"
```

Expected: print missing paths (may be several).

### Task 2: Fix evidence to real files

**Objective:** Every shipped/partial evidence path exists. `must_contain` matches file bytes if set.

**Files:**

- Modify: `docs/research/harness-techniques.json`

**Step 1:** For each MISSING line, find the real file with `search_files` (not scratch/). Patch the path.

**Step 2:** For each `must_contain`, `rg -l` that string in the evidence file. If absent, drop the field or pick a string that exists.

**Step 3:** Re-run the missing-path command. Expected: no MISSING lines.

**Step 4:** Confirm enums: status in shipped|partial|candidate|skip; category in the Contract 1 set.

### Task 3: Cover required seed techniques

**Objective:** Catalog includes at least these ids (create if missing): context-firewall, pi-iterative-compaction, stable-prefix-prompt, token-budgets, gbnf-schema-tools, tool-call-repair, hashline-edit, skills-md, memento-skill-loop, mcp-client-server, security-fence.

**Files:**

- Modify: `docs/research/harness-techniques.json`

**Step 1:** python3 print sorted ids. If any required id is missing, add a row with sources from `analysis/pi-agent.md` or arXiv 2603.18743 (Memento-Skills) as appropriate.

**Step 2:** Set `updated` to today's UTC date YYYY-MM-DD.

### Task 4: Keep evidence URLs honest

**Objective:** Every `sources[].url` is either a valid http(s) URL or an existing repo-relative path. No invented links.

**Files:**

- Modify: `docs/research/harness-techniques.json`

**Step 1:** For each repo-relative url, confirm the path exists (same check as evidence). For http(s) urls, spot-check only ones you added; keep inherited urls as-is even if unverified (mark nothing).

**Step 2:** Remove any url you cannot justify from a seed doc (`analysis/pi-agent.md`, parity audit, the Meept ideas note).

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] Zero MISSING evidence paths for shipped/partial
- [ ] No path under scratch/
- [ ] Every sources url is http(s) or an existing repo path

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

Do not write the Python tracker script. That is leaf 02.
Do not edit Makefile or AGENTS.md. That is leaf 03.
