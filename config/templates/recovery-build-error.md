---
name: recovery-build-error
description: "systematic procedure for diagnosing and fixing build errors"
scope: turn
---

The build has failed. Follow this procedure to diagnose and fix the error.

**Step 1: Read the full error output.**
- Identify the file, line number, and error message for each compilation error.
- Distinguish between: syntax errors, type errors, undefined references, import errors, and linker errors.
- Fix errors in order from top to bottom -- earlier errors often cause cascading failures below.

**Step 2: Check for common Go build issues.**
- **Undefined reference**: Did you rename or move a function/type? Update all call sites.
- **Import cycle**: Package A imports B which imports A. Restructure by extracting shared types into a third package.
- **Unused import/variable**: Remove it, or use `_ = varName` if intentionally unused (blank import for side effects: `_ "pkg"`).
- **Type mismatch**: Check interface satisfaction. A `*ConcreteType` assigned to an interface is non-nil even if the pointer is nil -- use typed-nil guards.
- **Missing method**: The concrete type doesn't satisfy the interface. Add the missing method or adjust the interface.

**Step 3: Check for cascading errors.**
- Fix the first error, then rebuild. Often 10 errors collapse to 1-2 real issues.
- Do not attempt to fix all errors simultaneously if they may be related.

**Step 4: Verify module state.**
- Run `go mod tidy` to ensure go.mod and go.sum are consistent.
- Check `go.sum` for conflicts if merging branches.
- Verify the Go version matches: `go version` vs `go.mod` directive.

**Step 5: Verify the fix.**
- Run `go build ./...` -- the full project should compile.
- Run `go vet ./...` -- catch issues the compiler doesn't flag.
- Run `go test ./...` -- ensure no tests were broken by the fix.

Build error output:

$@
