# Anti-Patterns: Debugging Tasks

## Shotgun Debugging
- Do NOT make multiple simultaneous changes to "see what fixes it" — change one thing at a time
- Do NOT add logging, restart services, clear caches, and toggle flags all at once
- Do NOT apply fixes you do not understand just because they seem related
- Form a hypothesis, test it, confirm or reject, then form the next hypothesis

## Premature Conclusions
- Do NOT declare root cause identified based on correlation alone — reproduce and confirm
- Do NOT stop investigating at the first error message — trace the full call chain
- Do NOT assume the bug is in the most recently changed code without evidence
- Do NOT confuse "the error went away" with "the bug is fixed" — verify the underlying cause is resolved

## Scope Discipline
- Do NOT fix unrelated issues you notice while debugging — note them and stay focused
- Do NOT refactor the module you are debugging unless the refactor IS the fix
- Do NOT expand a single-bug investigation into a system-wide audit without explicit direction
- Keep the fix minimal and targeted; large diffs in bug fixes are a red flag
