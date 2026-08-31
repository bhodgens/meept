# Plan: Skill evolution: improve hujson-unquoted-keys-fix

## Meta

- plan_id: plan-20260831232014-0007
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.29 (32 positive, 66 negative). The skill likely needs clearer detection criteria and more specific transformation rules for HUJSON unquoted key fixes.

Candidate content:
---
name: hujson-unquoted-keys-fix
description: Detect and fix unquoted keys in HUJSON (Heredoc JSON) files by quoting them properly while preserving comments and formatting.
---

## When to Use
Use this skill when a HUJSON file contains unquoted object keys that violate JSON/HUJSON syntax rules. Common triggers include lint errors, parser failures, or explicit requests to fix HUJSON formatting.

## Key Rules
1. **Identify unquoted keys**: Look for bare identifiers at the start of key positions in objects (e.g., `{ foo: "bar" }` — `foo` is unquoted).
2. **Quote with double quotes**: Convert unquoted keys to properly quoted strings (e.g., `{ "foo": "bar" }`).
3. **Preserve comments**: HUJSON supports line comments (`//`) and block comments (`/* */`). Never remove or alter comments.
4. **Preserve formatting**: Maintain indentation, line breaks, and trailing commas (if present).
5. **Do not quote string values**: Only quote keys, never modify the content of already-quoted values.
6. **Root-level keys**: If the file is a single JSON object at the root, all top-level unquoted keys must be fixed.

## Detection Pattern
- Regex hint for unquoted keys: match word characters immediately after `{` or `,` followed by `:`
- Example invalid: `{ name: "test", age: 30 }`
- Example valid: `{ "name": "test", "age": 30 }`

## Transformation Steps
1. Read the HUJSON file completely.
2. Parse the structure to identify all object key positions.
3. For each unquoted key, wrap it in double quotes.
4. Verify all comments and whitespace are untouched.
5. Write the corrected content back using `file_write` with `direct:true`.

## Edge Cases
- Keys containing special characters (e.g., hyphens, spaces) **must** be quoted regardless — this skill only fixes *previously* unquoted ones.
- Nested objects: recurse into all levels.
- Arrays inside objects: only fix keys, do not touch array elements.
- Empty objects `{}` or empty arrays `[]` require no changes.

## Output
Return the corrected HUJSON content. Do not add explanatory text unless asked.

## Notes

