# Plan: Skill evolution: improve git-blame-contributor-analysis

## Meta

- plan_id: plan-20260831231926-0004
- created: 2026-08-31
- status: planning

## Summary

Effectiveness of 0.20 with 44% negative rate indicates the skill is frequently misapplied or produces poor outputs. Likely issues: unclear trigger conditions (when to use git blame vs simpler alternatives), ambiguous output format, or missing guidance on interpreting blame results practically.

Candidate content:
# git-blame-contributor-analysis

Use this skill when the user wants to understand **who contributed what** to a file or set of files, including line-level attribution, contribution breakdown, or identifying owners of specific sections.

## When to Use

- Analyzing ownership or responsibility for specific code sections
- Finding the primary contributor(s) to a file or function
- Understanding contribution history before making changes
- Investigating why certain code exists (who wrote it and when)

## When NOT to Use

- Simple log viewing → use `git log` directly
- Counting commits by author → use `git shortlog`
- Finding recent changes → use `git log -p`

## Procedure

1. **Run git blame** with appropriate options:
   - For overall file analysis: `git blame <file>`
   - For line-range focus: `git blame -L <start>,<end> <file>`
   - For formatted output: `git blame --line-porcelain <file>`

2. **Aggregate results** to identify:
   - Top contributors by line count
   - Time span of contributions
   - Files with most diverse/least diverse authorship

3. **Present findings** in structured format:

```
## File: <path>
- Total lines: <N>
- Contributors:
  - <author> (<commit_count> commits, <line_count> lines)
  - ...
- Primary owner: <author>
- Last modified: <date> by <author>
```

## Output Format

Return a concise summary covering:
1. **Top contributors** ranked by lines touched
2. **Primary owner** — the single most responsible author
3. **Contribution timeline** — earliest and latest commits
4. **Notable patterns** — e.g., file rewritten by one person, stale sections, hot files

## Tool Hints

- Use `explore` for read-only search of relevant files first
- Use `code` for analyzing blame output and generating reports
- Use `git` for running git blame commands directly

## Example

User: "Who wrote the auth module in main.go?"
→ Run: `git blame internal/auth/main.go`
→ Report top authors, highlight the primary author of key functions


## Notes

