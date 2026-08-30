# Plan: Skill evolution: improve hashline-file-editing

## Meta

- plan_id: plan-20260830220232-0002
- created: 2026-08-30
- status: planning

## Summary

The skill body is completely empty while being actively injected (5 injections), so it provides zero guidance at runtime, which plausibly explains the poor signal (1 positive, 2 negative, effectiveness 0.25). The skill needs a full rewrite: precise hashline syntax spec, a mandatory read-verify-edit-verify workflow, and explicit failure modes (stale line numbers, off-by-one, indentation loss, 1-based indexing) that are the most common causes of negative outcomes for line-range editing.

Candidate content:
# Hashline File Editing

## Purpose
Edit existing files precisely using hashline notation (`<path>#L<start>-L<end>`) instead of rewriting whole files.

## When to Use
- You must modify, replace, insert, or delete specific lines in an existing file.
- A hashline reference like `src/app.py#L12-L18` appears in the conversation.
- You know the exact target region and want a minimal, surgical change.

Do NOT use for: creating new files, whole-file rewrites, or edits whose location is unknown (search/read first, then apply).

## Syntax
`<file-path>#L<start>-L<end>`

- Line numbers are **1-based** and the range is **inclusive on both ends**.
- Single line: `src/utils.py#L42-L42`
- Range: `src/main.rs#L10-L25` covers exactly lines 10 through 25 (16 lines).

## Procedure (read - verify - edit - verify)
1. **Read first.** Read the target range plus at least 5 lines of context above and below. Never edit from memory.
2. **Quote current content.** Before editing, restate the exact current content of the range so any mismatch is visible.
3. **Check freshness.** If the file may have changed since your last read (other edits, linters, other agents), re-read and recompute line numbers.
4. **Compose the replacement.** Match the file's existing indentation exactly (tabs vs. spaces). Do not reformat or re-indent lines outside the change.
5. **Apply the edit** to the stated range only.
6. **Verify.** Re-read the edited region and, when available, run a syntax check or linter for that file type.

## Edit Operations
- **Replace:** target the exact range; replacement text becomes the new content of those lines.
- **Insert after line N:** target `#LN-LN`, keep the original line, and append the new lines after it.
- **Delete:** target the exact range and supply an empty replacement.
- **One range per edit.** Do not chain multiple disjoint ranges into a single operation; issue sequential edits, re-reading between them if line numbers shift.

## Rules
- Never guess line numbers — derive them from a fresh read.
- Edit the smallest range that accomplishes the change.
- Preserve trailing whitespace conventions and line endings of the file.
- If verification shows the edit landed in the wrong place, revert and redo with recomputed numbers rather than patching the mistake with further blind edits.

## Common Mistakes to Avoid
- **Off-by-one:** `#L5-L9` is 5 lines; confirm both endpoints before writing.
- **Stale references:** search results or earlier reads may no longer match the file; always re-read immediately before editing.
- **Indentation loss:** leading whitespace must be reproduced exactly.
- **Zero-based thinking:** hashlines are 1-based, unlike many internal tool outputs.

## Example
Replace lines 12-18 of `src/utils.py`:

Verified current content (`src/utils.py#L12-L18`):
```python
def old_function(x):
    # legacy implementation
    ...
```

Edit: replace `src/utils.py#L12-L18` with:
```python
def new_function(x):
    return x * 2
```

Then re-read `src/utils.py#L8-L24` and run the linter to confirm.

## Verification Checklist
- [ ] Range read immediately before the edit
- [ ] Existing content quoted verbatim
- [ ] Replacement matches file indentation
- [ ] Edited region re-read after applying
- [ ] Syntax/lint check passes (when available)

## Notes

