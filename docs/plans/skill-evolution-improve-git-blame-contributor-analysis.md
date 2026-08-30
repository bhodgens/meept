# Plan: Skill evolution: improve git-blame-contributor-analysis

## Meta

- plan_id: plan-20260830220145-0001
- created: 2026-08-30
- status: planning

## Summary

Effectiveness is 0.00 (13 injections, 7 negative, 0 positive) and the skill body is completely empty, so every injection delivers zero guidance and actively wastes context. Prior archive proposals were rejected due to fallback scoring, so the skill will remain active — the only viable path is to replace the empty body with concise, accurate contributor-analysis guidance. The most common failure mode for this task is naive blame usage (last-touch attribution, reformat commits, moved code), so the improved content emphasizes attribution-accuracy flags and complementary commands.

Candidate content:
---
name: git-blame-contributor-analysis
description: Use when attributing code to contributors — answering who wrote or last modified a file, line range, or module; measuring per-author contribution; assessing code ownership or bus factor; or preparing code-history summaries. Covers git blame, git shortlog, and per-author statistics, including attribution-accuracy caveats.
---

# Git Blame Contributor Analysis

Attribute code to contributors accurately and summarize per-author contribution.

## When to Use
- "Who wrote this file/line/function?"
- "Who are the main contributors to module X?"
- Ownership, onboarding, bus-factor, or reviewer-selection questions.

## Core Commands

| Goal | Command |
|---|---|
| Blame a file with author emails | `git blame -e <file>` |
| Blame a line range | `git blame -L <start>,<end> <file>` |
| Ignore whitespace-only changes | `git blame -w <file>` |
| Detect moved/copied code | `git blame -M -C -C <file>` |
| Skip known reformat commits | `git blame --ignore-rev <rev>` (or list revs in `.git-blame-ignore-revs`) |
| Machine-readable output | `git blame --porcelain <file>` |
| Commit counts per author | `git shortlog -sne` (add `--all` for all branches) |
| Per-author churn on a path | `git log --follow --numstat --format='%an' -- <file>` |

## Attribution Accuracy (critical)
Blame shows the **last** commit that touched each line, not the original author. Before reporting results:
1. Add `-w` so reformatting/reindentation does not steal attribution.
2. Identify bulk-move, rename, or reformat commits; re-run with `--ignore-rev`, or recommend committing a `.git-blame-ignore-revs` file.
3. Use `-M -C` when code was copied or moved from elsewhere in the repo.
4. For renamed files, use `git log --follow -- <path>` to reach original authors.

## Summarizing Results
- Group blamed lines by author:
  `git blame --porcelain <file> | grep '^author ' | sort | uniq -c | sort -rn`
- Report both **line attribution** (blame) and **commit activity** (shortlog) — they answer different questions and often disagree.
- Always state caveats: last-touch attribution semantics, refactor commits excluded or included, branches and date range used.

## Pitfalls
- Do not equate blame line counts with authorship credit; refactors shift attribution over time.
- Check provenance before blaming generated or vendored files.
- Scope blame with `-L` ranges or pathspecs in large repos to keep output small and fast.


## Notes

