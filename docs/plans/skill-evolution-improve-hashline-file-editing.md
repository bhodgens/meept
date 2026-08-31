# Plan: Skill evolution: improve hashline-file-editing

## Meta

- plan_id: plan-20260831231959-0006
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.16 (6 positive vs 29 negative) — skill is being over-injected (37 times) with poor results. The description likely lacks specificity on when to use file_write with direct:true versus other write methods, leading to misapplication.

Candidate content:
# hashline-file-editing

Use `file_write` with `direct:true` ONLY when the user explicitly requests direct disk writing, immediate persistence, or references a file write operation that must bypass staging buffers.

## When to use
- User says "write directly", "use direct:true", "persist immediately", or specifies `file_write` with `direct:true`
- The task requires an atomic, on-disk write without intermediate preview or diff
- Creating output files (e.g., `answer.txt`, `summary.txt`) where the exact byte content matters

## When NOT to use
- General code editing or refactoring — use standard file write without `direct:true`
- Writing large files or multiple files — prefer batch operations
- When the user doesn't specify direct writing — use default behavior

## Tool format
```
file_write(path="<absolute_or_relative_path>", content="<exact_content>", direct:true)
```

## Examples
- Write `answer.txt` containing exactly `42` → use `file_write` with `direct:true`
- Write `summary.txt` with project description → use `file_write` with `direct:true`
- Edit an existing Go source file → do NOT use this skill; use standard coding workflow

## Notes

