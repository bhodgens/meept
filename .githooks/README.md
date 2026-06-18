# Git Hooks for Meept

This directory contains git hooks for enforcing code quality and documentation standards.

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

Entry point that runs all checks sequentially:

1. **Deferred item validation** - Checks findings docs have resolution plans
2. **Feature documentation check** - Verifies code changes include doc updates

### pre-commit-deferred

Validates that any findings documents with deferred items have corresponding deferred implementation plans.

**Triggers on:** Changes to `docs/plans/*findings*.md`

**Checks:**
- Counts unresolved deferred items in findings documents
- Verifies deferred implementation plans exist
- Blocks commit if deferred items lack resolution plans

**See:** CLAUDE.md "Deferred Item Resolution Protocol"

### pre-commit-feature-docs

Ensures code changes have corresponding documentation updates.

**Triggers on:** Changes to Go source files in `internal/`, `cmd/`, or `pkg/`

**Checks:**
- Detects feature code changes
- Maps changed files to documentation (`docs/workflows/`, `docs/concepts/`, `docs/reference/`)
- Verifies documentation exists and was updated
- Offers to generate documentation using aider

**Aider Integration:**

When documentation is missing, the hook offers to:
1. Create a documentation template
2. Run aider with glm-5.2 to analyze code changes
3. Generate draft documentation based on the code

**Manual aider usage:**
```bash
# Install aider
pip install aider-chat

# Configure for glm-5.2 (already in project .aider.conf.yml)
echo "model: glm-5.2" > ~/.aider.conf.yml

# Generate documentation
aider --model glm-5.2 \
      --message "Generate feature documentation based on these code changes" \
      docs/workflows/new-feature.md \
      internal/newfeature/file.go
```

**Configuration:**
```bash
# Override model (default: glm-5.2)
export AIDER_MODEL=glm-5.2

# Or use a different model
export AIDER_MODEL=claude-sonnet-4-6
```

## Skipping Hooks

For emergency commits (not recommended):

```bash
git commit --no-verify -m "Emergency fix"
```

**Warning:** Skipping hooks bypasses important quality checks. Only use for:
- WIP commits during active development
- Emergency hotfixes
- Resolving hook-related issues

## Testing Hooks

To test hooks without committing:

```bash
# Run all hooks manually
.githooks/pre-commit

# Run individual hooks
.githooks/pre-commit-deferred
.githooks/pre-commit-feature-docs

# Debug mode (verbose output)
bash -x .githooks/pre-commit-feature-docs
```

Note: Hooks check staged changes. If nothing is staged, they'll report "No staged changes to check".

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

### False positives in documentation check

**Cause:** The hook may flag files that are actually documented elsewhere

**Solution:**
1. Update the feature mapping in `pre-commit-feature-docs`
2. Or add the file to the exclusion list in `is_feature_code()`
3. Or stage the documentation file with your code changes

### Bash version issues

**Cause:** Some hook features may require bash 4+

**Solution:** The hooks are written to be compatible with bash 3.x (macOS default). If you encounter issues, ensure you're using bash 4+ or report the bug.

## Feature Mapping

The hook maps code directories to documentation files:

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

## Additional Resources

- [Git Hooks Documentation](https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks)
- [Aider Documentation](https://aider.chat/)
- [glm-5.2 Model](https://z.ai/) - Z.ai language model
- CLAUDE.md - Project-specific guidelines
