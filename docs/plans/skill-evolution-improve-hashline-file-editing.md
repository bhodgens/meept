# Plan: Skill evolution: improve hashline-file-editing

## Meta

- plan_id: plan-20260831214648-0029
- created: 2026-08-31
- status: planning

## Summary

Effectiveness of 0.16 with 83% negative outcomes indicates the skill is likely being triggered inappropriately or producing poor edits. The skill needs clearer activation conditions, decision logic against alternatives, and explicit failure modes to prevent misuse.

Candidate content:
## hashline-file-editing

Targeted file editing using hash-based line identification for precise, surgical modifications.

### When to Use

Use ONLY when ALL of the following are true:
1. The target file is large or complex enough that full rewrites are error-prone
2. You need to modify specific lines without affecting surrounding content
3. You can uniquely identify the target line(s) via content hash or stable anchor

### When NOT to Use (choose alternative)

- Small files (<50 lines): use direct full-file edit instead
- Adding new content at end of file: use append operations
- Rewriting large sections (>20% of file): use full replacement
- Unknown/unverifiable target lines: do not attempt hashline editing

### Workflow

1. **Read the file** to understand its structure and locate the target content
2. **Identify the exact line(s)** to modify using their content hash or stable context anchor
3. **Verify uniqueness** — confirm the hash/anchor matches only the intended lines
4. **Apply the edit** using the hashline reference, preserving all surrounding content
5. **Validate** — re-read the affected section to confirm the edit is correct and nothing else changed

### Best Practices

- Always verify the hash reference resolves to exactly the lines you intend
- Include surrounding context in verification to catch accidental matches
- Prefer content-based hashes over line-number-based references for stability
- If the target content is ambiguous, fall back to a full-file edit with clear diff

### Failure Modes

- Hash collision or ambiguous match: abort and use alternative method
- Edit introduces syntax errors: revert and reassess approach
- Surrounding content modified unexpectedly: investigate and fix


## Notes

