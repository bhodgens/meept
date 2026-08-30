# Plan: Skill evolution: improve hujson-unquoted-keys-fix

## Meta

- plan_id: plan-20260830213339-0002
- created: 2026-08-30
- status: planning

## Summary

The skill body contains only a placeholder analysis prompt with zero actionable instructions for actually fixing HuJSON unquoted keys. With 10 injections, 0 positive and 4 negative outcomes (effectiveness 0.00), every injection has wasted context and likely contributed to failures or confusion. The fix is to replace the placeholder with concrete, executable guidance: trigger conditions, a step-by-step quoting procedure, keyword/numeric key edge cases, comment/trailing-comma handling for strict JSON conversion, worked examples, and a validation checklist.

Candidate content:
# HuJSON Unquoted Keys Fix

## Purpose
Convert HuJSON (Human JSON, used by tools like Caddy and Tailscale) with unquoted object keys into valid JSON by wrapping every bare key in double quotes. Apply this when a strict JSON parser rejects a config file, or when a task asks to fix unquoted keys / convert HuJSON to JSON.

## When to Apply
- A JSON parser (jq, `python -m json.tool`, `JSON.parse`, etc.) fails with errors such as "Expecting property name enclosed in double quotes" or "invalid character" before a `:`.
- The file is known or suspected to be HuJSON: unquoted keys, `//` or `/* */` comments, or trailing commas may be present.
- The task explicitly says "fix unquoted keys", "convert HuJSON to JSON", or "make this valid JSON".

## Procedure
1. **Locate all bare (unquoted) keys.** A bare key is a token appearing directly before `:` inside `{ ... }` that is not already wrapped in double quotes. Example: in `{name: "value"}`, `name` is bare.
2. **Wrap each bare key in double quotes.** `{name: "value"}` → `{"name": "value"}`.
3. **Handle keyword and numeric keys.** HuJSON permits `true`, `false`, `null`, and numeric literals as keys; these must become strings: `{null: 1, 42: "a"}` → `{"null": 1, "42": "a"}`.
4. **Never alter quoted keys.** Keys already in double quotes are left untouched, including their escape sequences.
5. **Preserve all values exactly.** Only keys are modified. Do not reformat, reorder, or change strings, numbers, booleans, or whitespace unless the task also requests it.
6. **If strict JSON output is required** (not just key quoting), also:
   - Remove `//` line comments and `/* */` block comments.
   - Remove trailing commas after the final element in objects and arrays.
7. **Validate the result** with a strict JSON parser, e.g. `jq . file > /dev/null` or `python -m json.tool file`, and report whether it passes.

## Edge Cases
- Apply the fix recursively at every nesting depth inside objects and arrays.
- Keys containing spaces or special characters must already be quoted in valid HuJSON; if you encounter an invalid bare key, quote it and escape interior `"` and `\` characters.
- Single-quoted keys (JSON5 style) are not valid HuJSON; if found, convert to double-quoted with proper escaping.
- Empty objects `{}` require no change.
- Do not confuse keys with values: only tokens immediately preceding `:` inside object braces are keys.

## Examples
Input:
```
{
  // server config
  apps: {
    http: {
      servers: {
        srv0: {
          listen: [":443",],
        },
      },
    },
  },
}
```
Output (keys quoted only):
```
{
  // server config
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "listen": [":443",],
        },
      },
    },
  },
}
```
Output (full strict JSON conversion):
```json
{
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "listen": [":443"]
        }
      }
    }
  }
}
```

## Verification Checklist
- [ ] Every key is enclosed in double quotes.
- [ ] No values, strings, or numbers were changed.
- [ ] Comments and trailing commas were removed only if strict JSON was requested.
- [ ] The output passes validation with `jq` or `python -m json.tool`.

## Notes

