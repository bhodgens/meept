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

Entry point that runs all checks sequentially (18 total). The numbers below are
the `[n/18]` labels `.githooks/pre-commit` prints:

| # | Hook | Purpose |
|---|------|---------|
| 1 | pre-commit-build | Staged packages + clean downstream importers compile (no stray binaries) |
| 2 | pre-commit-deferred | Findings docs have resolution plans |
| 3 | pre-commit-mutexio | No I/O under mutex (CLAUDE.md rule) |
| 4 | pre-commit-u1000 | Unused code detection (staticcheck) |
| 5 | pre-commit-vet | Common Go bugs (built-in) |
| 6 | pre-commit-setters | Nil-safe setter methods |
| 7 | pre-commit-selflock | Self-deadlocking locks (AST analyzer) |
| 8 | pre-commit-gosec | Security vulnerabilities |
| 9 | pre-commit-errors | Error handling anti-patterns |
| 10 | pre-commit-predictable-ids | Use pkg/id.Generate (not time.Now().UnixNano()) |
| 11 | pre-commit-sqlite-pragmas | SQLite WAL + busy_timeout required |
| 12 | pre-commit-channel-nilafterclose | Use sync.Once (not close+nil) |
| 13 | pre-commit-concurrency | Concurrency annotations (informational — never blocks) |
| 14 | pre-commit-mutation | Mutation-test coverage (report only — never blocks) |
| 15 | pre-commit-feature-docs | Documentation updates |
| 16 | pre-commit-ascii | No CJK/Hangul/fullwidth characters in staged text files |
| 17 | pre-commit-dart-format | Staged Dart blob (index content) is dart-format clean |
| 18 | pre-commit-e2e | Affected hermetic e2e suites run + new feature packages have e2e coverage |

Steps 13 and 14 are reported but never block the commit. Steps 1-12, 15-17,
and 18 do.
`pre-commit-staticcheck` is not in this list: it is standalone (run it directly,
or via your editor) and additionally analyzes the tag-gated `magefiles` package
with `-tags mage`.

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

### pre-commit-predictable-ids

Flags ID-generation code that uses `time.Now().UnixNano()` (or `Unix()`) instead of the crypto/rand-backed `pkg/id.Generate()` helper. Nanosecond timestamps can collide under concurrency or clock drift, producing duplicate persistent keys.

**Triggers on:** Added lines in staged Go source files (excludes `_test.go`)

**Detects:**
- `fmt.Sprintf("prefix-%d", time.Now().UnixNano())` — most common pattern
- Direct assignment: `id = time.Now().UnixNano()` where the variable name suggests an ID
- Byte/hex manipulation seeded from `time.Now().UnixNano()`

**Example:**
```go
// WRONG: nanosecond timestamps can collide under concurrency
jobID := fmt.Sprintf("dispatch-%d", time.Now().UnixNano())

// RIGHT: crypto/rand-backed, no collisions
import "github.com/caimlas/meept/pkg/id"
jobID := id.Generate("dispatch-")
```

Suppress false positives (e.g., genuine timestamps that aren't IDs) with:
```go
createdAt = time.Now().UnixNano() //nolint:predictableids // genuine timestamp, not an ID
```

---

### pre-commit-sqlite-pragmas

Ensures all `sql.Open("sqlite3", ...)` calls include WAL mode and busy_timeout in the DSN.

**Triggers on:** Added lines in staged Go source files (excludes `_test.go`)

**Checks:**
- Flags any `sql.Open("sqlite3", <dsn>)` where `<dsn>` doesn't contain `_journal_mode=WAL` and `_busy_timeout`
- Prevents "database is locked" errors under concurrent SQLite access

**Example:**
```go
// WRONG: default journal mode causes lock contention
db, err := sql.Open("sqlite3", path)

// RIGHT: WAL + busy_timeout prevents concurrent-access issues
dsn := path + "?_journal_mode=WAL&_busy_timeout=5000"
db, err := sql.Open("sqlite3", dsn)
```

**Suppress:** `//nolint:sqlitepragmas` when PRAGMAs are managed via `PRAGMA` statements on an already-open connection.

---

### pre-commit-channel-nilafterclose

Detects the `close(ch); ch = nil` pattern on struct fields, which causes data races when the channel is read from the struct field in a `select` statement.

**Triggers on:** Added lines in staged Go source files (excludes `_test.go`)

**Checks:**
- Flags `close(s.field)` followed by `s.field = nil` within 3 lines
- The nil'd field causes `select { case <-s.field: }` to block forever (nil channels never complete)

**Example:**
```go
// WRONG: select reads nil'd field, blocks forever
func (s *Scheduler) Stop() {
    close(s.stopCh)
    s.stopCh = nil  // Start()'s select now sees nil, blocks forever
}

// RIGHT: sync.Once for safe idempotent close
func (s *Scheduler) Stop() {
    s.stopOnce.Do(func() { close(s.stopCh) })
}
```

**Suppress:** `//nolint:channil` when the nil check is a deliberate sentinel (e.g., checked via `!= nil` before entering the select).

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

### pre-commit-dart-format

Blocks commits that stage unformatted Dart files under `ui/flutter_ui/`.

**Triggers on:** Staged `ui/flutter_ui/**/*.dart` files (added, copied, modified, renamed)

**Checks:**
- Validates the staged **index blob** (`git show ":$f"` written to a temp file), i.e. exactly the bytes the commit will contain — formatting the working tree after staging no longer hides an unformatted commit
- Runs `dart format --output=none --set-exit-if-changed` on that content only; files outside `ui/flutter_ui/` are ignored
- Lists every staged path that needs formatting and points at `make fmt-gui`
- With Dart files to check and no `dart` binary: **FAILS** (exit 1) instead of skipping — same policy as `make fmt-check-gui`, so the hook and `make lint-ci` cannot disagree about a tree neither can verify
- Passes cleanly (exit 0, explicit message) only when nothing Dart is staged

**Requires:** dart (on PATH, or the dart bundled with the Flutter SDK)

**See:** `make fmt-check-gui`, `make fmt-gui`, docs/workflows/flutter_gui.md

---

### pre-commit-e2e

Enforces the e2e testing policy on staged Go changes (the repo's
NO-NEW-UNIT-TESTS policy — see docs/workflows/e2e-testing.md).

**Triggers on:** Staged Go files under `internal/`, `pkg/`, or `cmd/` (and any
new package directory under those trees)

**Checks:**
- **Affected-suite gate**: maps staged paths through `e2e/manifest.json`
  `path_map` and runs the affected hermetic e2e suites
  (`scripts/e2e-affected.sh`). Suites whose dirs don't exist yet (manifest
  status `todo`) are reported and skipped; the smoke suite runs as fallback
  whenever feature Go files are staged with no hit or with only-pending hits.
  Any failing suite FAILS the commit.
- **New-feature coverage rule**: a staged change creating files under a NEW
  package directory (no `path_map` entry) FAILS with instructions to add an
  e2e suite + manifest entry, unless e2e suite files are staged in the same
  commit. Exempt: `_test.go`, docs, generated files, the e2e tree itself.

**Skip (emergencies only — prints a loud warning):**
```bash
MEEPT_SKIP_E2E=1 git commit -m "..."
```

**See:** docs/workflows/e2e-testing.md, scripts/e2e-affected.sh, e2e/manifest.json

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

**Cause:** A sub-hook needs a newer bash than the one `bash` resolves to on PATH

**Solution:** The hooks are written to be compatible with bash 3.2 (macOS
default), and the sub-hooks start with `#!/usr/bin/env bash` so the same files
run on macOS and on the Linux CI runner. If `bash --version` says 3.2 and a
hook still fails, that is a bug in the hook — report it rather than upgrading
bash as a workaround (grading the interpreter was the old failure mode: a
hard-coded `/opt/homebrew/bin/bash` shebang made the CI hook job unable to
exec the suite at all).

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

## Pre-push: bench regression gate (pre-push → pre-push-bench)

Before every `git push`, runs the meept-bench regression suite
(9 tasks, ~2 min) against the local daemon and diffs the result against
the committed baseline (`~/git/meept-bench/results/baseline/regression.jsonl`).

- **Regressed or errored** → push blocked, scorecard path printed.
- **Daemon unreachable** → push blocked with instructions (the gate is
  the point; silent skips would be decorative).
- **Skip options**: `git push --no-verify` (all hooks) or
  `MEEPT_BENCH_SKIP=1 git push` (bench gate only).
- **Re-baseline deliberately** after an intended behavior change:
  `cp ~/git/meept-bench/results/gate-pre-push/results.jsonl \
      ~/git/meept-bench/results/baseline/regression.jsonl`

Requires the meept daemon running locally. Config override:
`MEEPT_BENCH_DIR=/path/to/meept-bench`.
