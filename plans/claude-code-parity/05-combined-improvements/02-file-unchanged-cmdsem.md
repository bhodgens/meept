# Leaf 05-02: FILE_UNCHANGED Optimization + Command Exit Code Semantics

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 05-combined-improvements/orchestrator.md
**Scope:** (A) Add FILE_UNCHANGED stub to file_read when content hasn't changed since last read. (B) Add command exit code semantics map to shell tool so grep/rg/diff/find/test exit 1 is not reported as an error.
**Dependencies:** None
**Estimated Context:** ~50K

## Interface Contract

This leaf exposes:
- `FileUnchangedStub` constant returned by file_read for unchanged files
- `commandSemantics` map in shell tool for exit code interpretation
- Updated shell tool result construction using semantics map

## Tasks

### Part A: FILE_UNCHANGED Optimization

### Task 1: Add unchanged detection to file_read

**File:** `internal/tools/builtin/file_read.go` (or wherever file_read is implemented)

Read the existing file_read implementation. It has a `ReadCache` that stores snapshots for edit recovery. Add a content-hash check before returning full content:

```go
const FileUnchangedStub = "File unchanged since last read. The content from the earlier read is still current — refer to that instead of re-reading."

// In the Execute method, after reading the file content:
contentHash := sha256Hex(content)
if cached, ok := t.readCache.Get(filePath); ok {
    if cached.ContentHash == contentHash {
        return &tools.Result{
            Output:  FileUnchangedStub,
            Success: true,
            // Include minimal metadata so the agent knows the file exists
            Metadata: map[string]any{
                "path":      filePath,
                "unchanged": true,
                "hash":      contentHash,
            },
        }, nil
    }
}
// Otherwise, return full content and update cache with hash
```

The ReadCache entry needs a `ContentHash` field. Read the existing cache struct and add it.

### Task 2: Ensure hashline anchors still work with unchanged stub

The hashline-anchored edit system (`LINE:HASH|content`) depends on file_read returning formatted content. When file_read returns the unchanged stub, the agent should refer to its earlier read for anchors. Verify that:
- The stub message is clear enough that the agent knows to use previous content
- The edit recovery system (`findMatchingLine`, `attemptRecovery`) still works because it uses its own cached snapshots, not the file_read output

No code change needed if the edit system uses its own cache (which it does — `ReadCache` stores snapshots independently). Just verify and document.

### Task 3: Tests for FILE_UNCHANGED

**File:** `internal/tools/builtin/file_read_test.go` (extend existing)

- `TestFileReadUnchanged` — second read of same file returns stub
- `TestFileReadChanged` — read after modification returns full content
- `TestFileReadUnchangedDifferentFile` — different file returns full content
- `TestFileReadUnchangedHash` — hash matches on unchanged, differs on changed

### Part B: Command Exit Code Semantics

### Task 4: Add command semantics map to shell tool

**File:** `internal/tools/builtin/shell.go`

Read the existing shell tool. Find where the result is constructed from the exit code (currently: `Success: returnCode == 0`). Add a semantics map:

```go
// commandSemantics maps command names to exit code interpretation.
// For these commands, exit code 1 is NOT an error — it has a specific
// meaning that should be reported as informational, not as a failure.
type commandSemantic struct {
    isError func(exitCode int) bool
    message func(exitCode int) string
}

var commandSemantics = map[string]commandSemantic{
    "grep": {
        isError: func(c int) bool { return c >= 2 },
        message: func(c int) string {
            if c == 1 { return "no matches found" }
            return ""
        },
    },
    "rg": {
        isError: func(c int) bool { return c >= 2 },
        message: func(c int) string {
            if c == 1 { return "no matches found" }
            return ""
        },
    },
    "diff": {
        isError: func(c int) bool { return c >= 2 },
        message: func(c int) string {
            if c == 1 { return "files differ" }
            return ""
        },
    },
    "find": {
        isError: func(c int) bool { return c >= 2 },
        message: func(c int) string {
            if c == 1 { return "some directories were inaccessible" }
            return ""
        },
    },
    "test": {
        isError: func(c int) bool { return c >= 2 },
        message: func(c int) string {
            if c == 1 { return "condition is false" }
            return ""
        },
    },
}
```

### Task 5: Wire semantics into result construction

**File:** `internal/tools/builtin/shell.go` (continued)

Update the result construction to use the semantics map. Extract the base command name from the command string (first word, stripping path prefixes):

```go
func interpretExitCode(command string, exitCode int) (success bool, message string) {
    if exitCode == 0 {
        return true, ""
    }

    // Extract base command name (first word, strip path)
    baseCmd := extractBaseCommand(command)

    if sem, ok := commandSemantics[baseCmd]; ok {
        if !sem.isError(exitCode) {
            // Exit code 1 for grep/diff/etc. is informational, not an error
            return true, sem.message(exitCode)
        }
    }

    // Default: non-zero exit is a failure
    return false, fmt.Sprintf("exit code %d", exitCode)
}

func extractBaseCommand(command string) string {
    // Strip leading env vars, sudo, etc.
    // Take the first word that looks like a command name
    fields := strings.Fields(command)
    for _, f := range fields {
        // Skip env var assignments (KEY=VALUE)
        if strings.Contains(f, "=") {
            continue
        }
        // Strip path prefix
        base := filepath.Base(f)
        return base
    }
    return ""
}
```

Update the result:
```go
success, semanticMsg := interpretExitCode(cmd, exitCode)
result := &tools.Result{
    Output:  output,
    Success: success,
}
if semanticMsg != "" {
    result.Metadata = map[string]any{"semantic_message": semanticMsg}
    // Append to output so the agent sees it
    result.Output = output + "\n[" + semanticMsg + "]"
}
```

### Task 6: Handle compound commands

For compound commands (`cmd1 | cmd2`, `cmd1 && cmd2`), the exit code is from the LAST command in the pipeline/chain. The semantics map should apply to the last command. Read the existing pipe/metachar splitting logic in shell.go — if it already splits and evaluates segments, apply semantics to the final segment's command.

If the shell tool runs the full compound command and gets a single exit code, apply semantics based on the last command in the chain (split on `|`, `&&`, `||`, `;` and take the last segment's base command).

### Task 7: Tests for command semantics

**File:** `internal/tools/builtin/shell_test.go` (extend existing)

- `TestGrepExit1NotError` — grep with no matches (exit 1) returns Success=true, message="no matches found"
- `TestGrepExit2IsError` — grep with error (exit 2) returns Success=false
- `TestDiffExit1NotError` — diff with differences (exit 1) returns Success=true
- `TestDiffExit2IsError` — diff with error (exit 2) returns Success=false
- `TestFindExit1NotError` — find with inaccessible dirs (exit 1) returns Success=true
- `TestUnknownCommandExit1IsError` — unknown command with exit 1 returns Success=false
- `TestExtractBaseCommand` — handles paths, env vars, sudo
- `TestCompoundCommandSemantics` — `ls | grep foo` uses grep semantics for exit code

## Self-Verification Checklist

- [ ] `go build ./internal/tools/...` compiles
- [ ] `go test ./internal/tools/builtin/... -race -run "TestFileRead|TestGrep|TestDiff|TestFind|TestCommand|TestExtract"` passes
- [ ] FILE_UNCHANGED stub returned for unchanged files
- [ ] grep exit 1 reported as success with "no matches found"
- [ ] diff exit 1 reported as success with "files differ"
- [ ] Unknown commands still treat exit 1 as failure
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] FILE_UNCHANGED uses content hash comparison (not timestamp)
- [ ] Hashline edit system unaffected by unchanged stub
- [ ] Semantics map covers grep, rg, diff, find, test
- [ ] Exit code >= 2 is always an error (even for semantic commands)
- [ ] Compound commands use last command's semantics
- [ ] extractBaseCommand handles paths, env vars, sudo
- [ ] Semantic message appended to output (agent sees it)
- [ ] No debug artifacts, no TODOs, no placeholder values
