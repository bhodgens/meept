---
name: recovery-test-failure
description: "systematic procedure for diagnosing and fixing test failures"
scope: turn
---

A test has failed. Follow this procedure to diagnose and fix it.

**Step 1: Read the failure output carefully.**
- Identify the exact test name, file, and line number.
- Read the error message and any diff output completely before acting.
- Determine: is this a assertion failure, a panic, a timeout, or a compilation error?

**Step 2: Reproduce the failure in isolation.**
- Run only the failing test: `go test -run TestName -v ./path/to/package/`
- If the failure is intermittent, run with `-count=N` to check for flakiness.
- Check if the failure is environment-dependent (missing env vars, network, file paths).

**Step 3: Identify the root cause.**
- Read the test code and the code under test. Do not assume -- read both.
- Common root causes, in order of likelihood:
  1. The test assertions don't match recent changes to the code under test.
  2. The code under test has a regression (new bug introduced by recent changes).
  3. Test state pollution (shared mutable state, unclosed resources, goroutine leaks).
  4. Race condition (run with `-race` to check).
  5. Time-dependent logic (use fake clocks or fixed timestamps).
  6. External dependency change (API contract changed, service down).

**Step 4: Fix the minimal necessary change.**
- Fix the root cause, not the symptom. Do not weaken assertions to make the test pass.
- If the test itself was wrong, fix the test AND add a comment explaining the correct expected behavior.
- If the code under test has a bug, fix the bug. Do not add workarounds in the test.

**Step 5: Verify the fix.**
- Run the failing test in isolation: it should pass.
- Run the full package tests: nothing else should break.
- If the fix touched shared code, run the full test suite: `go test ./...`

**Step 6: Document if non-obvious.**
- If the root cause was subtle, add a comment in the test or code explaining the invariant.
- If the failure revealed a gap in test coverage, add a regression test.

Test failure details:

$@
