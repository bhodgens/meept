# Plan: Skill evolution: improve git-blame-contributor-analysis

## Meta

- plan_id: plan-20260831224641-0038
- created: 2026-08-31
- status: planning

## Summary

Effectiveness of 0.19 (33 negative vs 10 positive) indicates the skill is frequently invoked but failing to deliver useful results. The skill likely lacks clear scope, concrete output format, and proper tool guidance for git blame analysis.

Candidate content:
# git-blame-contributor-analysis

## Purpose
Analyze git blame output to identify key contributors, their commit patterns, code ownership, and file evolution over time.

## When to Use
- User asks about who authored/modified specific files, lines, or sections of code.
- User wants to understand code ownership or responsibility for a file/directory.
- User asks about contributor activity, commit frequency, or change history.
- User needs to find the original author of a specific line or block of code.

## NOT for
- General code review or quality assessment.
- Merging or migrating repositories.
- Analyzing non-git version-controlled content.

## Tools Available
- `git blame` — line-level attribution
- `git log` — commit history and contributor stats
- `git shortlog` — summarized contributor output
- File reading for context around blamed lines

## Steps
1. **Understand the scope**: Determine whether the user is asking about a specific file, directory, or repository-wide contributor analysis.
2. **Gather blame data**:
   - Run `git blame <file>` for line-level attribution.
   - Run `git blame -e <file>` to include author emails.
   - Run `git log --follow -- <file>` for full history including renames.
3. **Summarize contributors**:
   - Run `git shortlog -sn -- <file>` to rank contributors by commit count.
   - Cross-reference with `git blame` output to identify dominant authors.
4. **Annotate findings**:
   - Note lines added/most recently modified by different contributors.
   - Identify stale sections (unchanged for long periods) vs. actively maintained ones.
5. **Present results**:
   - Show top contributors with commit counts and affected line ranges.
   - Highlight any surprising or unexpected attribution (e.g., large sections authored by a low-commit-count contributor).
   - Include file path and any relevant context about the code section.

## Output Format
```
### Contributor Analysis: <file path>

**Top Contributors:**
1. <Author> — <N> commits, ~<X>% of file
2. <Author> — <N> commits, ~<X>% of file
...

**Key Findings:**
- <Specific observation about code ownership or notable lines>
- <Any patterns in contribution over time>

**Original Authors of Notable Sections:**
- Lines <X>-<Y>: <Author> (<commit hash>) — <brief description if available>
```

## Best Practices
- Always specify the file path when running git commands.
- Use `--relative` flag if running from a subdirectory.
- Combine blame data with commit messages for richer context.
- If the file has no git history (e.g., newly added), state so explicitly.
- Do not fabricate contributor data; report accurately even if sparse.

## Notes

