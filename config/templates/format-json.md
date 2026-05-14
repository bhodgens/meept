---
name: format-json
description: "validate and pretty-print JSON"
scope: turn
---

Validate and pretty-print the following JSON.

If the input is valid JSON:
- Format it with 2-space indentation.
- Sort object keys alphabetically.
- Report the top-level type and number of elements/keys.

If the input is not valid JSON:
- Identify the syntax error (line/position if possible).
- Suggest a fix.
- Show the corrected JSON if the fix is unambiguous.

Input:

$@
