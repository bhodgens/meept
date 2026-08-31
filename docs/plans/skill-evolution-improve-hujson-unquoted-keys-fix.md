# Plan: Skill evolution: improve hujson-unquoted-keys-fix

## Meta

- plan_id: plan-20260831224718-0040
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.29 (31 positive vs 64 negative out of 95 total). The skill likely lacks precision in determining when unquoted keys are valid HUJSON versus truly invalid, leading to excessive false-positive fixes that corrupt valid content. Need to add explicit validation gates, safety checks, and distinguish HUJSON permissive syntax from actual errors.

Candidate content:
# HUJSON Unquoted Keys Fix

## Purpose
Fix genuinely invalid JSON-like content by adding quotes around object keys where required, while preserving valid HUJSON syntax.

## When to Apply
Apply ONLY when:
1. The content is in standard JSON format (not HUJSON), OR
2. The content is HUJSON but contains clearly broken syntax that would fail parsing
3. The unquoted key appears in a position where JSON mandates quoting (object keys)

## Pre-Flight Checks (skip if any fail)
- Verify the file/section is NOT already valid HUJSON. HUJSON permits unquoted keys by design — do NOT fix these.
- Check if the content uses HUJSON-specific features (`#` comments, trailing commas, unquoted string values, unquoted keys). If yes, the file is intentionally HUJSON — do not modify.
- Confirm the target is an object key position (preceded by `{` or `,` and followed by `:`).

## Decision Flow
1. Is the file/header declaring HUJSON format? → SKIP (unquoted keys are valid)
2. Are there HUJSON-style comments (`//` or `#`)? → SKIP (this is HUJSON)
3. Are there trailing commas? → SKIP (this is HUJSON)
4. Is the unquoted value a bare string that would be invalid in strict JSON? → FIX
5. Is the surrounding context clearly JSON (no HUJSON features)? → FIX only the broken keys

## Fix Rules
- Wrap ONLY object keys that are unquoted in valid JSON context with double quotes
- Do NOT modify string values, array elements, or comment content
- Preserve all whitespace and formatting outside the fix
- After fixing, validate the output parses as valid JSON

## Anti-Patterns (DO NOT Fix)
- `key: value` in HUJSON files
- Unquoted keys in files with `.hujson` extension
- Any content containing `#` or `//` comments
- Template/embedded contexts where quoting would break functionality
- Already-quoted keys (no double-quoting)

## Output Format
Return only the corrected content. Do not include explanations or diffs unless explicitly requested.

## Notes

