#!/bin/bash
# Install git hooks for Meept
#
# This script configures git to use the project's .githooks directory.
# Run from the project root: ./scripts/install-hooks.sh

set -e

PROJECT_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
GITHOOKS_DIR="$PROJECT_ROOT/.githooks"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Meept Git Hooks Installation"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Check if .githooks directory exists
if [ ! -d "$GITHOOKS_DIR" ]; then
  echo "❌ Error: .githooks directory not found at $GITHOOKS_DIR"
  echo ""
  echo "Make sure you're running this from the project root."
  exit 1
fi

# Configure git to use .githooks
echo "Configuring git to use .githooks directory..."
git config core.hooksPath .githooks

echo ""
echo "✅ Git hooks configured successfully"
echo ""
echo "Hooks location: $GITHOOKS_DIR"
echo "Installed hooks:"
echo "  - pre-commit (main entry point)"
echo "  - pre-commit-deferred (deferred item validation)"
echo "  - pre-commit-feature-docs (documentation check)"
echo ""

# Verify hooks are executable
echo "Verifying hooks are executable..."
for hook in "$GITHOOKS_DIR"/*; do
  if [ -f "$hook" ]; then
    chmod +x "$hook"
    echo "  ✓ $(basename "$hook")"
  fi
done

echo ""

# Test the hooks
echo "Testing hooks..."
if "$GITHOOKS_DIR/pre-commit" > /dev/null 2>&1; then
  echo "✅ Pre-commit hook test passed"
else
  echo "⚠️  Pre-commit hook test had issues (this is OK if no staged changes)"
fi

echo ""

# Check if aider is available
if command -v aider &> /dev/null; then
  echo "✅ aider found: $(which aider)"
  echo "   The feature-docs hook can use aider to generate documentation."
  echo "   Model: $(grep '^model:' .aider.conf.yml 2>/dev/null | cut -d: -f2 | tr -d ' ' || echo 'glm-5.2')"
else
  echo "⚠️  aider not found"
  echo ""
  echo "   The feature-docs hook can use aider to help generate documentation."
  echo "   Install with: pip install aider-chat"
  echo "   Or: brew install aider (if available)"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Usage:"
echo "  Hooks run automatically on 'git commit'"
echo "  Skip with --no-verify if needed (not recommended)"
echo ""
echo "For more information, see: .githooks/README.md"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
