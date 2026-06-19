# Git Hooks for Meept

This directory contains git hooks for enforcing code quality, security, and documentation standards.

## Installation

### Recommended: Use git config

The hooks are configured to run automatically when you clone the repository. If they're not running:

```bash
# From project root
git config core.hooksPath .githooks
```

### Alternative: Install script

```bash
# Run the installation script
./scripts/install-hooks.sh
```

This configures git and verifies the hooks are working.

### Manual installation (not recommended)

```bash
# Only if .githooks is not available
chmod +x .githooks/*
```

## Available Hooks

### pre-commit (Main Hook)

Entry point that runs all checks sequentially (8 total):

| # | Hook | Purpose |
|---|------|---------|
| 1 | pre-commit-deferred | Findings docs have resolution plans |
| 2 | pre-commit-mutexio | No I/O under mutex (CLAUDE.md rule) |
| 3 | pre-commit-u1000 | Unused code detection (staticcheck) |
| 4 | pre-commit-vet | Common Go bugs (built-in) |
| 5 | pre-commit-setters | Nil-safe setter methods |
| 6 | pre-commit-gosec | Security vulnerabilities |
| 7 | pre-commit-errors | Error handling anti-patterns |
| 8 | pre-commit-feature-docs | Documentation updates |

---

### pre-commit-deferred

Validates that findings documents with deferred items have corresponding deferred implementation plans.

**Triggers on:** Changes to `docs/plans/*findings*.md`

**Checks:**
- Counts unresolved deferred items in findings documents
- Verifies deferred implementation plans exist
- Blocks commit if deferred items lack resolution plans

**See:** CLAUDE.md "Deferred Item Resolution Protocol"

---

### pre-commit-mutexio

Enforces the CLAUDE.md mutex scope rule: never hold a mutex across I/O operations.

**Triggers on:** Changes to Go source files

**Checks:**
- Detects I/O operations (network, disk, LLM calls, channel sends) while mutex is held
- Flags `defer mu.Unlock()` patterns that span I/O
- Recommends "collect under lock, release, then operate" pattern

**See:** CLAUDE.md "Mutex scope"

---

### pre-commit-u1000

Runs staticcheck U1000 to detect unused code.

**Triggers on:** Changes to Go source files

**Checks:**
- Unused functions, methods, types, variables
- Helps keep codebase clean and maintainable

**Requires:** `staticcheck` (install: `go install honnef.co/go/tools/cmd/staticcheck@latest`)

---

### pre-commit-vet

Runs `go vet` on staged Go packages.

**Triggers on:** Changes to Go source files

**Detects:**
- Unreachable code
- Invalid printf format strings
- Possible nil pointer dereferences
- Shadowed variables
- Invalid struct tags
- Copying mutex values

**Requires:** Go (built-in, no installation needed)

---

### pre-commit-setters

Verifies all Set* methods on tool structs are nil-safe.

**Triggers on:** Changes to `internal/tools/` or `*_test.go` files

**Checks:**
- Runs `TestAllSetters_NilSafe` on staged changes
- Prevents typed-nil interface panics at runtime

**See:** CLAUDE.md "Typed-nil interface guard"

**Example:**
```go
// WRONG: direct assignment allows typed-nil panic
func (t *SomeTool) SetFenceChecker(fc FenceChecker) {
    t.fenceChecker = fc
}

// RIGHT: nil guard prevents panic
func (t *SomeTool) SetFenceChecker(fc FenceChecker) {
    if fc != nil {
        t.fenceChecker = fc
    }
}
```

---

### pre-commit-gosec

Security scanner for staged Go files.

**Triggers on:** Changes to Go source files

**Detects:**
- Hardcoded credentials and API keys (G101)
- SQL injection risks (G201, G202)
- Weak cryptographic functions (G401, G402)
- Command injection (G204)
- Unsafe file operations (G301-G306)
- Binding to all interfaces (G102)

**Requires:** `gosec` (install: `go install github.com/securego/gosec/v2/cmd/gosec@latest`)

**Graceful skip:** If gosec is not installed, the hook skips with a warning.

---

### pre-commit-errors

Checks for common error handling anti-patterns.

**Triggers on:** Changes to Go source files

**Detects:**
- Ignored errors (`_ = someFunc()`)
- `panic(err)` in non-test code
- `fmt.Errorf` without `%w` for wrapping
- Error assigned but not returned

**Example:**
```go
// WRONG: ignored error
_ = file.Close()

// RIGHT: handle error
if err := file.Close(); err != nil {
    return fmt.Errorf("close file: %w", err)
}
```

---

### pre-commit-feature-docs

Ensures code changes have corresponding documentation updates.

**Triggers on:** Changes to Go source files in `internal/`, `cmd/`, or `pkg/`

**Checks:**
- Detects feature code changes
- Maps changed files to documentation locations
- Verifies documentation exists and was modified
- Offers to generate documentation using aider

**Aider Integration:**

When documentation is missing, the hook offers to:
1. Create a documentation template
2. Run aider with glm-5.2 to analyze code changes
3. Generate draft documentation based on the code

**Manual aider usage:**
```bash
aider --model glm-5.2 \
      --message "Generate feature documentation based on these code changes" \
      docs/workflows/new-feature.md \
      internal/newfeature/file.go
```

**Configuration:**
```bash
# Override model (default: glm-5.2)
export AIDER_MODEL=glm-5.2
```

**Feature Mapping:**

| Code Directory | Documentation |
|----------------|--------------|
| `internal/agent/` | `docs/workflows/agent-orchestration.md` |
| `internal/llm/` | `docs/workflows/llm-management.md` |
| `internal/memory/` | `docs/workflows/memory.md` |
| `internal/security/` | `docs/workflows/security.md` |
| `internal/tools/` | `docs/workflows/tool-routing.md` |
| `internal/skills/` | `docs/workflows/skills.md` |
| `internal/scheduler/` | `docs/workflows/job-scheduling.md` |
| `internal/comm/` | `docs/workflows/external-integrations.md` |
| `internal/stt/` | `docs/workflows/speech-to-text.md` |
| `internal/tts/` | `docs/workflows/tts.md` |
| `internal/runtime/` | `docs/workflows/runtime.md` |
| `internal/pty/` | `docs/workflows/pty-streaming.md` |
| `internal/code/` | `docs/workflows/code-intelligence.md` |
| `internal/selfimprove/` | `docs/workflows/self-improvement.md` |
| `internal/project/` | `docs/workflows/project-context.md` |
| `internal/daemon/` | `docs/concepts/architecture.md` |

---

## Skipping Hooks

For emergency commits (not recommended):

```bash
git commit --no-verify -m "Emergency fix"
```

**Warning:** Skipping hooks bypasses important quality checks. Only use for:
- WIP commits during active development
- Emergency hotfixes
- Resolving hook-related issues

---

## Testing Hooks

To test hooks without committing:

```bash
# Run all hooks manually
.githooks/pre-commit

# Run individual hooks
.githooks/pre-commit-deferred
.githooks/pre-commit-vet
.githooks/pre-commit-gosec
.githooks/pre-commit-errors
.githooks/pre-commit-feature-docs

# Debug mode (verbose output)
bash -x .githooks/pre-commit-feature-docs
```

Note: Hooks check staged changes. If nothing is staged, they'll report "No staged changes to check".

---

## Troubleshooting

### Hook not running

**Cause:** git config not set or hooks not executable

**Solution:**
```bash
# Verify configuration
git config --get core.hooksPath  # Should output: .githooks

# If empty, set it
git config core.hooksPath .githooks

# Ensure hooks are executable
chmod +x .githooks/*
```

### Aider not found

**Cause:** aider is not installed

**Solution:**
```bash
pip install aider-chat
# Or: brew install aider (if available on your system)
```

### gosec not found

**Cause:** gosec is not installed

**Solution:**
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

The hook will skip gracefully if gosec is not installed.

### staticcheck not found

**Cause:** staticcheck is not installed

**Solution:**
```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
```

### False positives in documentation check

**Cause:** The hook may flag files that are actually documented elsewhere

**Solution:**
1. Update the feature mapping in `pre-commit-feature-docs`
2. Or add the file to the exclusion list in `is_feature_code()`
3. Or stage the documentation file with your code changes

### Bash version issues

**Cause:** Some hook features may require bash 4+

**Solution:** The hooks are written to be compatible with bash 3.x (macOS default). If you encounter issues, ensure you're using bash 4+ or report the bug.

---

## Development

### Adding new feature mappings

Edit `.githooks/pre-commit-feature-docs`:

```bash
extract_feature_name() {
  case "$base_feature" in
    "newfeature") echo "new-feature-doc" ;;
    # ... existing mappings
  esac
}
```

### Adding new hooks

1. Create hook script in `.githooks/`
2. Make executable: `chmod +x .githooks/new-hook`
3. Source from `pre-commit`:
   ```bash
   .githooks/new-hook || exit 1
   ```
4. Update this README

---

## Additional Resources

- [Git Hooks Documentation](https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks)
- [Aider Documentation](https://aider.chat/)
- [gosec Documentation](https://github.com/securego/gosec)
- [staticcheck Documentation](https://staticcheck.dev/)
- [glm-5.2 Model](https://z.ai/) - Z.ai language model
- CLAUDE.md - Project-specific guidelines
