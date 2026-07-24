# Anti-Patterns: Code Tasks

## Comments
- Do NOT add comments that restate what the code already says (`// increment counter` above `i++`)
- Do NOT leave commented-out code in the final result — delete it or keep it, never leave dead code
- Do NOT add TODO/FIXME comments as a substitute for implementing the fix now
- Comments should explain WHY, not WHAT

## Code Changes
- Do NOT refactor unrelated code while implementing a feature — keep the diff focused
- Do NOT change formatting, rename variables, or restructure files outside the scope of the task
- Do NOT introduce new dependencies when the standard library or existing dependencies suffice
- Do NOT modify public APIs or exported symbols unless the task explicitly requires it
- Do NOT add error handling that silently swallows errors — propagate or handle explicitly

## Verification
- Do NOT claim code compiles without running the compiler
- Do NOT claim tests pass without running them
- Do NOT assume a fix works because the error message changed — verify the actual behavior
- Run the full relevant test suite, not just the single test you added
