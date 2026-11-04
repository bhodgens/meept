# Git Hooks Reference

This document describes the git hooks used in the Meept project for enforcing code quality and documentation standards.

## Overview

Meept uses a multi-hook pre-commit system that validates:
1. Deferred item resolution from code reviews
2. Feature documentation updates for code changes

## Hook Architecture

```
git commit
    └── pre-commit (main entry point)
        ├── pre-commit-deferred
        └── pre-commit-feature-docs
```

## Installation

### Quick Install

```bash
# From project root
./scripts/install-hooks.sh
```

### Manual Install

```bash
# Make hooks executable
chmod +x .git/hooks/pre-commit*
```

### Persistent Installation (across clones)

```bash
# Create shared hooks directory
mkdir -p .githooks
cp .git/hooks/pre-commit* .githooks/

# Configure git to use project hooks
git config core.hooksPath .githooks
```

## Hooks

### pre-commit

**Purpose:** Main entry point that orchestrates all pre-commit checks.

**Location:** `.git/hooks/pre-commit`

**Checks:**
1. Runs deferred item validation
2. Runs feature documentation check

**Exit codes:**
- `0` - All checks passed
- `1` - One or more checks failed

### pre-commit-deferred

**Purpose:** Ensures deferred items from code reviews have resolution plans.

**Location:** `.git/hooks/pre-commit-deferred`

**Triggers:** Changes to `docs/plans/*findings*.md`

**Checks:**
- Counts unresolved deferred items in staged findings documents
- Verifies corresponding deferred implementation plans exist
- Blocks commit if deferred items lack resolution

**Example Output:**
```
⚠️  Found 3 unresolved deferred item(s) in staged findings files:
  - docs/plans/review-findings-1.md (2 items)
  - docs/plans/review-findings-2.md (1 item)

📋 ACTION REQUIRED:
   Option 1: Create docs/plans/[review]-deferred-implementation.md
   Option 2: Resolve the deferred items before committing
   Option 3: Skip this check with --no-verify (not recommended)
```

**See:** CLAUDE.md "Deferred Item Resolution Protocol"

### pre-commit-feature-docs

**Purpose:** Ensures code changes have corresponding documentation updates.

**Location:** `.git/hooks/pre-commit-feature-docs`

**Triggers:** Changes to Go source files in `internal/`, `cmd/`, or `pkg/`

**Checks:**
1. Detects feature code changes
2. Maps changed files to documentation locations
3. Verifies documentation exists and was modified
4. Offers to generate documentation using aider

**Feature Mapping:**

| Code Directory | Documentation File |
|----------------|-------------------|
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

**Aider Integration:**

When documentation is missing, the hook offers to:
1. Create a documentation template
2. Run aider with glm-5.2 to analyze code changes
3. Generate draft documentation

**Manual aider usage:**
```bash
aider --model glm-5.2 \
      --message "Generate feature documentation for these changes" \
      docs/workflows/feature-name.md \
      internal/feature/file.go
```

**Configuration:**
```bash
# Override model (default: glm-5.2)
export AIDER_MODEL=claude-sonnet-4-6

# The hook uses project-level .aider.conf.yml if available
```

**Example Output:**
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Feature Documentation Pre-Commit Check
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Analyzing staged changes...

⚠️  No documentation found for feature: new-feature
   Suggested documentation file: docs/workflows/new-feature.md

Documentation template:
---
title: New-Feature
---

# New-Feature

## Overview
<!-- What does this feature do? -->

...

📋 ACTION REQUIRED:
   Option 1: Create documentation at docs/workflows/new-feature.md
   Option 2: Update existing docs if feature already documented elsewhere
   Option 3: Use aider to help generate docs (run: aider --model glm-5.2)
   Option 4: Skip this check with --no-verify (not recommended)
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `AIDER_MODEL` | `glm-5.2` | Model to use for aider documentation generation |

### Aider Configuration

Project-level: `.aider.conf.yml`
User-level: `~/.aider.conf.yml`

**Example `.aider.conf.yml`:**
```yaml
# Model configuration
model: glm-5.2

# Don't auto-commit - let user review first
auto-commits: false

# Show diffs after changes
show-diffs: true

# Disable fancy UI
fancy-input: false

# Reasoning effort
reasoning-effort: medium

# Verify context
verify-context: true
```

## Skipping Hooks

For emergency commits (not recommended):

```bash
git commit --no-verify -m "Emergency fix"
```

**When to skip:**
- WIP commits during active development
- Emergency hotfixes
- Resolving hook-related issues

**When NOT to skip:**
- Regular development commits
- Feature completions
- Code review iterations

## Troubleshooting

### Hook not running

**Symptoms:** Commit succeeds without running checks

**Solution:**
```bash
# Verify hooks are executable
ls -la .git/hooks/pre-commit*

# Make executable if needed
chmod +x .git/hooks/pre-commit*
```

### "aider not found" error

**Symptoms:** Hook fails when trying to generate documentation

**Solution:**
```bash
# Install aider
pip install aider-chat

# Or via Homebrew (if available)
brew install aider
```

### False positives in documentation check

**Symptoms:** Hook flags files that are actually documented

**Solutions:**
1. Add documentation file to staging: `git add docs/workflows/feature.md`
2. Update feature mapping in hook script
3. Add file to exclusion list in `is_feature_code()` function

### Hook is too slow

**Symptoms:** Commit takes too long

**Solutions:**
1. Reduce scope of changed files
2. Use `--no-verify` for WIP commits only
3. Optimize feature mapping logic

## Testing Hooks

Run hooks manually without committing:

```bash
# Test all checks
.git/hooks/pre-commit

# Test individual hooks
.git/hooks/pre-commit-deferred
.git/hooks/pre-commit-feature-docs

# Debug mode (verbose output)
bash -x .git/hooks/pre-commit-feature-docs
```

## Development

### Adding new feature mappings

Edit `.git/hooks/pre-commit-feature-docs`:

```bash
extract_feature_name() {
  case "$base_feature" in
    "newfeature") echo "new-feature-doc" ;;
    # ... existing mappings
  esac
}
```

### Adding new hooks

1. Create hook script in `.git/hooks/`
2. Make executable: `chmod +x .git/hooks/new-hook`
3. Source from `pre-commit`:
   ```bash
   .git/hooks/new-hook || exit 1
   ```

## Related Documentation

- [CLAUDE.md](../../CLAUDE.md) - Project guidelines including hook usage
- [Documentation Maintenance](../../CLAUDE.md#documentation-maintenance) - When to update docs
- [Deferred Item Resolution Protocol](../../CLAUDE.md#deferred-item-resolution-protocol) - Deferred items
- [Feature Documentation Requirements](../../CLAUDE.md#feature-documentation-requirements) - Documentation standards

## External Resources

- [Git Hooks Documentation](https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks)
- [Aider Documentation](https://aider.chat/)
- [glm-5.2 Model](https://z.ai/) - Z.ai language model
