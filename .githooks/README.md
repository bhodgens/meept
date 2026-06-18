# Git Hooks for Meept

This directory contains git hooks for enforcing code quality and documentation standards.

## Installation

To install the hooks:

```bash
# Make hooks executable
chmod +x .git/hooks/pre-commit*

# Optional: Install to shared location for all contributors
cp .git/hooks/pre-commit* /usr/local/share/meept-hooks/
```

For a project-wide installation that persists across clones:

```bash
# Create a hooks directory in the project root
mkdir -p .githooks
cp .git/hooks/pre-commit* .githooks/

# Configure git to use this directory
git config core.hooksPath .githooks
```

## Available Hooks

### pre-commit (Main Hook)

Entry point that runs all checks sequentially:
1. Deferred item validation
2. Feature documentation check

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

# Configure for glm-5.2
echo "model: glm-5.2" > ~/.aider.conf.yml

# Generate documentation
aider --model glm-5.2 \
      --message "Generate feature documentation based on these code changes" \
      docs/workflows/new-feature.md \
      internal/newfeature/file.go
```

**Configuration:**
```bash
# Override model
export AIDER_MODEL=glm-5.2

# Or use a different model
export AIDER_MODEL=claude-sonnet-4-6
```

## Skipping Hooks

For emergency commits (not recommended):

```bash
git commit --no-verify -m "Emergency fix"
```

⚠️ **Warning:** Skipping hooks bypasses important quality checks. Only use for:
- WIP commits during active development
- Emergency hotfixes
- Resolving hook-related issues

## Hook Development

To test hooks without committing:

```bash
# Run hooks manually
.git/hooks/pre-commit

# Run individual hook
.git/hooks/pre-commit-feature-docs

# Debug mode (show what would be checked)
bash -x .git/hooks/pre-commit-feature-docs
```

## Troubleshooting

### Hook not running

Ensure hooks are executable:
```bash
chmod +x .git/hooks/pre-commit*
```

### Aider not found

Install aider:
```bash
pip install aider-chat
```

### False positives in documentation check

The hook may flag files that are actually documented elsewhere. In this case:
1. Update the feature mapping in `pre-commit-feature-docs`
2. Or add the file to the exclusion list in `is_feature_code()`

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

## Additional Resources

- [Git Hooks Documentation](https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks)
- [Aider Documentation](https://aider.chat/)
- CLAUDE.md - Project-specific guidelines
